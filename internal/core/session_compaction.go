package core

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ashiqrniloy/synapta-cli/internal/llm"
)

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
