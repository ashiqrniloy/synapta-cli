package core

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ashiqrniloy/synapta-cli/internal/llm"
)

const (
	currentSessionVersion = 1

	compactionSummaryPrefix       = "The conversation history before this point was compacted into the following summary:\n\n<summary>\n"
	compactionSummarySuffix       = "\n</summary>"
	compactionMethodModel         = "model"
	compactionMethodDeterministic = "fallback"
)

type CompactionSettings struct {
	Enabled          bool
	ReserveTokens    int
	KeepRecentTokens int
}

func DefaultCompactionSettings() CompactionSettings {
	return CompactionSettings{
		Enabled:          true,
		ReserveTokens:    16384,
		KeepRecentTokens: 20000,
	}
}

type CompactionSummarizer func(ctx context.Context, toSummarize []llm.Message, previousSummary string) (string, error)

type SessionInfo struct {
	Path         string
	ID           string
	CWD          string
	Created      time.Time
	Modified     time.Time
	MessageCount int
	FirstMessage string
}

type sessionEntry struct {
	Type                  string            `json:"type"`
	Version               int               `json:"version,omitempty"`
	ID                    string            `json:"id,omitempty"`
	Timestamp             string            `json:"timestamp,omitempty"`
	CWD                   string            `json:"cwd,omitempty"`
	Message               *llm.Message      `json:"message,omitempty"`
	Summary               string            `json:"summary,omitempty"`
	FirstKeptMessageIndex int               `json:"firstKeptMessageIndex,omitempty"`
	TokensBefore          int               `json:"tokensBefore,omitempty"`
	CompactionMethod      string            `json:"compactionMethod,omitempty"`
	Operation             *ContextOperation `json:"operation,omitempty"`
}

type ContextOperation struct {
	Action       string `json:"action"`
	ContextIndex int    `json:"contextIndex"`
	Role         string `json:"role,omitempty"`
	Category     string `json:"category,omitempty"`
	BeforeHash   string `json:"beforeHash,omitempty"`
	AfterHash    string `json:"afterHash,omitempty"`
}

type contextMessageRef struct {
	Message      llm.Message
	EntryIndex   int
	MessageIndex int
	IsCompaction bool
}

// SessionStore persists and compacts conversation history in a pi-style append-only JSONL file.
//
// Sessions are grouped by agent and CWD and stored as separate JSONL files:
//
//	~/.synapta/sessions/<agent>/<encoded-cwd>/<timestamp>_<session-id>.jsonl
//
// The active session can be switched to support "new session" and "resume session" flows.
type SessionStore struct {
	mu         sync.Mutex
	baseDir    string
	agentID    string
	cwd        string
	sessionDir string
	filePath   string
	sessionID  string
	entries    []sessionEntry
	settings   CompactionSettings
	appendFile *os.File
}

func NewSessionStore(baseDir, agentID, cwd string, settings CompactionSettings) (*SessionStore, error) {
	s := &SessionStore{
		baseDir:    baseDir,
		agentID:    agentID,
		cwd:        cwd,
		sessionDir: sessionDirFor(baseDir, agentID, cwd),
		settings:   settings,
	}
	if err := os.MkdirAll(s.sessionDir, 0755); err != nil {
		return nil, fmt.Errorf("creating sessions dir: %w", err)
	}

	recent, err := findMostRecentSessionFile(s.sessionDir)
	if err != nil {
		return nil, err
	}
	if recent == "" {
		if err := s.startNewLocked(); err != nil {
			return nil, err
		}
		return s, nil
	}

	s.filePath = recent
	if err := s.loadFromFileLocked(); err != nil {
		return nil, err
	}
	return s, nil
}

func OpenSessionStore(baseDir, agentID, cwd, sessionPath string, settings CompactionSettings) (*SessionStore, error) {
	if strings.TrimSpace(sessionPath) == "" {
		return nil, fmt.Errorf("session path is required")
	}
	absPath, err := filepath.Abs(sessionPath)
	if err != nil {
		return nil, fmt.Errorf("resolve session path: %w", err)
	}
	s := &SessionStore{
		baseDir:    baseDir,
		agentID:    agentID,
		cwd:        cwd,
		sessionDir: sessionDirFor(baseDir, agentID, cwd),
		filePath:   absPath,
		settings:   settings,
	}
	if err := s.loadFromFileLocked(); err != nil {
		return nil, err
	}
	return s, nil
}

