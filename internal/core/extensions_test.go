package core

import (
	"os"
	"strings"
	"testing"
)

func TestLoadExtensions_FailureModes(t *testing.T) {
	tests := []struct {
		name              string
		setup             func(t *testing.T, root string) (cwd string, agent string)
		wantExtensions    int
		wantWarningSubstr []string
	}{
		{
			name: "invalid manifest + duplicate id + empty command",
			setup: func(t *testing.T, root string) (string, string) {
				cwd := root + "/cwd"
				agent := root + "/agent"
				extMustMkdirAll(t, cwd+"/extensions/ext-a")
				extMustMkdirAll(t, cwd+"/extensions/ext-b")
				extMustMkdirAll(t, cwd+"/extensions/ext-c")
				extMustMkdirAll(t, agent+"/extensions/ext-bad")

				extMustWriteFile(t, cwd+"/extensions/ext-a/extension.json", `{"id":"dup","name":"A","command":"echo","args":["a"]}`)
				extMustWriteFile(t, cwd+"/extensions/ext-b/extension.json", `{"id":"dup","name":"B","command":"echo","args":["b"]}`)
				extMustWriteFile(t, cwd+"/extensions/ext-c/extension.json", `{"id":"empty","name":"C","command":"   "}`)
				extMustWriteFile(t, agent+"/extensions/ext-bad/extension.json", `{bad-json}`)
				return cwd, agent
			},
			wantExtensions:    1,
			wantWarningSubstr: []string{"invalid extension manifest", "duplicate extension id", "empty command"},
		},
		{
			name: "all valid manifests",
			setup: func(t *testing.T, root string) (string, string) {
				cwd := root + "/cwd"
				agent := root + "/agent"
				extMustMkdirAll(t, cwd+"/extensions/e1")
				extMustMkdirAll(t, agent+"/extensions/e2")
				extMustWriteFile(t, cwd+"/extensions/e1/extension.json", `{"id":"one","command":"echo"}`)
				extMustWriteFile(t, agent+"/extensions/e2/extension.json", `{"id":"two","command":"echo"}`)
				return cwd, agent
			},
			wantExtensions:    2,
			wantWarningSubstr: nil,
		},
		{
			name: "collects extension tool manifest warnings",
			setup: func(t *testing.T, root string) (string, string) {
				cwd := root + "/cwd"
				agent := root + "/agent"
				extMustMkdirAll(t, cwd+"/extensions/ext-a")
				extMustMkdirAll(t, agent+"/extensions/ext-b/tools")

				extMustWriteFile(t, cwd+"/extensions/ext-a/extension.json", `{"id":"e-a","command":"echo"}`)
				extMustWriteFile(t, agent+"/extensions/ext-b/extension.json", `{"id":"e-b","command":"echo"}`)
				extMustWriteFile(t, cwd+"/extensions/ext-a/tool.json", `{bad}`)
				extMustWriteFile(t, agent+"/extensions/ext-b/tools/b.json", `{bad}`)
				return cwd, agent
			},
			wantExtensions:    2,
			wantWarningSubstr: []string{"invalid extension tool manifest", "ext-a/tool.json", "ext-b/tools/b.json"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			cwd, agent := tt.setup(t, root)
			res := LoadExtensions(LoadExtensionsOptions{CWD: cwd, AgentDir: agent})

			if len(res.Extensions) != tt.wantExtensions {
				t.Fatalf("expected %d extensions, got %d (%#v)", tt.wantExtensions, len(res.Extensions), res.Extensions)
			}
			joined := strings.Join(res.Warnings, "\n")
			for _, want := range tt.wantWarningSubstr {
				if !strings.Contains(joined, want) {
					t.Fatalf("expected warning containing %q, got warnings:\n%s", want, joined)
				}
			}
			if len(tt.wantWarningSubstr) == 0 && len(res.Warnings) != 0 {
				t.Fatalf("expected no warnings, got %v", res.Warnings)
			}
		})
	}
}

func extMustMkdirAll(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
}

func extMustWriteFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
