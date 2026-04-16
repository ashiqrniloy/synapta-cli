package core

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/ashiqrniloy/synapta-cli/internal/core/tools"
	"github.com/ashiqrniloy/synapta-cli/internal/llm"
)

func TestParseToolCall_MalformedArguments(t *testing.T) {
	registry := NewToolRegistry()
	if err := registry.RegisterBuiltins(tools.NewToolSet(t.TempDir())); err != nil {
		t.Fatalf("register builtins: %v", err)
	}

	tests := []struct {
		name      string
		toolName  string
		arguments string
		errSubstr string
	}{
		{name: "read invalid json", toolName: "read", arguments: "{", errSubstr: "invalid read arguments"},
		{name: "write invalid json", toolName: "write", arguments: "not-json", errSubstr: "invalid write arguments"},
		{name: "bash invalid json", toolName: "bash", arguments: "[1,2", errSubstr: "invalid bash arguments"},
		{name: "unknown tool", toolName: "wat", arguments: "{}", errSubstr: "unknown tool"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseToolCall(llm.ToolCall{Function: llm.ToolFunctionCall{Name: tt.toolName, Arguments: tt.arguments}}, registry)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.errSubstr) {
				t.Fatalf("expected error containing %q, got %v", tt.errSubstr, err)
			}
		})
	}
}

func TestParseToolCall_ExtractsMetadata(t *testing.T) {
	registry := NewToolRegistry()
	if err := registry.RegisterBuiltins(tools.NewToolSet(t.TempDir())); err != nil {
		t.Fatalf("register builtins: %v", err)
	}
	parsed, err := ParseToolCall(llm.ToolCall{Function: llm.ToolFunctionCall{Name: "write", Arguments: `{"path":"a.txt"}`}}, registry)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if parsed.Path != "a.txt" {
		t.Fatalf("expected path a.txt, got %q", parsed.Path)
	}

	parsed, err = ParseToolCall(llm.ToolCall{Function: llm.ToolFunctionCall{Name: "bash", Arguments: `{"command":"echo hi"}`}}, registry)
	if err != nil {
		t.Fatalf("parse bash: %v", err)
	}
	if parsed.Command != "echo hi" {
		t.Fatalf("expected command echo hi, got %q", parsed.Command)
	}

	if !json.Valid(parsed.RawArguments) {
		t.Fatal("expected raw arguments to be valid json")
	}
}

func TestParseToolCall_AllowsUnknownWithoutRegistry(t *testing.T) {
	parsed, err := ParseToolCall(llm.ToolCall{Function: llm.ToolFunctionCall{Name: "custom", Arguments: `{"path":"x"}`}}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if parsed.Name != "custom" {
		t.Fatalf("expected custom, got %q", parsed.Name)
	}
}

func TestLoadRuntimeTools_UserAndExtensionManifests(t *testing.T) {
	root := t.TempDir()
	cwd := root + "/cwd"
	agent := root + "/agent"
	mustMkdirAll(t, cwd+"/.synapta/tools")
	mustMkdirAll(t, cwd+"/extensions/ext-1/tools")
	mustMkdirAll(t, agent+"/tools")
	mustMkdirAll(t, agent+"/extensions/ext-2")

	mustWriteFile(t, cwd+"/.synapta/tools/echo.json", `{
		"name":"echo_user",
		"description":"echo",
		"parameters":{"type":"object","properties":{}},
		"command":"bash",
		"args":["-lc","cat"],
		"streaming":false
	}`)
	mustWriteFile(t, cwd+"/extensions/ext-1/tools/t1.json", `{
		"name":"ext_tool",
		"description":"ext",
		"parameters":{"type":"object","properties":{}},
		"command":"bash",
		"args":["-lc","cat"],
		"streaming":false
	}`)
	mustWriteFile(t, agent+"/tools/a.json", `{
		"name":"agent_tool",
		"description":"agent",
		"parameters":{"type":"object","properties":{}},
		"command":"bash",
		"args":["-lc","cat"],
		"streaming":false
	}`)
	mustWriteFile(t, agent+"/extensions/ext-2/tool.json", `{
		"name":"ext_single",
		"description":"ext single",
		"parameters":{"type":"object","properties":{}},
		"command":"bash",
		"args":["-lc","cat"],
		"streaming":false
	}`)

	registry := NewToolRegistry()
	warnings := registry.LoadRuntimeTools(LoadToolRegistryOptions{CWD: cwd, AgentDir: agent})
	if len(warnings) != 0 {
		t.Fatalf("expected no warnings, got %v", warnings)
	}
	for _, name := range []string{"echo_user", "ext_tool", "agent_tool", "ext_single"} {
		if _, ok := registry.Get(name); !ok {
			t.Fatalf("expected tool %q to be loaded", name)
		}
	}
}

func TestToolRegistry_ManifestValidationWarnings(t *testing.T) {
	root := t.TempDir()
	cwd := root + "/cwd"
	mustMkdirAll(t, cwd+"/tools")
	mustWriteFile(t, cwd+"/tools/bad.json", `{bad}`)
	mustWriteFile(t, cwd+"/tools/missing.json", `{"name":"x"}`)

	registry := NewToolRegistry()
	warnings := registry.LoadRuntimeTools(LoadToolRegistryOptions{CWD: cwd})
	if len(warnings) < 2 {
		t.Fatalf("expected >=2 warnings, got %v", warnings)
	}
	joined := strings.Join(warnings, "\n")
	if !strings.Contains(joined, "invalid tool manifest") {
		t.Fatalf("expected invalid tool manifest warning, got %s", joined)
	}
	if !strings.Contains(joined, "missing command") {
		t.Fatalf("expected missing command warning, got %s", joined)
	}
}

func mustMkdirAll(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
}

func mustWriteFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
