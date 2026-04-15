package core

import (
	"fmt"

	"github.com/ashiqrniloy/synapta-cli/internal/llm"
)

// Token budget configuration for context management.
type ContextBudgetConfig struct {
	// MaxRequestTokens is the target maximum tokens for any single API request.
	// Set to 0 to use default (80% of context window).
	MaxRequestTokens int

	// SummarizationBudgetTokens limits the tokens passed to summarization calls.
	// Should be small enough that the summarization request itself won't timeout.
	SummarizationBudgetTokens int

	// ReserveTokens reserves space in context window for response generation.
	ReserveTokens int

	// TruncateBelowTokens when context exceeds budget, truncate content of
	// individual messages to this size before dropping messages entirely.
	TruncateBelowTokens int
}

// DefaultContextBudgetConfig returns sensible defaults.
func DefaultContextBudgetConfig() ContextBudgetConfig {
	return ContextBudgetConfig{
		MaxRequestTokens:          0, // Use 80% of context window
		SummarizationBudgetTokens: 80000,
		ReserveTokens:             8192,
		TruncateBelowTokens:       4000,
	}
}

// ContextSize holds token estimates for a context.
type ContextSize struct {
	SystemPromptTokens int
	HistoryTokens      int
	ToolResultsTokens  int
	TotalTokens        int
	ContextWindow      int
	PercentUsed        float64
}

// EstimateContextSize calculates token usage for a set of messages.
func EstimateContextSize(messages []llm.Message, contextWindow int) ContextSize {
	s := ContextSize{ContextWindow: contextWindow}

	for _, msg := range messages {
		tokens := budgetEstimateMessageTokens(msg)
		switch msg.Role {
		case "system":
			s.SystemPromptTokens += tokens
		case "tool":
			s.ToolResultsTokens += tokens
		default:
			s.HistoryTokens += tokens
		}
	}

	s.TotalTokens = s.SystemPromptTokens + s.HistoryTokens + s.ToolResultsTokens
	if contextWindow > 0 {
		s.PercentUsed = float64(s.TotalTokens) / float64(contextWindow) * 100
	}

	return s
}

// EnforceBudget ensures messages fit within the token budget.
// Returns truncated messages and information about what was trimmed.
func EnforceBudget(messages []llm.Message, maxTokens int, truncateBelow int) ([]llm.Message, string) {
	if maxTokens <= 0 {
		return messages, ""
	}

	current := budgetEstimateAllTokens(messages)
	if current <= maxTokens {
		return messages, ""
	}

	// Strategy: Keep recent messages, truncate oldest
	// Sort by role priority: system > user > assistant > tool
	var systemMsgs, userMsgs, assistantMsgs, toolMsgs []llm.Message
	for _, msg := range messages {
		switch msg.Role {
		case "system":
			systemMsgs = append(systemMsgs, msg)
		case "user":
			userMsgs = append(userMsgs, msg)
		case "assistant":
			assistantMsgs = append(assistantMsgs, msg)
		case "tool":
			toolMsgs = append(toolMsgs, msg)
		}
	}

	// Count tokens in each category
	systemTokens := budgetEstimateAllTokens(systemMsgs)
	userTokens := budgetEstimateAllTokens(userMsgs)
	assistantTokens := budgetEstimateAllTokens(assistantMsgs)
	toolTokens := budgetEstimateAllTokens(toolMsgs)

	// Reserve space for system and user messages
	availableForOthers := maxTokens - systemTokens - userTokens

	// If we don't have room, truncate user messages
	if availableForOthers < 0 {
		userTokens = max(0, maxTokens-systemTokens)
		truncated := budgetTruncateMessagesToTokens(userMsgs, userTokens, truncateBelow)
		kept := append([]llm.Message{}, systemMsgs...)
		kept = append(kept, truncated...)
		return finishBudgetEnforcement(kept, maxTokens, truncateBelow)
	}

	// Try to fit everything, starting with most recent
	kept := append([]llm.Message{}, systemMsgs...)
	kept = append(kept, userMsgs...)

	remaining := availableForOthers - assistantTokens - toolTokens
	if remaining >= 0 {
		kept = append(kept, assistantMsgs...)
		kept = append(kept, toolMsgs...)
		return finishBudgetEnforcement(kept, maxTokens, truncateBelow)
	}

	// Need to drop something - start with oldest tool messages
	kept = append(kept, assistantMsgs...)
	remaining = availableForOthers - assistantTokens
	if remaining >= 0 {
		kept = append(kept, budgetTruncateMessagesToTokens(toolMsgs, remaining, truncateBelow)...)
		return finishBudgetEnforcement(kept, maxTokens, truncateBelow)
	}

	// Drop all tool messages, keep only recent assistant
	remaining = availableForOthers
	if remaining <= 0 {
		return finishBudgetEnforcement(kept, maxTokens, truncateBelow)
	}

	// Truncate recent assistant messages
	kept = append(kept, budgetTruncateMessagesToTokens(assistantMsgs, remaining, truncateBelow)...)

	return finishBudgetEnforcement(kept, maxTokens, truncateBelow)
}

