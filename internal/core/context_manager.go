package core

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/synapta/synapta-cli/internal/llm"
)

// ContextManager builds the full message context sent to the LLM.
//
// It mirrors pi's defaults:
// - prepend system prompt every turn (if any)
// - include project context files in the system prompt
// - append current date and working directory to system prompt
// - include complete in-session conversation history messages
//
// This is generic and can be reused by future Synapta agents.
type ContextManager struct {
	agentID           string
	agentDir          string
	cwd               string
	systemPromptStore *SystemPromptStore
}

func NewContextManager(agentID, agentDir, cwd string, systemPromptStore *SystemPromptStore) *ContextManager {
	return &ContextManager{
		agentID:           agentID,
		agentDir:          agentDir,
		cwd:               cwd,
		systemPromptStore: systemPromptStore,
	}
}

func (m *ContextManager) SetCWD(cwd string) {
	m.cwd = cwd
}

// Build returns the exact messages to send to the LLM for the next interaction.
func (m *ContextManager) Build(history []llm.Message) ([]llm.Message, error) {
	messages := make([]llm.Message, 0, len(history)+1)

	systemPrompt, err := m.buildSystemPrompt()
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(systemPrompt) != "" {
		messages = append(messages, llm.Message{Role: "system", Content: systemPrompt})
	}

	for _, msg := range history {
		if !isContextRole(msg.Role) {
			continue
		}
		if strings.TrimSpace(msg.Content) == "" {
			continue
		}
		messages = append(messages, llm.Message{Role: msg.Role, Content: msg.Content})
	}

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

func (m *ContextManager) buildSystemPrompt() (string, error) {
	if m.systemPromptStore == nil {
		return "", nil
	}

	basePrompt, err := m.systemPromptStore.Load(m.agentID)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(basePrompt) == "" {
		return "", nil
	}

	var b strings.Builder
	b.WriteString(strings.TrimSpace(basePrompt))

	contextFiles := loadProjectContextFiles(m.agentDir, m.cwd)
	if len(contextFiles) > 0 {
		b.WriteString("\n\n# Project Context\n\n")
		b.WriteString("Project-specific instructions and guidelines:\n\n")
		for _, cf := range contextFiles {
			b.WriteString("## ")
			b.WriteString(cf.Path)
			b.WriteString("\n\n")
			b.WriteString(strings.TrimSpace(cf.Content))
			b.WriteString("\n\n")
		}
	}

	promptCwd := strings.ReplaceAll(m.cwd, "\\", "/")
	date := time.Now().Format("2006-01-02")
	b.WriteString("\nCurrent date: ")
	b.WriteString(date)
	b.WriteString("\nCurrent working directory: ")
	b.WriteString(promptCwd)

	return b.String(), nil
}

type contextFile struct {
	Path    string
	Content string
}

func loadProjectContextFiles(agentDir, cwd string) []contextFile {
	files := make([]contextFile, 0)
	seen := make(map[string]struct{})

	if cf, ok := loadContextFileFromDir(agentDir); ok {
		files = append(files, cf)
		seen[cf.Path] = struct{}{}
	}

	ancestorFiles := make([]contextFile, 0)
	currentDir := cwd
	for {
		if cf, ok := loadContextFileFromDir(currentDir); ok {
			if _, exists := seen[cf.Path]; !exists {
				ancestorFiles = append([]contextFile{cf}, ancestorFiles...)
				seen[cf.Path] = struct{}{}
			}
		}

		parent := filepath.Dir(currentDir)
		if parent == currentDir {
			break
		}
		currentDir = parent
	}

	files = append(files, ancestorFiles...)
	return files
}

func loadContextFileFromDir(dir string) (contextFile, bool) {
	if strings.TrimSpace(dir) == "" {
		return contextFile{}, false
	}

	candidates := []string{"AGENTS.md", "CLAUDE.md"}
	for _, name := range candidates {
		p := filepath.Join(dir, name)
		data, err := os.ReadFile(p)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			continue
		}
		return contextFile{Path: p, Content: string(data)}, true
	}

	return contextFile{}, false
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
	return fmt.Sprintf("context messages: %d", len(msgs)), nil
}
