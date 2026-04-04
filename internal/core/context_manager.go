package core

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/synapta/synapta-cli/internal/llm"
)

// ContextManager builds the full message context sent to the LLM.
//
// Prompt assembly is layered to maximize prompt-prefix stability:
// 1) Stable system prefix (agent prompt + project context + skills catalog)
// 2) Dynamic conversation history
//
// Stable fragments are cached and invalidated explicitly when environment changes.
type ContextManager struct {
	agentID           string
	agentDir          string
	cwd               string
	systemPromptStore *SystemPromptStore

	mu sync.Mutex

	skillCache *SkillCatalogCache

	stablePrefixFragment fragmentCache
	projectFragment      fragmentCache
	skillsFragment       fragmentCache

	stablePrefixDirty bool
	projectDirty      bool
	skillsDirty       bool

	lastFingerprint PromptFingerprint
}

type fragmentCache struct {
	signature string
	content   string
}

type PromptFingerprint struct {
	StablePrefixHash string
	MetadataHash     string
	HistoryHash      string
	PromptHash       string
	MessageCount     int
}

func NewContextManager(agentID, agentDir, cwd string, systemPromptStore *SystemPromptStore) *ContextManager {
	return &ContextManager{
		agentID:              agentID,
		agentDir:             agentDir,
		cwd:                  cwd,
		systemPromptStore:    systemPromptStore,
		skillCache:           NewSkillCatalogCache(),
		stablePrefixDirty:    true,
		projectDirty:         true,
		skillsDirty:          true,
		lastFingerprint:      PromptFingerprint{},
		stablePrefixFragment: fragmentCache{},
		projectFragment:      fragmentCache{},
		skillsFragment:       fragmentCache{},
	}
}

func (m *ContextManager) SetCWD(cwd string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cwd == cwd {
		return
	}
	m.cwd = cwd
	m.projectDirty = true
	m.skillsDirty = true
	m.stablePrefixDirty = true
}

func (m *ContextManager) InvalidateSystemPrompt() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stablePrefixDirty = true
}

func (m *ContextManager) InvalidateProjectContext() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.projectDirty = true
	m.stablePrefixDirty = true
}

func (m *ContextManager) InvalidateSkills() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.skillsDirty = true
	m.stablePrefixDirty = true
	if m.skillCache != nil {
		m.skillCache.Invalidate()
	}
}

func (m *ContextManager) LastPromptFingerprint() PromptFingerprint {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lastFingerprint
}

// Build returns the exact messages to send to the LLM for the next interaction.
func (m *ContextManager) Build(history []llm.Message) ([]llm.Message, error) {
	stablePrefix, err := m.buildStablePrefix()
	if err != nil {
		return nil, err
	}
	metadata := m.buildRuntimeMetadata()

	messages := make([]llm.Message, 0, len(history)+2)
	if strings.TrimSpace(stablePrefix) != "" {
		messages = append(messages, llm.Message{Role: "system", Content: stablePrefix})
	}
	if strings.TrimSpace(metadata) != "" {
		messages = append(messages, llm.Message{Role: "system", Content: metadata})
	}

	for _, msg := range history {
		if !isContextRole(msg.Role) {
			continue
		}
		if strings.TrimSpace(msg.Content) == "" {
			continue
		}
		messages = append(messages, llm.Message{Role: msg.Role, Content: msg.Content, Name: msg.Name, ToolCallID: msg.ToolCallID})
	}

	m.mu.Lock()
	m.lastFingerprint = PromptFingerprint{
		StablePrefixHash: hashText(stablePrefix),
		MetadataHash:     hashText(metadata),
		HistoryHash:      hashMessages(history),
		PromptHash:       hashMessages(messages),
		MessageCount:     len(messages),
	}
	m.mu.Unlock()

	return messages, nil
}

func isContextRole(role string) bool {
	switch role {
	case "user", "assistant", "tool":
		return true
	default:
		return false
	}
}