func ListAllSessions(baseDir, agentID string) ([]SessionInfo, error) {
	agentSessionsDir := filepath.Join(baseDir, "sessions", agentID)
	if _, err := os.Stat(agentSessionsDir); err != nil {
		if os.IsNotExist(err) {
			return []SessionInfo{}, nil
		}
		return nil, fmt.Errorf("stat sessions dir: %w", err)
	}

	dirs, err := os.ReadDir(agentSessionsDir)
	if err != nil {
		return nil, fmt.Errorf("read sessions dir: %w", err)
	}

	all := make([]SessionInfo, 0)
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		infos, err := listSessionsFromDir(filepath.Join(agentSessionsDir, d.Name()))
		if err != nil {
			continue
		}
		all = append(all, infos...)
	}
	sort.Slice(all, func(i, j int) bool {
		return all[i].Modified.After(all[j].Modified)
	})
	return all, nil
}

func listSessionsFromDir(dir string) ([]SessionInfo, error) {
	if _, err := os.Stat(dir); err != nil {
		if os.IsNotExist(err) {
			return []SessionInfo{}, nil
		}
		return nil, fmt.Errorf("stat sessions dir: %w", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read sessions dir: %w", err)
	}

	infos := make([]SessionInfo, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		info, err := buildSessionInfo(path)
		if err != nil {
			continue
		}
		infos = append(infos, info)
	}

	sort.Slice(infos, func(i, j int) bool {
		return infos[i].Modified.After(infos[j].Modified)
	})
	return infos, nil
}

func sessionDirFor(baseDir, agentID, cwd string) string {
	return filepath.Join(baseDir, "sessions", agentID, encodeCWD(cwd))
}

func encodeCWD(cwd string) string {
	repl := strings.NewReplacer("/", "-", "\\", "-", ":", "-", " ", "_")
	encoded := repl.Replace(strings.TrimSpace(cwd))
	if encoded == "" {
		return "default"
	}
	return strings.Trim(encoded, "-")
}

func findMostRecentSessionFile(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", fmt.Errorf("read sessions dir: %w", err)
	}

	type candidate struct {
		path    string
		modTime time.Time
	}
	candidates := make([]candidate, 0, len(entries))

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		if !isValidSessionFile(path) {
			continue
		}
		st, err := os.Stat(path)
		if err != nil {
			continue
		}
		candidates = append(candidates, candidate{path: path, modTime: st.ModTime()})
	}
	if len(candidates) == 0 {
		return "", nil
	}
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].modTime.After(candidates[j].modTime)
	})
	return candidates[0].path, nil
}

func isValidSessionFile(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	if !scanner.Scan() {
		return false
	}
	line := strings.TrimSpace(scanner.Text())
	if line == "" {
		return false
	}
	var e sessionEntry
	if err := json.Unmarshal([]byte(line), &e); err != nil {
		return false
	}
	return e.Type == "session" && strings.TrimSpace(e.ID) != ""
}

