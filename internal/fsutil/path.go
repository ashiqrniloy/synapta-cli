package fsutil

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// IgnoreRules configures directory walk filtering.
type IgnoreRules struct {
	SkipHiddenDirs bool
	IgnoreDirNames map[string]struct{}
}

// DefaultIgnoreRules skips hidden directories and node_modules.
func DefaultIgnoreRules() IgnoreRules {
	return IgnoreRules{
		SkipHiddenDirs: true,
		IgnoreDirNames: map[string]struct{}{"node_modules": {}},
	}
}

// ResolvePath expands user-home prefixes and resolves a path relative to base.
func ResolvePath(base, p string) string {
	trimmed := strings.TrimSpace(p)
	if trimmed == "" {
		return ""
	}
	trimmed = ExpandHome(trimmed)
	if filepath.IsAbs(trimmed) {
		return filepath.Clean(trimmed)
	}
	if strings.TrimSpace(base) == "" {
		if wd, err := os.Getwd(); err == nil {
			base = wd
		}
	}
	return filepath.Clean(filepath.Join(base, trimmed))
}

// ExpandHome expands "~" and "~/..." forms when possible.
func ExpandHome(path string) string {
	if path == "~" {
		home, _ := os.UserHomeDir()
		if strings.TrimSpace(home) != "" {
			return home
		}
		return path
	}
	if strings.HasPrefix(path, "~/") {
		home, _ := os.UserHomeDir()
		if strings.TrimSpace(home) != "" {
			return filepath.Join(home, strings.TrimPrefix(path, "~/"))
		}
	}
	return path
}

// CanonicalPath returns an absolute cleaned path and resolves symlinks when possible.
func CanonicalPath(path string) string {
	abs := CleanAbs(path)
	if abs == "" {
		return ""
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil && strings.TrimSpace(resolved) != "" {
		return filepath.Clean(resolved)
	}
	return abs
}

// CleanAbs converts a path to absolute and cleans separators.
func CleanAbs(path string) string {
	if strings.TrimSpace(path) == "" {
		return ""
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	return filepath.Clean(abs)
}

// IsWithinRoot reports whether path is inside root (or equal to root).
// It resolves symlinks where possible, including existing parent segments.
func IsWithinRoot(path, root string) bool {
	if strings.TrimSpace(path) == "" || strings.TrimSpace(root) == "" {
		return false
	}
	canonRoot := canonicalizeForContainment(root)
	canonPath := canonicalizeForContainment(path)
	if canonRoot == "" || canonPath == "" {
		return false
	}
	rel, err := filepath.Rel(canonRoot, canonPath)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

// IsWithinRootLexical reports whether path is inside root using cleaned absolute
// paths without resolving symlinks.
func IsWithinRootLexical(path, root string) bool {
	if strings.TrimSpace(path) == "" || strings.TrimSpace(root) == "" {
		return false
	}
	cleanRoot := CleanAbs(root)
	cleanPath := CleanAbs(path)
	if cleanRoot == "" || cleanPath == "" {
		return false
	}
	rel, err := filepath.Rel(cleanRoot, cleanPath)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

func canonicalizeForContainment(path string) string {
	abs := CleanAbs(path)
	if abs == "" {
		return ""
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return filepath.Clean(resolved)
	}

	// Resolve existing parent segments to account for symlinked directories
	// when the final target path does not exist yet.
	cur := abs
	for {
		if _, err := os.Lstat(cur); err == nil {
			break
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			break
		}
		cur = parent
	}

	resolvedBase := cur
	if r, err := filepath.EvalSymlinks(cur); err == nil {
		resolvedBase = r
	}
	rel, err := filepath.Rel(cur, abs)
	if err != nil {
		return filepath.Clean(abs)
	}
	return filepath.Clean(filepath.Join(resolvedBase, rel))
}

// ReadDirFiltered reads directory entries and removes ignored directories.
func ReadDirFiltered(dir string, rules IgnoreRules) ([]os.DirEntry, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	out := make([]os.DirEntry, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() && ShouldIgnoreDir(entry.Name(), rules) {
			continue
		}
		out = append(out, entry)
	}
	return out, nil
}

// WalkFiles walks root recursively while applying ignore rules for directories.
func WalkFiles(root string, rules IgnoreRules, fn func(path string, d fs.DirEntry) error) error {
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() && ShouldIgnoreDir(d.Name(), rules) {
			if path == root {
				return nil
			}
			return filepath.SkipDir
		}
		return fn(path, d)
	})
}

// ShouldIgnoreDir reports whether a directory name is ignored by rules.
func ShouldIgnoreDir(name string, rules IgnoreRules) bool {
	if name == "" {
		return false
	}
	if rules.SkipHiddenDirs && strings.HasPrefix(name, ".") {
		return true
	}
	_, blocked := rules.IgnoreDirNames[name]
	return blocked
}

// ResolveAgentDir returns the app directory path from env var or ~/.<appName>.
func ResolveAgentDir(appName, envVar string) string {
	if envVar != "" {
		if envDir := strings.TrimSpace(os.Getenv(envVar)); envDir != "" {
			return CleanAbs(ExpandHome(envDir))
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "." + appName
	}
	return filepath.Join(home, "."+appName)
}
