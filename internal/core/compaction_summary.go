package core

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/ashiqrniloy/synapta-cli/internal/llm"
)

const CompactionSummarizationSystemPrompt = "You are a precise technical summarizer. Produce structured, concise summaries that preserve critical implementation details, exact file paths, function names, and errors."

const (
	CompactionPromptFileName = "compaction.md"

	defaultCompactionPrompt = `The messages above are a conversation to summarize. Create a structured context checkpoint summary that another LLM will use to continue the work.

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
- [Issues preventing progress, if any]

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

func BuildCompactionSummarizationInput(messages []llm.Message, previousSummary string) string {
	var b strings.Builder
	b.WriteString("<conversation>\n")
	b.WriteString(serializeConversation(messages))
	b.WriteString("\n</conversation>\n\n")
	b.WriteString("Summarize only the conversation above.\n")
	if strings.TrimSpace(previousSummary) != "" {
		b.WriteString("<previous-summary-read-only>\n")
		b.WriteString(strings.TrimSpace(previousSummary))
		b.WriteString("\n</previous-summary-read-only>\n\n")
		b.WriteString("The summary above is read-only prior context. Do NOT rewrite or incorporate it into the new summary output.\n\n")
	}
	b.WriteString(CurrentCompactionPrompt())
	return b.String()
}

func serializeConversation(messages []llm.Message) string {
	var b strings.Builder
	for _, m := range messages {
		role := strings.ToUpper(strings.TrimSpace(m.Role))
		if role == "" {
			role = "MESSAGE"
		}
		content := strings.TrimSpace(m.Content)
		if content == "" {
			continue
		}
		b.WriteString("[")
		b.WriteString(role)
		b.WriteString("]\n")
		b.WriteString(content)
		b.WriteString("\n\n")
	}
	return strings.TrimSpace(b.String())
}
