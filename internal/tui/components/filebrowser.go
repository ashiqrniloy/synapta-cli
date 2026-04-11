package components

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ashiqrniloy/synapta-cli/internal/tui/theme"
	"charm.land/lipgloss/v2"
)

// FileEntry represents a file or directory in the browser.
type FileEntry struct {
	Name     string
	Path     string
	IsDir    bool
	Children []FileEntry // populated when expanded
}

// FileBrowser manages the inline file browser state.
type FileBrowser struct {
	active     bool
	cwd        string
	entries    []FileEntry
	filtered   []FileEntry
	cursor     int
	styles     *theme.Styles
	maxVisible int
	// Navigation history (for drilling into folders)
	path       []string // breadcrumb of visited directories
	allEntries []FileEntry // all entries from cwd for search
}

// NewFileBrowser creates a new file browser.
func NewFileBrowser(styles *theme.Styles) *FileBrowser {
	return &FileBrowser{styles: styles, maxVisible: 5}
}

// Activate starts the file browser from the given root directory.
func (fb *FileBrowser) Activate(cwd string) {
	fb.active = true
	fb.cwd = cwd
	fb.path = []string{cwd}
	fb.cursor = 0
	fb.maxVisible = 5
	
	// Load all entries from cwd for search functionality
	fb.loadAllEntries(cwd)
	
	// Load entries for current view
	fb.loadEntries(cwd)
	fb.filtered = fb.entries
}

// Deactivate exits file browser mode.
func (fb *FileBrowser) Deactivate() {
	fb.active = false
	fb.entries = nil
	fb.filtered = nil
	fb.cursor = 0
	fb.path = nil
	fb.allEntries = nil
}

// IsActive returns true if the file browser is active.
func (fb *FileBrowser) IsActive() bool { return fb.active }

// Filter updates the filtered list based on the query.
func (fb *FileBrowser) Filter(query string) {
	query = strings.ToLower(strings.TrimSpace(query))
	
	if query == "" {
		// Reset to current directory entries
		currentDir := fb.currentDirectory()
		fb.loadEntries(currentDir)
		fb.filtered = fb.entries
	} else {
		// Search through all entries
		filtered := make([]FileEntry, 0)
		for _, entry := range fb.allEntries {
			if strings.Contains(strings.ToLower(entry.Name), query) {
				filtered = append(filtered, entry)
			}
		}
		fb.filtered = filtered
	}
	
	if fb.cursor >= len(fb.filtered) {
		fb.cursor = max(len(fb.filtered)-1, 0)
	}
}

// MoveUp moves the cursor up.
func (fb *FileBrowser) MoveUp() {
	if fb.cursor > 0 {
		fb.cursor--
	}
}

// MoveDown moves the cursor down.
func (fb *FileBrowser) MoveDown() {
	if fb.cursor < len(fb.filtered)-1 {
		fb.cursor++
	}
}

// Selected returns the currently selected entry, or nil if none.
func (fb *FileBrowser) Selected() *FileEntry {
	if len(fb.filtered) == 0 {
		return nil
	}
	return &fb.filtered[fb.cursor]
}

// CurrentDirectory returns the current directory being browsed.
func (fb *FileBrowser) currentDirectory() string {
	if len(fb.path) == 0 {
		return fb.cwd
	}
	return fb.path[len(fb.path)-1]
}

// NavigateInto navigates into the selected directory.
func (fb *FileBrowser) NavigateInto(dirPath string) bool {
	if !fb.isValidDirectory(dirPath) {
		return false
	}
	fb.path = append(fb.path, dirPath)
	fb.cursor = 0
	fb.loadEntries(dirPath)
	fb.filtered = fb.entries
	return true
}

// NavigateBack goes back one directory level.
// Returns true if we can go back, false if we're at root.
func (fb *FileBrowser) NavigateBack() bool {
	if len(fb.path) <= 1 {
		return false
	}
	fb.path = fb.path[:len(fb.path)-1]
	fb.cursor = 0
	currentDir := fb.currentDirectory()
	fb.loadEntries(currentDir)
	fb.filtered = fb.entries
	return true
}

// Path returns the current navigation path.
func (fb *FileBrowser) Path() []string {
	return fb.path
}

// Breadcrumb returns a display-friendly path string.
func (fb *FileBrowser) Breadcrumb() string {
	if len(fb.path) == 0 {
		return "/"
	}
	
	relPath, err := filepath.Rel(fb.cwd, fb.currentDirectory())
	if err != nil || relPath == "." {
		return "/"
	}
	
	if relPath == fb.currentDirectory() || strings.HasPrefix(relPath, "..") {
		return "/" + filepath.Base(fb.currentDirectory())
	}
	
	return "/" + relPath
}

// VisibleWindow returns the currently visible items and their start index.
func (fb *FileBrowser) VisibleWindow() ([]FileEntry, int) {
	if len(fb.filtered) == 0 {
		return nil, 0
	}
	if fb.maxVisible <= 0 || len(fb.filtered) <= fb.maxVisible {
		return fb.filtered, 0
	}
	start := fb.cursor - fb.maxVisible/2
	if start < 0 {
		start = 0
	}
	maxStart := len(fb.filtered) - fb.maxVisible
	if start > maxStart {
		start = maxStart
	}
	end := start + fb.maxVisible
	return fb.filtered[start:end], start
}

