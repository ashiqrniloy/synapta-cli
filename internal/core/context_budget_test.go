package core

import (
	"testing"

	"github.com/ashiqrniloy/synapta-cli/internal/llm"
)

func TestBudgetEstimateMessageTokens(t *testing.T) {
	tests := []struct {
		name    string
		msg     llm.Message
		wantMin int
		wantMax int
	}{
		{
			name:    "empty content",
			msg:     llm.Message{Role: "user", Content: ""},
			wantMin: 0,
			wantMax: 0,
		},
		{
			name:    "short text",
			msg:     llm.Message{Role: "user", Content: "Hello"},
			wantMin: 2,
			wantMax: 10,
		},
		{
			name:    "medium text",
			msg:     llm.Message{Role: "user", Content: "This is a test message with some content."},
			wantMin: 8,
			wantMax: 20,
		},
		{
			name:    "long text",
			msg:     llm.Message{Role: "user", Content: string(make([]byte, 1000))},
			wantMin: 200,
			wantMax: 350,
		},
		{
			name:    "system role adds overhead",
			msg:     llm.Message{Role: "system", Content: "Test"},
			wantMin: 5,
			wantMax: 15,
		},
		{
			name:    "tool role adds overhead",
			msg:     llm.Message{Role: "tool", Content: "Test"},
			wantMin: 7,
			wantMax: 20,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := budgetEstimateMessageTokens(tt.msg)
			if got < tt.wantMin || got > tt.wantMax {
				t.Errorf("budgetEstimateMessageTokens() = %v, want between %v and %v", got, tt.wantMin, tt.wantMax)
			}
		})
	}
}

func TestEstimateContextSize(t *testing.T) {
	messages := []llm.Message{
		{Role: "system", Content: "You are a helpful assistant."},
		{Role: "user", Content: "Hello"},
		{Role: "assistant", Content: "Hi there!"},
		{Role: "tool", Content: `{"result": "success"}`},
	}

	size := EstimateContextSize(messages, 128000)

	if size.SystemPromptTokens == 0 {
		t.Error("expected system tokens to be > 0")
	}
	if size.HistoryTokens == 0 {
		t.Error("expected history tokens to be > 0")
	}
	if size.ToolResultsTokens == 0 {
		t.Error("expected tool tokens to be > 0")
	}
	if size.TotalTokens == 0 {
		t.Error("expected total tokens to be > 0")
	}
	if size.ContextWindow != 128000 {
		t.Errorf("ContextWindow = %v, want 128000", size.ContextWindow)
	}
	if size.PercentUsed <= 0 || size.PercentUsed > 100 {
		t.Errorf("PercentUsed = %v, want between 0 and 100", size.PercentUsed)
	}
}

func TestEnforceBudget(t *testing.T) {
	// Create a list of messages that will exceed typical limits
	messages := make([]llm.Message, 100)
	for i := range messages {
		messages[i] = llm.Message{
			Role:    "user",
			Content: string(make([]byte, 500)),
		}
	}

	totalTokens := budgetEstimateAllTokens(messages)
	if totalTokens <= 5000 {
		t.Skipf("test messages too small: %d tokens", totalTokens)
	}

	// Try to fit in 5000 tokens
	kept, _ := EnforceBudget(messages, 5000, 0)

	if len(kept) == 0 {
		t.Error("expected at least some messages to be kept")
	}
	if len(kept) >= len(messages) {
		t.Error("expected some messages to be dropped")
	}

	// Verify the kept messages fit within budget
	keptTokens := budgetEstimateAllTokens(kept)
	if keptTokens > 5000 {
		t.Errorf("kept tokens %d exceeds budget %d", keptTokens, 5000)
	}
}

func TestEnforceBudgetPreservesRecentMessages(t *testing.T) {
	// Create messages with distinct content
	messages := []llm.Message{
		{Role: "user", Content: "First message with some content"},
		{Role: "user", Content: "Second message with more content"},
		{Role: "user", Content: "Third message"},
		{Role: "user", Content: "Fourth message"},
		{Role: "user", Content: "Fifth and final message"},
	}

	// Each message is roughly 5-10 tokens, budget for 2-3 messages
	kept, reason := EnforceBudget(messages, 50, 0)

	if len(kept) == 0 {
		t.Error("expected at least some messages to be kept")
	}
	if len(kept) >= len(messages) {
		t.Error("expected some messages to be dropped")
	}

	// Should keep the most recent messages (last few)
	if len(kept) > 0 {
		lastContent := kept[len(kept)-1].Content
		// The last kept message should be one of the recent ones
		if lastContent == "First message with some content" {
			t.Error("should not keep the oldest message as the last")
		}
	}

	t.Logf("Kept %d messages, reason: %s", len(kept), reason)
}

func TestPrepareRequestSafely(t *testing.T) {
	// Create messages that exceed the budget
	messages := make([]llm.Message, 50)
	for i := range messages {
		messages[i] = llm.Message{
			Role:    "user",
			Content: string(make([]byte, 300)),
		}
	}

	// Use a tight budget
	result, size, warning := PrepareRequestSafely(messages, 128000, 2000, 500)

	t.Logf("Result: %d messages, %d tokens", len(result), budgetEstimateAllTokens(result))
	t.Logf("Size.TotalTokens: %d", size.TotalTokens)
	t.Logf("Warning: '%s'", warning)

	if len(result) == 0 {
		t.Error("expected some messages to be returned")
	}

	// The result should fit within effective budget
	keptTokens := budgetEstimateAllTokens(result)
	if keptTokens > 1500 {
		t.Errorf("total tokens %d exceeds effective budget %d", keptTokens, 1500)
	}

	// Warning should indicate budget was exceeded
	if warning == "" {
		t.Error("expected warning when budget is exceeded")
	}
}

func TestPrepareSummarizationSafely(t *testing.T) {
	// Create messages for summarization
	messages := make([]llm.Message, 30)
	for i := range messages {
		messages[i] = llm.Message{
			Role:    "user",
			Content: string(make([]byte, 500)),
		}
	}

	result, warning := PrepareSummarizationSafely(messages, 3000, 0)

	if len(result) == 0 {
		t.Error("expected some messages to be returned for summarization")
	}

	total := budgetEstimateAllTokens(result)
	if total > 3000 {
		t.Errorf("summarization tokens %d exceeds budget %d", total, 3000)
	}

	t.Logf("Warning: %s", warning)
}

func TestDefaultContextBudgetConfig(t *testing.T) {
	cfg := DefaultContextBudgetConfig()

	if cfg.MaxRequestTokens != 0 {
		t.Errorf("MaxRequestTokens = %v, want 0", cfg.MaxRequestTokens)
	}
	if cfg.SummarizationBudgetTokens != 80000 {
		t.Errorf("SummarizationBudgetTokens = %v, want 80000", cfg.SummarizationBudgetTokens)
	}
	if cfg.ReserveTokens != 8192 {
		t.Errorf("ReserveTokens = %v, want 8192", cfg.ReserveTokens)
	}
	if cfg.TruncateBelowTokens != 4000 {
		t.Errorf("TruncateBelowTokens = %v, want 4000", cfg.TruncateBelowTokens)
	}
}