func buildSessionInfo(path string) (SessionInfo, error) {
	f, err := os.Open(path)
	if err != nil {
		return SessionInfo{}, err
	}
	defer f.Close()

	entries := make([]sessionEntry, 0, 64)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var e sessionEntry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			continue
		}
		entries = append(entries, e)
	}
	if err := scanner.Err(); err != nil {
		return SessionInfo{}, err
	}
	if len(entries) == 0 || entries[0].Type != "session" {
		return SessionInfo{}, fmt.Errorf("invalid session file")
	}

	header := entries[0]
	created, _ := time.Parse(time.RFC3339, header.Timestamp)
	if created.IsZero() {
		created = time.Now()
	}

	messageCount := 0
	firstMessage := ""
	for _, e := range entries {
		if e.Type != "message" || e.Message == nil {
			continue
		}
		if e.Message.Role != "user" && e.Message.Role != "assistant" {
			continue
		}
		messageCount++
		if firstMessage == "" && e.Message.Role == "user" {
			firstMessage = strings.TrimSpace(e.Message.Content)
		}
	}
	if firstMessage == "" {
		firstMessage = "(no messages)"
	}

	st, err := os.Stat(path)
	if err != nil {
		return SessionInfo{}, err
	}
	modified := st.ModTime()

	return SessionInfo{
		Path:         path,
		ID:           header.ID,
		CWD:          header.CWD,
		Created:      created,
		Modified:     modified,
		MessageCount: messageCount,
		FirstMessage: firstMessage,
	}, nil
}

func (s *SessionStore) StartNewSession() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.startNewLocked()
}

func (s *SessionStore) startNewLocked() error {
	if err := s.closeAppendFileLocked(); err != nil {
		return err
	}
	timestamp := time.Now().UTC()
	s.sessionID = fmt.Sprintf("%d", timestamp.UnixNano())
	fileTime := strings.NewReplacer(":", "-", ".", "-").Replace(timestamp.Format(time.RFC3339Nano))
	s.filePath = filepath.Join(s.sessionDir, fileTime+"_"+s.sessionID+".jsonl")

	header := sessionEntry{
		Type:      "session",
		Version:   currentSessionVersion,
		ID:        s.sessionID,
		Timestamp: timestamp.Format(time.RFC3339),
		CWD:       s.cwd,
	}
	s.entries = []sessionEntry{header}
	return s.rewriteAllLocked()
}

func (s *SessionStore) OpenSession(sessionPath string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	absPath, err := filepath.Abs(sessionPath)
	if err != nil {
		return fmt.Errorf("resolve session path: %w", err)
	}
	s.filePath = absPath
	return s.loadFromFileLocked()
}

func (s *SessionStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closeAppendFileLocked()
}

func (s *SessionStore) loadFromFileLocked() error {
	if err := s.closeAppendFileLocked(); err != nil {
		return err
	}

	f, err := os.Open(s.filePath)
	if err != nil {
		return fmt.Errorf("open session file: %w", err)
	}
	defer f.Close()

	s.entries = make([]sessionEntry, 0)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var e sessionEntry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			continue
		}
		s.entries = append(s.entries, e)
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("scan session file: %w", err)
	}
	if len(s.entries) == 0 || s.entries[0].Type != "session" {
		return fmt.Errorf("invalid session file: missing session header")
	}
	header := s.entries[0]
	s.sessionID = header.ID
	if strings.TrimSpace(header.CWD) != "" {
		s.cwd = header.CWD
	}
	return nil
}

func (s *SessionStore) SessionID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sessionID
}

func (s *SessionStore) SessionFile() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.filePath
}

func (s *SessionStore) CWD() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cwd
}

func (s *SessionStore) ContextOperations() []ContextOperation {
	s.mu.Lock()
	defer s.mu.Unlock()
	ops := make([]ContextOperation, 0)
	for _, e := range s.entries {
		if e.Type == "context_op" && e.Operation != nil {
			ops = append(ops, *e.Operation)
		}
	}
	return ops
}

func (s *SessionStore) AppendMessage(msg llm.Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry := sessionEntry{
		Type:      "message",
		Timestamp: time.Now().Format(time.RFC3339),
		Message:   &msg,
	}
	s.entries = append(s.entries, entry)
	return s.appendEntryLocked(entry)
}

func (s *SessionStore) ContextMessages() []llm.Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.contextMessagesLocked()
}

func (s *SessionStore) ContextMessageTimestamps() []time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	refs := s.contextMessageRefsLocked()
	out := make([]time.Time, 0, len(refs))
	for _, ref := range refs {
		ts := time.Time{}
		if ref.EntryIndex >= 0 && ref.EntryIndex < len(s.entries) {
			raw := strings.TrimSpace(s.entries[ref.EntryIndex].Timestamp)
			if raw != "" {
				if parsed, err := time.Parse(time.RFC3339, raw); err == nil {
					ts = parsed
				}
			}
		}
		out = append(out, ts)
	}
	return out
}