// Cursor returns the current cursor index.
func (fb *FileBrowser) Cursor() int { return fb.cursor }

// TotalItems returns the total number of filtered items.
func (fb *FileBrowser) TotalItems() int { return len(fb.filtered) }

// loadEntries reads the directory contents.
func (fb *FileBrowser) loadEntries(dir string) {
	fb.entries = nil
	
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	
	fileEntries := make([]FileEntry, 0)
	for _, entry := range entries {
		// Skip hidden files/directories
		name := entry.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		
		entryPath := filepath.Join(dir, name)
		isDir := entry.IsDir()
		
		fe := FileEntry{
			Name:  name,
			Path:  entryPath,
			IsDir: isDir,
		}
		
		if isDir {
			// Load children for preview (limit to first few)
			fe.Children = fb.loadChildEntries(entryPath)
		}
		
		fileEntries = append(fileEntries, fe)
	}
	
	// Sort: directories first, then alphabetically
	sort.Slice(fileEntries, func(i, j int) bool {
		if fileEntries[i].IsDir != fileEntries[j].IsDir {
			return fileEntries[i].IsDir
		}
		return strings.ToLower(fileEntries[i].Name) < strings.ToLower(fileEntries[j].Name)
	})
	
	fb.entries = fileEntries
}

// loadChildEntries loads a limited number of entries from a directory.
func (fb *FileBrowser) loadChildEntries(dir string) []FileEntry {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	
	children := make([]FileEntry, 0, 5)
	count := 0
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		
		count++
		if count > 5 {
			break
		}
		
		children = append(children, FileEntry{
			Name:  name,
			Path:  filepath.Join(dir, name),
			IsDir: entry.IsDir(),
		})
	}
	
	return children
}

// loadAllEntries recursively loads all files and directories from cwd.
func (fb *FileBrowser) loadAllEntries(root string) {
	fb.allEntries = nil
	fb.walkDirectory(root, 0, 3) // Limit depth for performance
}

// walkDirectory recursively walks a directory up to maxDepth.
func (fb *FileBrowser) walkDirectory(dir string, currentDepth, maxDepth int) {
	if currentDepth > maxDepth {
		return
	}
	
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		
		entryPath := filepath.Join(dir, name)
		isDir := entry.IsDir()
		
		fb.allEntries = append(fb.allEntries, FileEntry{
			Name:  name,
			Path:  entryPath,
			IsDir: isDir,
		})
		
		if isDir && currentDepth < maxDepth {
			fb.walkDirectory(entryPath, currentDepth+1, maxDepth)
		}
	}
}

// isValidDirectory checks if a path is a valid directory within cwd.
func (fb *FileBrowser) isValidDirectory(path string) bool {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	
	absCwd, err := filepath.Abs(fb.cwd)
	if err != nil {
		return false
	}
	
	// Must be within cwd
	if !strings.HasPrefix(absPath, absCwd) {
		return false
	}
	
	info, err := os.Stat(absPath)
	if err != nil || !info.IsDir() {
		return false
	}
	
	return true
}

// View renders the file browser.
func (fb *FileBrowser) View(width int) string {
	if !fb.active {
		return ""
	}

	styles := fb.styles
	fgColor := styles.CommandHighlightStyle.GetForeground()
	highlightBg := styles.CommandHighlightStyle.GetBackground()
	mutedFg := styles.MutedStyle.GetForeground()

	lines := []string{
		lipgloss.NewStyle().Foreground(mutedFg).Bold(true).Width(width).Render("Files (type to search)"),
	}
	
	// Breadcrumb
	breadcrumb := fb.Breadcrumb()
	lines = append(lines, lipgloss.NewStyle().Foreground(fgColor).Render("📁 "+breadcrumb))

	visible, start := fb.VisibleWindow()
	total := fb.TotalItems()
	
	if len(visible) == 0 {
		lines = append(lines, lipgloss.NewStyle().Foreground(mutedFg).Render("  (empty directory)"))
	}
	
	for i, entry := range visible {
		absoluteIdx := start + i
		icon := "📄"
		if entry.IsDir {
			icon = "📁"
		}
		
		label := fmt.Sprintf("%s %s", icon, entry.Name)
		if absoluteIdx == fb.cursor {
			line := "▸ " + label
			rendered := lipgloss.NewStyle().Foreground(fgColor).Background(highlightBg).Bold(true).Render(line)
			padding := max(width-lipgloss.Width(rendered), 1)
			lines = append(lines, lipgloss.NewStyle().Background(highlightBg).Render(line+strings.Repeat(" ", padding)))
		} else {
			lines = append(lines, lipgloss.NewStyle().Foreground(mutedFg).Render("  "+label))
		}
	}

	meta := "↑↓ navigate  •  Enter select/open  •  ← back  •  Esc cancel"
	if total > fb.maxVisible {
		meta = fmt.Sprintf("%s  •  %d-%d of %d", meta, start+1, start+len(visible), total)
	}
	lines = append(lines, lipgloss.NewStyle().Foreground(mutedFg).Render(meta))
	return strings.Join(lines, "\n")
}
