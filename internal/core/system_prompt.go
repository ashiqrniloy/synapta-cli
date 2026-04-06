package core

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const AgentCode = "code"

// DefaultCodeSystemPrompt is the user-editable default prompt seeded at
// ~/.synapta/system-prompts/code/code.md when missing.
const DefaultCodeSystemPrompt = `You are a coding agent harness. You help users by executing commands, reading files and editing code and markdown files

Available Tools:
BASH: Execute shell commands
READ: Read file contents
WRITE: Create or edit files

Guidelines:
1. Use rg and find command to find relevant files based on the user request.
2. Use read command to read or inspect file contents.
3. Use patch or ed command to write to files.
4. Be concise in your responses.
5. Show full file paths when using READ and WRITE tools.`

// SystemPromptStore manages user-editable per-agent system prompt files.
type SystemPromptStore struct {
	baseDir string
}

func NewSystemPromptStore(baseDir string) *SystemPromptStore {
	return &SystemPromptStore{baseDir: baseDir}
}

func (s *SystemPromptStore) PromptPath(agentID string) string {
	return filepath.Join(s.baseDir, "system-prompts", agentID, agentID+".md")
}

// EnsureDefaultIfAgentDirMissing ensures the prompt directory exists and creates
// the default prompt file when missing. Existing files are never overwritten.
func (s *SystemPromptStore) EnsureDefaultIfAgentDirMissing(agentID, defaultPrompt string) error {
	agentDir := filepath.Dir(s.PromptPath(agentID))
	if err := os.MkdirAll(agentDir, 0755); err != nil {
		return fmt.Errorf("creating system prompt dir: %w", err)
	}

	promptPath := s.PromptPath(agentID)
	if _, err := os.Stat(promptPath); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("checking system prompt file: %w", err)
	}

	content := strings.TrimSpace(defaultPrompt)
	if content == "" {
		content = strings.TrimSpace(DefaultCodeSystemPrompt)
	}
	if err := os.WriteFile(promptPath, []byte(content+"\n"), 0644); err != nil {
		return fmt.Errorf("writing default system prompt: %w", err)
	}
	return nil
}

// Load returns the prompt content for an agent.
// Missing file means no system prompt ("", nil).
func (s *SystemPromptStore) Load(agentID string) (string, error) {
	promptPath := s.PromptPath(agentID)
	data, err := os.ReadFile(promptPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("reading system prompt %q: %w", promptPath, err)
	}

	content := strings.TrimSpace(string(data))
	if content == "" {
		return "", nil
	}
	return content, nil
}
