package core

import (
	"context"
	"strings"
	"testing"

	"github.com/ashiqrniloy/synapta-cli/internal/llm"
)

func TestSessionStoreManualCompactionReplacesTailWithOnlySummary(t *testing.T) {
	dir := t.TempDir()
	store, err := NewSessionStore(dir, AgentCode, "/tmp/project", DefaultCompactionSettings())
	if err != nil {
		t.Fatalf("NewSessionStore() error = %v", err)
	}

	seed := []llm.Message{
		{Role: "user", Content: "u1"},
		{Role: "assistant", Content: "a1"},
		{Role: "user", Content: "u2"},
		{Role: "assistant", Content: "a2"},
	}
	for _, msg := range seed {
		if err := store.AppendMessage(msg); err != nil {
			t.Fatalf("AppendMessage() error = %v", err)
		}
	}

	summarizer := func(ctx context.Context, toSummarize []llm.Message, previousSummary string) (string, error) {
		return "summary-1", nil
	}
	compacted, _, err := store.ManualCompact(context.Background(), 128000, summarizer)
	if err != nil {
		t.Fatalf("ManualCompact() error = %v", err)
	}
	if !compacted {
		t.Fatalf("expected compaction to run")
	}

	ctx := store.ContextMessages()
	if len(ctx) != 1 {
		t.Fatalf("expected only compaction summary in context, got %d messages", len(ctx))
	}
	if ctx[0].Role != "user" || !strings.Contains(ctx[0].Content, "summary-1") {
		t.Fatalf("unexpected first context message: %#v", ctx[0])
	}
}

func TestSessionStoreMultipleManualCompactionsKeepAllSummaries(t *testing.T) {
	dir := t.TempDir()
	store, err := NewSessionStore(dir, AgentCode, "/tmp/project", DefaultCompactionSettings())
	if err != nil {
		t.Fatalf("NewSessionStore() error = %v", err)
	}

	for _, msg := range []llm.Message{
		{Role: "user", Content: "u1"},
		{Role: "assistant", Content: "a1"},
		{Role: "user", Content: "u2"},
		{Role: "assistant", Content: "a2"},
	} {
		if err := store.AppendMessage(msg); err != nil {
			t.Fatalf("AppendMessage() error = %v", err)
		}
	}

	call := 0
	summarizer := func(ctx context.Context, toSummarize []llm.Message, previousSummary string) (string, error) {
		call++
		if call == 1 {
			return "summary-1", nil
		}
		return "summary-2", nil
	}

	compacted, _, err := store.ManualCompact(context.Background(), 128000, summarizer)
	if err != nil || !compacted {
		t.Fatalf("first ManualCompact() = (%v, %v)", compacted, err)
	}

	if err := store.AppendMessage(llm.Message{Role: "user", Content: "u3"}); err != nil {
		t.Fatalf("AppendMessage() error = %v", err)
	}
	if err := store.AppendMessage(llm.Message{Role: "assistant", Content: "a3"}); err != nil {
		t.Fatalf("AppendMessage() error = %v", err)
	}
	if err := store.AppendMessage(llm.Message{Role: "user", Content: "u4"}); err != nil {
		t.Fatalf("AppendMessage() error = %v", err)
	}
	if err := store.AppendMessage(llm.Message{Role: "assistant", Content: "a4"}); err != nil {
		t.Fatalf("AppendMessage() error = %v", err)
	}

	compacted, _, err = store.ManualCompact(context.Background(), 128000, summarizer)
	if err != nil || !compacted {
		t.Fatalf("second ManualCompact() = (%v, %v)", compacted, err)
	}

	ctx := store.ContextMessages()
	if len(ctx) != 2 {
		t.Fatalf("expected two compaction summaries in context, got %d messages", len(ctx))
	}
	if !strings.Contains(ctx[0].Content, "summary-1") {
		t.Fatalf("first summary missing: %q", ctx[0].Content)
	}
	if !strings.Contains(ctx[1].Content, "summary-2") {
		t.Fatalf("second summary missing: %q", ctx[1].Content)
	}
}