func (s *SessionStore) contextMessagesLocked() []llm.Message {
	refs := s.contextMessageRefsLocked()
	out := make([]llm.Message, 0, len(refs))
	for _, ref := range refs {
		out = append(out, ref.Message)
	}
	return out
}

func (s *SessionStore) contextMessageRefsLocked() []contextMessageRef {
	messages := s.messageEntriesLocked()
	messageEntryIndices := s.messageEntryIndicesLocked()
	compactions := s.compactionEntriesLocked()
	if len(compactions) == 0 {
		out := make([]contextMessageRef, 0, len(messages))
		for i, msg := range messages {
			entryIdx := -1
			if i >= 0 && i < len(messageEntryIndices) {
				entryIdx = messageEntryIndices[i]
			}
			out = append(out, contextMessageRef{Message: msg, EntryIndex: entryIdx, MessageIndex: i})
		}
		return out
	}

	start := compactions[len(compactions)-1].FirstKeptMessageIndex
	if start < 0 {
		start = 0
	}
	if start > len(messages) {
		start = len(messages)
	}

	result := make([]contextMessageRef, 0, len(compactions)+len(messages)-start)
	for _, compaction := range compactions {
		result = append(result, contextMessageRef{
			Message:      llm.Message{Role: "user", Content: compactionSummaryPrefix + strings.TrimSpace(compaction.Summary) + compactionSummarySuffix},
			EntryIndex:   -1,
			MessageIndex: -1,
			IsCompaction: true,
		})
	}
	for i := start; i < len(messages); i++ {
		entryIdx := -1
		if i >= 0 && i < len(messageEntryIndices) {
			entryIdx = messageEntryIndices[i]
		}
		result = append(result, contextMessageRef{Message: messages[i], EntryIndex: entryIdx, MessageIndex: i})
	}
	return result
}

func (s *SessionStore) messageEntryIndicesLocked() []int {
	indices := make([]int, 0)
	for i, e := range s.entries {
		if e.Type != "message" || e.Message == nil {
			continue
		}
		if !isDynamicContextMessage(*e.Message) {
			continue
		}
		indices = append(indices, i)
	}
	return indices
}

func (s *SessionStore) UpdateContextMessageAt(index int, content string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	refs := s.contextMessageRefsLocked()
	if index < 0 || index >= len(refs) {
		return fmt.Errorf("context index out of range")
	}
	ref := refs[index]
	if ref.IsCompaction || ref.EntryIndex < 0 {
		return fmt.Errorf("selected context entry is not editable")
	}
	if ref.EntryIndex >= len(s.entries) || s.entries[ref.EntryIndex].Type != "message" || s.entries[ref.EntryIndex].Message == nil {
		return fmt.Errorf("selected context entry not found")
	}
	before := s.entries[ref.EntryIndex].Message.Content
	s.entries[ref.EntryIndex].Message.Content = content
	op := sessionEntry{
		Type:      "context_op",
		Timestamp: time.Now().Format(time.RFC3339),
		Operation: &ContextOperation{
			Action:       "edit",
			ContextIndex: index,
			Role:         ref.Message.Role,
			BeforeHash:   hashText(before),
			AfterHash:    hashText(content),
		},
	}
	s.entries = append(s.entries, op)
	return s.rewriteAllLocked()
}

