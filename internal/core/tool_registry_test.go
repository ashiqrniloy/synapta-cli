package core

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveManifestWorkDir(t *testing.T) {
	home, _ := os.UserHomeDir()

	tests := []struct {
		name        string
		configured  string
		manifestDir string
		cwd         string
		source      string
		want        string
	}{
		{
			name:        "extension default uses manifest dir",
			configured:  "",
			manifestDir: "/repo/extensions/ext-a",
			cwd:         "/repo",
			source:      ToolSourceExtension,
			want:        filepath.Clean("/repo/extensions/ext-a"),
		},
		{
			name:        "user default uses cwd",
			configured:  "",
			manifestDir: "/repo/.synapta/tools",
			cwd:         "/repo",
			source:      ToolSourceUser,
			want:        filepath.Clean("/repo"),
		},
		{
			name:        "relative path resolves from manifest dir",
			configured:  "./bin",
			manifestDir: "/repo/extensions/ext-a",
			cwd:         "/repo",
			source:      ToolSourceExtension,
			want:        filepath.Clean("/repo/extensions/ext-a/bin"),
		},
		{
			name:        "absolute configured path preserved",
			configured:  "/tmp/custom-wd",
			manifestDir: "/repo/extensions/ext-a",
			cwd:         "/repo",
			source:      ToolSourceExtension,
			want:        filepath.Clean("/tmp/custom-wd"),
		},
		{
			name:        "home shorthand is expanded",
			configured:  "~/.cache/synapta-tool",
			manifestDir: "/repo/extensions/ext-a",
			cwd:         "/repo",
			source:      ToolSourceExtension,
			want:        filepath.Join(home, ".cache", "synapta-tool"),
		},
		{
			name:        "cleaning applies to cwd and manifest defaults",
			configured:  "",
			manifestDir: "/repo/extensions/ext-a/../ext-a",
			cwd:         "/repo/./",
			source:      ToolSourceExtension,
			want:        filepath.Clean("/repo/extensions/ext-a"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveManifestWorkDir(tt.configured, tt.manifestDir, tt.cwd, tt.source)
			if filepath.Clean(got) != filepath.Clean(tt.want) {
				t.Fatalf("resolveManifestWorkDir(%q, %q, %q, %q) = %q, want %q", tt.configured, tt.manifestDir, tt.cwd, tt.source, got, tt.want)
			}
		})
	}
}
