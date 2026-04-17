package fsutil

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsWithinRoot(t *testing.T) {
	root := t.TempDir()
	inside := filepath.Join(root, "a", "b.txt")
	if !IsWithinRoot(inside, root) {
		t.Fatalf("expected %q to be inside %q", inside, root)
	}

	outside := t.TempDir()
	outsidePath := filepath.Join(outside, "x.txt")
	if IsWithinRoot(outsidePath, root) {
		t.Fatalf("expected %q to be outside %q", outsidePath, root)
	}
}

func TestIsWithinRoot_SymlinkedParentOutside(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()

	linkDir := filepath.Join(root, "linked")
	if err := os.Symlink(outside, linkDir); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}

	candidate := filepath.Join(linkDir, "file.txt")
	if IsWithinRoot(candidate, root) {
		t.Fatalf("expected symlinked path %q to resolve outside root %q", candidate, root)
	}
}

func TestWalkFiles_IgnoreRules(t *testing.T) {
	root := t.TempDir()
	mustMkdirAll(t, filepath.Join(root, "visible"))
	mustMkdirAll(t, filepath.Join(root, ".hidden"))
	mustMkdirAll(t, filepath.Join(root, "node_modules", "pkg"))
	mustWriteFile(t, filepath.Join(root, "visible", "ok.txt"), "ok")
	mustWriteFile(t, filepath.Join(root, ".hidden", "secret.txt"), "x")
	mustWriteFile(t, filepath.Join(root, "node_modules", "pkg", "dep.txt"), "x")

	seen := map[string]struct{}{}
	err := WalkFiles(root, DefaultIgnoreRules(), func(path string, d os.DirEntry) error {
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		seen[rel] = struct{}{}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}

	if _, ok := seen[filepath.Join("visible", "ok.txt")]; !ok {
		t.Fatalf("expected visible file to be visited")
	}
	if _, ok := seen[filepath.Join(".hidden", "secret.txt")]; ok {
		t.Fatalf("did not expect hidden file to be visited")
	}
	if _, ok := seen[filepath.Join("node_modules", "pkg", "dep.txt")]; ok {
		t.Fatalf("did not expect node_modules file to be visited")
	}
}

func mustMkdirAll(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
}

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
