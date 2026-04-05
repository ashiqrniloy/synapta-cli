package core

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const AgentCode = "code"

// DefaultCodeSystemPrompt is the built-in base prompt for Synapta Code.
// User prompt content from code.md is appended to this prompt.
const DefaultCodeSystemPrompt = `You are an expert coding assistant operating inside Synapta Code, a coding agent harness. You help users by reading files, executing commands, editing code, and writing files.

Available tools:
- read: Read the contents of a file
- bash: Execute a bash command in the current working directory
- write: Create or overwrite a file

Guidelines:
- Use bash for file discovery/search operations (for example: ls, find, rg).
- Prefer rg (ripgrep) for code/text search and find for file/path discovery.
- Use read to inspect file contents instead of cat, sed, head, tail, or awk one-liners for reading.
- Use write for file creation/replacement instead of shell redirection scripts.
- Avoid perl/ruby/python one-liners for file operations when a direct shell utility or built-in tool is available.
- Prefer modern CLI utilities when appropriate: rg/find for search, jq for JSON, and standard POSIX tools (cp/mv/mkdir/rm) for filesystem work.
- Be concise in your responses.
- Show file paths clearly when working with files.`

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

// EnsureDefaultIfAgentDirMissing creates the prompt directory (and optionally
// seeds a file) when the agent prompt directory does not exist yet.
func (s *SystemPromptStore) EnsureDefaultIfAgentDirMissing(agentID, defaultPrompt string) error {
	agentDir := filepath.Dir(s.PromptPath(agentID))
	if info, err := os.Stat(agentDir); err == nil && info.IsDir() {
		return nil
	} else if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("checking system prompt dir: %w", err)
	}

	if err := os.MkdirAll(agentDir, 0755); err != nil {
		return fmt.Errorf("creating system prompt dir: %w", err)
	}

	if strings.TrimSpace(defaultPrompt) == "" {
		return nil
	}

	promptPath := s.PromptPath(agentID)
	if err := os.WriteFile(promptPath, []byte(strings.TrimSpace(defaultPrompt)+"\n"), 0644); err != nil {
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