func (m *ContextManager) buildStablePrefix() (string, error) {
	m.mu.Lock()
	cwd := m.cwd
	agentDir := m.agentDir
	agentID := m.agentID
	store := m.systemPromptStore
	skillCache := m.skillCache
	m.mu.Unlock()

	if store == nil {
		return "", nil
	}

	basePrompt, err := store.Load(agentID)
	if err != nil {
		return "", err
	}
	basePrompt = strings.TrimSpace(basePrompt)
	if basePrompt == "" {
		return "", nil
	}

	projectPaths := discoverProjectContextPaths(agentDir, cwd)
	projectSig := signatureFromPaths(projectPaths)

	skillOptions := LoadSkillsOptions{CWD: cwd, AgentDir: agentDir, IncludeDefaults: true}
	skillsSig := skillsLoadSignature(skillOptions)

	m.mu.Lock()
	if m.projectFragment.signature != projectSig || m.projectDirty {
		m.projectFragment.content = renderProjectContextFromPaths(projectPaths)
		m.projectFragment.signature = projectSig
		m.projectDirty = false
		m.stablePrefixDirty = true
	}
	if m.skillsFragment.signature != skillsSig || m.skillsDirty {
		var skillsResult SkillsResult
		if skillCache != nil {
			skillsResult = skillCache.Load(skillOptions)
		} else {
			skillsResult = LoadSkills(skillOptions)
		}
		m.skillsFragment.content = FormatSkillsForPrompt(skillsResult.Skills)
		m.skillsFragment.signature = skillsSig
		m.skillsDirty = false
		m.stablePrefixDirty = true
	}

	stableSig := hashText(basePrompt + "\n" + m.projectFragment.signature + "\n" + m.skillsFragment.signature)
	if !m.stablePrefixDirty && m.stablePrefixFragment.signature == stableSig {
		content := m.stablePrefixFragment.content
		m.mu.Unlock()
		return content, nil
	}

	var b strings.Builder
	b.WriteString(basePrompt)
	if strings.TrimSpace(m.projectFragment.content) != "" {
		b.WriteString("\n\n")
		b.WriteString(strings.TrimSpace(m.projectFragment.content))
	}
	if strings.TrimSpace(m.skillsFragment.content) != "" {
		b.WriteString("\n")
		b.WriteString(strings.TrimSpace(m.skillsFragment.content))
	}

	m.stablePrefixFragment = fragmentCache{signature: stableSig, content: b.String()}
	m.stablePrefixDirty = false
	content := m.stablePrefixFragment.content
	m.mu.Unlock()
	return content, nil
}

func (m *ContextManager) buildRuntimeMetadata() string {
	return ""
}

func discoverProjectContextPaths(agentDir, cwd string) []string {
	paths := make([]string, 0)
	seen := map[string]struct{}{}

	if p, ok := findContextFileInDir(agentDir); ok {
		paths = append(paths, p)
		seen[p] = struct{}{}
	}

	ancestorPaths := make([]string, 0)
	currentDir := cwd
	for {
		if p, ok := findContextFileInDir(currentDir); ok {
			if _, exists := seen[p]; !exists {
				ancestorPaths = append([]string{p}, ancestorPaths...)
				seen[p] = struct{}{}
			}
		}
		parent := filepath.Dir(currentDir)
		if parent == currentDir {
			break
		}
		currentDir = parent
	}

	paths = append(paths, ancestorPaths...)
	return paths
}

func findContextFileInDir(dir string) (string, bool) {
	if strings.TrimSpace(dir) == "" {
		return "", false
	}
	for _, name := range []string{"AGENTS.md", "CLAUDE.md"} {
		p := filepath.Join(dir, name)
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p, true
		}
	}
	return "", false
}

func renderProjectContextFromPaths(paths []string) string {
	if len(paths) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("# Project Context\n\n")
	b.WriteString("Project-specific instructions and guidelines:\n\n")
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		b.WriteString("## ")
		b.WriteString(p)
		b.WriteString("\n\n")
		b.WriteString(strings.TrimSpace(string(data)))
		b.WriteString("\n\n")
	}
	return strings.TrimSpace(b.String())
}

func signatureFromPaths(paths []string) string {
	if len(paths) == 0 {
		return "empty"
	}
	sigs := make([]string, 0, len(paths))
	for _, p := range paths {
		sigs = append(sigs, fileStatSignature(p))
	}
	sort.Strings(sigs)
	return strings.Join(sigs, "|")
}

func hashMessages(messages []llm.Message) string {
	if len(messages) == 0 {
		return hashText("")
	}
	var b strings.Builder
	for _, m := range messages {
		b.WriteString("<")
		b.WriteString(m.Role)
		b.WriteString(":")
		b.WriteString(m.Name)
		b.WriteString(":")
		b.WriteString(m.ToolCallID)
		b.WriteString(">")
		b.WriteString(m.Content)
		b.WriteString("\n")
	}
	return hashText(b.String())
}

func hashText(text string) string {
	s := sha256.Sum256([]byte(text))
	return hex.EncodeToString(s[:])
}

// BashExecutionToText formats a shell command execution as a user-context message.
func BashExecutionToText(command, output string, failed bool) string {
	text := fmt.Sprintf("Ran `%s`\n", strings.TrimSpace(command))
	trimmed := strings.TrimSpace(output)
	if trimmed != "" {
		text += "```\n" + trimmed + "\n```"
	} else {
		text += "(no output)"
	}
	if failed {
		text += "\n\nCommand failed"
	}
	return text
}

func (m *ContextManager) DebugDescribe(history []llm.Message) (string, error) {
	msgs, err := m.Build(history)
	if err != nil {
		return "", err
	}
	fp := m.LastPromptFingerprint()
	return fmt.Sprintf("context messages: %d stable=%s metadata=%s history=%s", len(msgs), fp.StablePrefixHash[:12], fp.MetadataHash[:12], fp.HistoryHash[:12]), nil
}