func (s *SessionStore) RemoveContextMessageAt(index int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	refs := s.contextMessageRefsLocked()
	if index < 0 || index >= len(refs) {
		return fmt.Errorf("context index out of range")
	}
	ref := refs[index]
	if ref.IsCompaction || ref.EntryIndex < 0 {
		return fmt.Errorf("selected context entry is not removable")
	}
	if ref.EntryIndex >= len(s.entries) || s.entries[ref.EntryIndex].Type != "message" || s.entries[ref.EntryIndex].Message == nil {
		return fmt.Errorf("selected context entry not found")
	}

	removed := s.entries[ref.EntryIndex].Message
	s.entries = append(s.entries[:ref.EntryIndex], s.entries[ref.EntryIndex+1:]...)
	if ref.MessageIndex >= 0 {
		for i := range s.entries {
			if s.entries[i].Type != "compaction" {
				continue
			}
			if s.entries[i].FirstKeptMessageIndex > ref.MessageIndex {
				s.entries[i].FirstKeptMessageIndex--
			}
		}
	}
	op := sessionEntry{
		Type:      "context_op",
		Timestamp: time.Now().Format(time.RFC3339),
		Operation: &ContextOperation{
			Action:       "remove",
			ContextIndex: index,
			Role:         removed.Role,
			BeforeHash:   hashText(removed.Content),
		},
	}
	s.entries = append(s.entries, op)
	return s.rewriteAllLocked()
}

func (s *SessionStore) CompactIfNeeded(ctx context.Context, contextWindow int, summarizer CompactionSummarizer) (bool, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.compactLocked(ctx, false, contextWindow, summarizer)
}

func (s *SessionStore) ManualCompact(ctx context.Context, contextWindow int, summarizer CompactionSummarizer) (bool, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.compactLocked(ctx, true, contextWindow, summarizer)
}

func (s *SessionStore) compactLocked(ctx context.Context, force bool, contextWindow int, summarizer CompactionSummarizer) (bool, string, error) {
	if !s.settings.Enabled && !force {
		return false, "", nil
	}

	messages := s.messageEntriesLocked()
	latest := s.latestCompactionLocked()
	start := 0
	previousSummary := ""
	if latest != nil {
		start = latest.FirstKeptMessageIndex
		if start < 0 {
			start = 0
		}
		if start > len(messages) {
			start = len(messages)
		}
		previousSummary = strings.TrimSpace(latest.Summary)
	}

	currentSlice := messages[start:]
	contextSlice := s.contextMessagesLocked()
	if len(contextSlice) < 2 {
		return false, "", nil
	}

	tokensBefore := estimateContextTokens(currentSlice)
	if force {
		tokensBefore = estimateContextTokens(contextSlice)
	}
	if previousSummary != "" {
		tokensBefore += llm.EstimateTextTokens(compactionSummaryPrefix + previousSummary + compactionSummarySuffix)
	}

	if !force {
		if tokensBefore <= contextWindow-s.settings.ReserveTokens {
			return false, "", nil
		}
	}

	keepRel := len(currentSlice)
	if !force {
		keepRel = findKeepStartIndex(currentSlice, s.settings.KeepRecentTokens)
		if keepRel <= 0 || keepRel >= len(currentSlice) {
			keepRel = len(currentSlice) / 2
		}
	}
	if keepRel <= 0 {
		return false, "", nil
	}
	if keepRel > len(currentSlice) {
		keepRel = len(currentSlice)
	}

	toSummarize := currentSlice[:keepRel]
	if force {
		toSummarize = contextSlice
	}
	method := compactionMethodModel
	newSummary := ""
	if summarizer != nil {
		var err error
		newSummary, err = summarizer(ctx, toSummarize, previousSummary)
		if err != nil {
			return false, "", err
		}
	}
	if strings.TrimSpace(newSummary) == "" {
		method = compactionMethodDeterministic
		newSummary = summarizeMessagesDeterministic(toSummarize)
	}

	summary := strings.TrimSpace(newSummary)

	compaction := sessionEntry{
		Type:                  "compaction",
		Timestamp:             time.Now().Format(time.RFC3339),
		Summary:               summary,
		FirstKeptMessageIndex: start + keepRel,
		TokensBefore:          tokensBefore,
		CompactionMethod:      method,
	}
	s.entries = append(s.entries, compaction)
	if err := s.appendEntryLocked(compaction); err != nil {
		return false, "", err
	}
	return true, method, nil
}

