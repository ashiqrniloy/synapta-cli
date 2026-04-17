package core

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/ashiqrniloy/synapta-cli/internal/llm"
)

const (
	CompactionPromptFileName = "compaction.md"

	defaultCompactionPrompt = `You are performing manual context compaction for this session.

The conversation above is the exact working context. Generate the next structured compaction checkpoint summary.

If previous compaction summaries exist, continue from them as a checkpoint chain. Do not restart from scratch and do not contradict earlier checkpoints.

Use this EXACT format:

## Goal
[What is the user trying to accomplish? Can be multiple items if the session covers different tasks.]

## Constraints & Preferences
- [Any constraints, preferences, or requirements mentioned by user]
- [Or "(none)" if none were mentioned]

## Progress
### Done
- [x] [Completed tasks/changes]

### In Progress
- [ ] [Current work]

### Blocked
[Issues preventing progress, if any]

## Key Decisions
- **[Decision]**: [Brief rationale]

## Next Steps
1. [Ordered list of what should happen next]

## Critical Context
- [Any data, examples, or references needed to continue]
- [Or "(none)" if not applicable]

Keep each section concise. Preserve exact file paths, function names, and error messages.`
)

var (
	compactionPromptMu sync.RWMutex
	compactionPrompt   = defaultCompactionPrompt
)

func CurrentCompactionPrompt() string {
	compactionPromptMu.RLock()
	defer compactionPromptMu.RUnlock()
	if strings.TrimSpace(compactionPrompt) == "" {
		return defaultCompactionPrompt
	}
	return compactionPrompt
}

func SetCompactionPrompt(prompt string) {
	trimmed := strings.TrimSpace(prompt)
	if trimmed == "" {
		trimmed = defaultCompactionPrompt
	}
	compactionPromptMu.Lock()
	compactionPrompt = trimmed
	compactionPromptMu.Unlock()
}

func LoadCompactionPrompt(agentDir string) error {
	if strings.TrimSpace(agentDir) == "" {
		return fmt.Errorf("agent dir is required")
	}

	promptDir := filepath.Join(agentDir, "system-prompts", AgentCode)
	if err := os.MkdirAll(promptDir, 0755); err != nil {
		return fmt.Errorf("creating compaction prompt dir: %w", err)
	}

	promptPath := filepath.Join(promptDir, CompactionPromptFileName)
	if _, err := os.Stat(promptPath); err != nil {
		if os.IsNotExist(err) {
			if err := os.WriteFile(promptPath, []byte(strings.TrimSpace(defaultCompactionPrompt)+"\n"), 0644); err != nil {
				return fmt.Errorf("writing default compaction prompt: %w", err)
			}
			SetCompactionPrompt(defaultCompactionPrompt)
			return nil
		}
		return fmt.Errorf("checking compaction prompt file: %w", err)
	}

	data, err := os.ReadFile(promptPath)
	if err != nil {
		return fmt.Errorf("reading compaction prompt file: %w", err)
	}
	SetCompactionPrompt(string(data))
	return nil
}

// BuildCompactionRequestMessages appends a compaction instruction message to the end
// of the already-built conversation context so compaction uses the same request shape
// and prefix as normal task requests.
//
// The instruction is appended as a "user" role message because some providers (e.g., Google Vertex AI)
// require that the conversation must end with a user message.
func BuildCompactionRequestMessages(messages []llm.Message, previousSummary string) []llm.Message {
	out := make([]llm.Message, 0, len(messages)+1)
	out = append(out, messages...)

	instruction := strings.TrimSpace(CurrentCompactionPrompt())
	if strings.TrimSpace(previousSummary) != "" {
		instruction += "\n\nPrevious compaction summary exists in this session. Continue from it as a strict continuation, adding only new or changed facts when possible."
	}
	// Use "user" role so the conversation ends with a user message.
	// This is required by providers like Google Vertex AI that do not support assistant message prefill.
	out = append(out, llm.Message{Role: llm.RoleUser, Content: instruction})
	return out
}
