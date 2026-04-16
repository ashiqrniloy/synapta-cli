package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWritePathAndSymlinkBehavior(t *testing.T) {
	tests := []struct {
		name string
		run  func(t *testing.T)
	}{
		{
			name: "path traversal outside cwd rejected",
			run: func(t *testing.T) {
				dir := t.TempDir()
				tool := NewWriteTool(dir)
				_, err := tool.Execute(context.Background(), WriteInput{
					Path:    "../../etc/passwd",
					Mode:    WriteModeOverwrite,
					Content: "nope\n",
				})
				if err == nil {
					t.Fatal("expected traversal error, got nil")
				}
				if !strings.Contains(err.Error(), "outside the working directory") {
					t.Fatalf("unexpected traversal error: %v", err)
				}
			},
		},
		{
			name: "write to symlink path replaces link not external target",
			run: func(t *testing.T) {
				dir := t.TempDir()
				outside := t.TempDir()
				target := filepath.Join(outside, "target.txt")
				if err := os.WriteFile(target, []byte("safe\n"), 0o644); err != nil {
					t.Fatalf("seed target: %v", err)
				}
				linkPath := filepath.Join(dir, "link.txt")
				if err := os.Symlink(target, linkPath); err != nil {
					t.Skipf("symlink not supported: %v", err)
				}

				tool := NewWriteTool(dir)
				if _, err := tool.Execute(context.Background(), WriteInput{Path: "link.txt", Mode: WriteModeOverwrite, Content: "changed\n"}); err != nil {
					t.Fatalf("write via symlink path failed: %v", err)
				}

				targetBytes, err := os.ReadFile(target)
				if err != nil {
					t.Fatalf("read target: %v", err)
				}
				if string(targetBytes) != "safe\n" {
					t.Fatalf("expected external target unchanged, got %q", string(targetBytes))
				}
				linkBytes, err := os.ReadFile(linkPath)
				if err != nil {
					t.Fatalf("read link path: %v", err)
				}
				if string(linkBytes) != "changed\n" {
					t.Fatalf("expected link path content to update, got %q", string(linkBytes))
				}
				if info, err := os.Lstat(linkPath); err != nil {
					t.Fatalf("lstat link path: %v", err)
				} else if info.Mode()&os.ModeSymlink != 0 {
					t.Fatalf("expected link path to be replaced by regular file")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, tt.run)
	}
}
