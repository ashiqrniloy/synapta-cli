package core

import (
	"os"
	"strings"
	"testing"

	"github.com/ashiqrniloy/synapta-cli/internal/llm"
)

func TestContextManagerBuild_RuntimeMetadataAppendedAtEnd(t *testing.T) {
	m := NewContextManager(AgentCode, t.TempDir(), "/tmp/project", nil)
	history := []llm.Message{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "hi"},
	}

	msgs, err := m.Build(history)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if len(msgs) == 0 {
		t.Fatalf("expected non-empty messages")
	}

	last := msgs[len(msgs)-1]
	if last.Role != "system" {
		t.Fatalf("expected last role system, got %q", last.Role)
	}
	if !strings.Contains(last.Content, "# Runtime Metadata") {
		t.Fatalf("expected runtime metadata in last message, got: %q", last.Content)
	}
	if !strings.Contains(last.Content, "cwd: /tmp/project") {
		t.Fatalf("expected cwd in metadata, got: %q", last.Content)
	}
}

func TestContextManagerBuild_RuntimeMetadataAlwaysLastAcrossBuilds(t *testing.T) {
	m := NewContextManager(AgentCode, t.TempDir(), "/tmp/project", nil)

	cases := [][]llm.Message{
		{},
		{{Role: "user", Content: "one"}},
		{{Role: "user", Content: "one"}, {Role: "assistant", Content: "two"}, {Role: "user", Content: "three"}},
	}

	for i, history := range cases {
		msgs, err := m.Build(history)
		if err != nil {
			t.Fatalf("case %d: Build() error = %v", i, err)
		}
		if len(msgs) == 0 {
			t.Fatalf("case %d: expected non-empty messages", i)
		}
		last := msgs[len(msgs)-1]
		if last.Role != "system" || !strings.Contains(last.Content, "# Runtime Metadata") {
			t.Fatalf("case %d: expected runtime metadata as last message, got role=%q content=%q", i, last.Role, last.Content)
		}
	}
}

func TestContextManagerBuild_UsesUserEditableCodePromptAsBase(t *testing.T) {
	agentDir := t.TempDir()
	store := NewSystemPromptStore(agentDir)
	if err := store.EnsureDefaultIfAgentDirMissing(AgentCode, ""); err != nil {
		t.Fatalf("EnsureDefaultIfAgentDirMissing() error = %v", err)
	}
	if err := os.WriteFile(store.PromptPath(AgentCode), []byte("CUSTOM BASE PROMPT\n"), 0o644); err != nil {
		t.Fatalf("writing prompt file: %v", err)
	}

	m := NewContextManager(AgentCode, agentDir, t.TempDir(), store)
	msgs, err := m.Build(nil)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if len(msgs) == 0 {
		t.Fatalf("expected non-empty messages")
	}
	if msgs[0].Role != "system" {
		t.Fatalf("expected first message to be system, got %q", msgs[0].Role)
	}
	if !strings.HasPrefix(strings.TrimSpace(msgs[0].Content), "CUSTOM BASE PROMPT") {
		t.Fatalf("expected custom prompt to be base prefix, got: %q", msgs[0].Content)
	}
}
