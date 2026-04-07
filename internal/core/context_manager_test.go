package core

import (
	"os"
	"strings"
	"testing"

	"github.com/ashiqrniloy/synapta-cli/internal/llm"
)

func TestContextManagerBuild_DoesNotAppendRuntimeMetadata(t *testing.T) {
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

	for i, msg := range msgs {
		if msg.Role == "system" && strings.Contains(msg.Content, "# Runtime Metadata") {
			t.Fatalf("message %d unexpectedly contains runtime metadata", i)
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