func finishBudgetEnforcement(kept []llm.Message, maxTokens, truncateBelow int) ([]llm.Message, string) {
	// If still over, keep only the most recent messages
	if budgetEstimateAllTokens(kept) <= maxTokens {
		return kept, ""
	}

	// Binary search for the right number of recent messages to keep
	lo, hi := 0, len(kept)
	for lo < hi {
		mid := (lo + hi + 1) / 2
		recent := kept[len(kept)-mid:]
		if budgetEstimateAllTokens(recent) <= maxTokens {
			lo = mid
		} else {
			hi = mid - 1
		}
	}

	remaining := max(0, lo)
	if remaining == 0 {
		return []llm.Message{}, "context fully truncated"
	}

	truncated := kept[len(kept)-remaining:]
	return truncated, fmt.Sprintf("kept %d most recent messages", remaining)
}

// budgetTruncateMessagesToTokens truncates message content to fit within token budget.
// Drops messages entirely only after content truncation is exhausted.
func budgetTruncateMessagesToTokens(messages []llm.Message, maxTokens, truncateBelow int) []llm.Message {
	if len(messages) == 0 || maxTokens <= 0 {
		return nil
	}

	result := make([]llm.Message, 0, len(messages))
	current := 0

	for _, msg := range messages {
		if current >= maxTokens {
			break
		}

		msg := msg // copy
		tokens := budgetEstimateMessageTokens(msg)

		// Check if we need to truncate
		if tokens > maxTokens-current {
			// Need to fit within remaining budget
			remaining := maxTokens - current

			// Truncate content if possible
			if truncateBelow > 0 {
				// Calculate how many chars would fit in remaining budget
				maxChars := remaining * 4
				if len(msg.Content) > maxChars {
					msg.Content = msg.Content[:maxChars] + "\n[truncated]"
					result = append(result, msg)
					current += budgetEstimateMessageTokens(msg)
					continue
				}
			}

			// Can't truncate enough, skip this message
			continue
		}

		// Truncate content if it exceeds truncateBelow
		if truncateBelow > 0 && tokens > truncateBelow {
			maxChars := truncateBelow * 4
			if len(msg.Content) > maxChars {
				msg.Content = msg.Content[:maxChars] + "\n[truncated]"
				tokens = budgetEstimateMessageTokens(msg)
			}
		}

		result = append(result, msg)
		current += tokens
	}

	return result
}

// PrepareRequestSafely builds LLM messages with budget enforcement.
// Returns messages, budget info, and any warning.
func PrepareRequestSafely(messages []llm.Message, contextWindow, maxRequestTokens, reserveTokens int) ([]llm.Message, ContextSize, string) {
	if contextWindow <= 0 {
		contextWindow = 128000 // Safe default
	}
	if reserveTokens <= 0 {
		reserveTokens = 8192
	}
	if maxRequestTokens <= 0 {
		maxRequestTokens = int(float64(contextWindow) * 0.80)
	}

	effectiveMax := maxRequestTokens - reserveTokens
	// Only apply floor for large maxRequestTokens to allow for meaningful budgets
	// For small maxRequestTokens (e.g., 2000), respect the user's budget
	if maxRequestTokens >= 20000 && effectiveMax < 10000 {
		effectiveMax = 10000 // Absolute minimum for large requests
	}

	size := EstimateContextSize(messages, contextWindow)

	var warning string
	if size.TotalTokens > maxRequestTokens {
		truncated, reason := EnforceBudget(messages, effectiveMax, 0)
		warning = fmt.Sprintf("context exceeded budget (%d → %d tokens): %s", size.TotalTokens, budgetEstimateAllTokens(truncated), reason)
		return truncated, EstimateContextSize(truncated, contextWindow), warning
	}

	return messages, size, ""
}

// PrepareSummarizationSafely prepares messages for summarization with budget limits.
func PrepareSummarizationSafely(messages []llm.Message, budgetTokens, truncateBelow int) ([]llm.Message, string) {
	if budgetTokens <= 0 {
		budgetTokens = 80000
	}

	current := budgetEstimateAllTokens(messages)
	if current <= budgetTokens {
		return messages, ""
	}

	truncated, reason := EnforceBudget(messages, budgetTokens, truncateBelow)
	return truncated, fmt.Sprintf("truncated for summarization: %s", reason)
}

// budgetEstimateMessageTokens estimates tokens in a single message.
func budgetEstimateMessageTokens(msg llm.Message) int {
	return llm.EstimateMessageTokens(msg)
}

// budgetEstimateAllTokens sums token estimates for a message list.
func budgetEstimateAllTokens(messages []llm.Message) int {
	total := 0
	for _, msg := range messages {
		total += budgetEstimateMessageTokens(msg)
	}
	return total
}