func (s *SessionStore) messageEntriesLocked() []llm.Message {
	out := make([]llm.Message, 0)
	for _, e := range s.entries {
		if e.Type != "message" || e.Message == nil {
			continue
		}
		if !isDynamicContextMessage(*e.Message) {
			continue
		}
		out = append(out, *e.Message)
	}
	return out
}

func isDynamicContextMessage(msg llm.Message) bool {
	hasContent := strings.TrimSpace(msg.Content) != ""

	switch msg.Role {
	case "assistant":
		return hasContent || len(msg.ToolCalls) > 0
	case "tool":
		return hasContent || strings.TrimSpace(msg.ToolCallID) != "" || strings.TrimSpace(msg.Name) != ""
	case "user", "system":
		return hasContent
	default:
		return false
	}
}

func (s *SessionStore) latestCompactionLocked() *sessionEntry {
	for i := len(s.entries) - 1; i >= 0; i-- {
		if s.entries[i].Type == "compaction" {
			copyEntry := s.entries[i]
			return &copyEntry
		}
	}
	return nil
}

func (s *SessionStore) compactionEntriesLocked() []sessionEntry {
	entries := make([]sessionEntry, 0)
	for i := 0; i < len(s.entries); i++ {
		if s.entries[i].Type != "compaction" {
			continue
		}
		entries = append(entries, s.entries[i])
	}
	return entries
}

func (s *SessionStore) appendEntryLocked(e sessionEntry) error {
	if err := s.ensureAppendFileLocked(); err != nil {
		return err
	}
	data, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("marshal session entry: %w", err)
	}
	if _, err := s.appendFile.WriteString(string(data) + "\n"); err != nil {
		_ = s.closeAppendFileLocked()
		return fmt.Errorf("append session entry: %w", err)
	}
	return nil
}

func (s *SessionStore) ensureAppendFileLocked() error {
	if s.appendFile != nil {
		return nil
	}
	f, err := os.OpenFile(s.filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("open session file append: %w", err)
	}
	s.appendFile = f
	return nil
}

func (s *SessionStore) closeAppendFileLocked() error {
	if s.appendFile == nil {
		return nil
	}
	if err := s.appendFile.Close(); err != nil {
		s.appendFile = nil
		return fmt.Errorf("close session append file: %w", err)
	}
	s.appendFile = nil
	return nil
}

func (s *SessionStore) rewriteAllLocked() error {
	if err := s.closeAppendFileLocked(); err != nil {
		return err
	}

	f, err := os.Create(s.filePath)
	if err != nil {
		return fmt.Errorf("create session file: %w", err)
	}
	defer f.Close()
	for _, e := range s.entries {
		data, err := json.Marshal(e)
		if err != nil {
			return fmt.Errorf("marshal session entry: %w", err)
		}
		if _, err := f.WriteString(string(data) + "\n"); err != nil {
			return fmt.Errorf("write session entry: %w", err)
		}
	}
	return nil
}

func findKeepStartIndex(messages []llm.Message, keepTokens int) int {
	if keepTokens <= 0 {
		return len(messages)
	}
	acc := 0
	for i := len(messages) - 1; i >= 0; i-- {
		acc += estimateMessageTokens(messages[i])
		if acc >= keepTokens {
			return i
		}
	}
	return 0
}

func estimateContextTokens(messages []llm.Message) int {
	t := 0
	for _, m := range messages {
		t += estimateMessageTokens(m)
	}
	return t
}

func estimateMessageTokens(msg llm.Message) int {
	return llm.EstimateMessageTokens(msg)
}

func summarizeMessagesDeterministic(messages []llm.Message) string {
	base := "## Goal\nContinue the session with preserved context.\n\n## Progress\n"
	base += fmt.Sprintf("Messages summarized: %d\n", len(messages))
	base += "\n## Key Context\n"
	for i, m := range messages {
		if i >= 10 {
			break
		}
		content := strings.ReplaceAll(strings.TrimSpace(m.Content), "\n", " ")
		if len(content) > 200 {
			content = content[:200] + "..."
		}
		base += fmt.Sprintf("- [%s] %s\n", strings.ToUpper(m.Role), content)
	}
	return strings.TrimSpace(base)
}
