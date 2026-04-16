package core

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/ashiqrniloy/synapta-cli/internal/llm"
)

func TestSessionStoreManualCompactionRetainsOnlySummaryAfterCompaction(t *testing.T) {
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


func TestSessionStoreToolAndSystemMessagesArePersistedInContext(t *testing.T) {
	dir := t.TempDir()
	store, err := NewSessionStore(dir, AgentCode, "/tmp/project", DefaultCompactionSettings())
	if err != nil {
		t.Fatalf("NewSessionStore() error = %v", err)
	}

	seed := []llm.Message{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "u1"},
		{Role: "assistant", Content: "a1"},
		{Role: "tool", Name: "read", ToolCallID: "t1", Content: `{"ok":true}`},
	}
	for _, msg := range seed {
		if err := store.AppendMessage(msg); err != nil {
			t.Fatalf("AppendMessage() error = %v", err)
		}
	}

	ctx := store.ContextMessages()
	if len(ctx) != len(seed) {
		t.Fatalf("expected %d context messages, got %d", len(seed), len(ctx))
	}
	if ctx[0].Role != "system" || ctx[3].Role != "tool" {
		t.Fatalf("expected system+tool roles to be preserved, got %#v", ctx)
	}
}

func TestSessionStoreManualCompactionSummarizesEntireContextEachTime(t *testing.T) {
	dir := t.TempDir()
	store, err := NewSessionStore(dir, AgentCode, "/tmp/project", DefaultCompactionSettings())
	if err != nil {
		t.Fatalf("NewSessionStore() error = %v", err)
	}

	for _, msg := range []llm.Message{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "u1"},
		{Role: "assistant", Content: "a1"},
		{Role: "tool", Content: "tool-1"},
	} {
		if err := store.AppendMessage(msg); err != nil {
			t.Fatalf("AppendMessage() error = %v", err)
		}
	}

	calls := 0
	summarizer := func(ctx context.Context, toSummarize []llm.Message, previousSummary string) (string, error) {
		calls++
		if calls == 1 {
			if len(toSummarize) != 4 {
				t.Fatalf("first compaction expected 4 msgs, got %d", len(toSummarize))
			}
			if previousSummary != "" {
				t.Fatalf("expected empty previous summary on first compaction")
			}
			return "summary-1", nil
		}
		if len(toSummarize) != 3 {
			t.Fatalf("second compaction expected current context (prior summary + new msgs), got %d", len(toSummarize))
		}
		if strings.TrimSpace(previousSummary) != "summary-1" {
			t.Fatalf("expected previous summary to be summary-1, got %q", previousSummary)
		}
		return "summary-2", nil
	}

	compacted, _, err := store.ManualCompact(context.Background(), 128000, summarizer)
	if err != nil || !compacted {
		t.Fatalf("first ManualCompact() = (%v, %v)", compacted, err)
	}

	if err := store.AppendMessage(llm.Message{Role: "user", Content: "u2"}); err != nil {
		t.Fatalf("AppendMessage() error = %v", err)
	}
	if err := store.AppendMessage(llm.Message{Role: "assistant", Content: "a2"}); err != nil {
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
	if !strings.Contains(ctx[0].Content, "summary-1") || !strings.Contains(ctx[1].Content, "summary-2") {
		t.Fatalf("expected both summaries in context, got %#v", ctx)
	}
}

func TestSessionStoreLoadFromFile_IgnoresCorruptAndPartialJSONLLines(t *testing.T) {
	dir := t.TempDir()
	store, err := NewSessionStore(dir, AgentCode, "/tmp/project", DefaultCompactionSettings())
	if err != nil {
		t.Fatalf("NewSessionStore() error = %v", err)
	}
	if err := store.AppendMessage(llm.Message{Role: "user", Content: "ok-1"}); err != nil {
		t.Fatalf("AppendMessage() error = %v", err)
	}
	if err := store.AppendMessage(llm.Message{Role: "assistant", Content: "ok-2"}); err != nil {
		t.Fatalf("AppendMessage() error = %v", err)
	}

	path := store.SessionFile()
	garbage := "\nthis is not json\n{\"type\":\"message\",\"message\":\"broken\"\n"
	if f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644); err != nil {
		t.Fatalf("open session file: %v", err)
	} else {
		if _, err := f.WriteString(garbage); err != nil {
			_ = f.Close()
			t.Fatalf("append garbage: %v", err)
		}
		_ = f.Close()
	}

	reopened, err := OpenSessionStore(dir, AgentCode, "/tmp/project", path, DefaultCompactionSettings())
	if err != nil {
		t.Fatalf("OpenSessionStore() error = %v", err)
	}
	ctx := reopened.ContextMessages()
	if len(ctx) != 2 {
		t.Fatalf("expected 2 valid messages after recovery, got %d (%#v)", len(ctx), ctx)
	}
	if ctx[0].Content != "ok-1" || ctx[1].Content != "ok-2" {
		t.Fatalf("unexpected recovered messages: %#v", ctx)
	}
}

func TestSessionStoreLoadFromFile_MissingHeaderEvenWithValidMessageFails(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/orphan.jsonl"
	content := strings.Join([]string{
		`{"type":"message","timestamp":"2026-01-01T00:00:00Z","message":{"role":"user","content":"x"}}`,
		`{"type":"message","timestamp":"2026-01-01T00:00:01Z","message":{"role":"assistant","content":"y"}}`,
	}, "\n") + "\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write orphan session: %v", err)
	}

	_, err := OpenSessionStore(dir, AgentCode, "/tmp/project", path, DefaultCompactionSettings())
	if err == nil {
		t.Fatal("expected error for missing session header, got nil")
	}
	if !strings.Contains(err.Error(), "missing session header") {
		t.Fatalf("unexpected error: %v", err)
	}
}
