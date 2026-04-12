package components

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"charm.land/lipgloss/v2"
)

// ─── Modal Open/Close ──────────────────────────────────────────────

func (m *CodeAgentModel) openFileBrowserModal() {
	m.fileBrowserModalOpen = true
	m.fileBrowserModalCursor = 0
	m.fileBrowserModalSearch = ""
	m.fileBrowserModalOffset = 0
	m.fileBrowserModalPath = m.currentCwd

	// Load entries from cwd
	m.fileBrowserModalEntries = m.loadFileBrowserEntries(m.currentCwd, 3)
}

func (m *CodeAgentModel) closeFileBrowserModal() {
	m.fileBrowserModalOpen = false
	m.fileBrowserModalCursor = 0
	m.fileBrowserModalSearch = ""
	m.fileBrowserModalEntries = nil
	m.fileBrowserModalOffset = 0
	m.fileBrowserModalPath = ""
}

// isFileBrowserAtRoot returns true if the current path is the same as cwd.
func (m *CodeAgentModel) isFileBrowserAtRoot() bool {
	return m.fileBrowserModalPath == m.currentCwd
}

// fileBrowserGoBack goes back one directory level.
func (m *CodeAgentModel) fileBrowserGoBack() bool {
	if m.isFileBrowserAtRoot() {
		return false // Can't go back further
	}

	parent := filepath.Dir(m.fileBrowserModalPath)
	// Ensure we don't go above cwd
	if !strings.HasPrefix(parent, m.currentCwd) {
		parent = m.currentCwd
	}

	m.fileBrowserModalPath = parent
	m.fileBrowserModalEntries = m.loadFileBrowserEntries(parent, 3)
	m.fileBrowserModalCursor = 0
	m.fileBrowserModalOffset = 0
	m.fileBrowserModalSearch = ""
	return true
}

// fileBrowserNavigateInto navigates into the selected directory.
func (m *CodeAgentModel) fileBrowserNavigateInto(dirPath string) {
	m.fileBrowserModalPath = dirPath
	m.fileBrowserModalEntries = m.loadFileBrowserEntries(dirPath, 3)
	m.fileBrowserModalCursor = 0
	m.fileBrowserModalOffset = 0
	m.fileBrowserModalSearch = ""
}

// ─── Entry Loading ─────────────────────────────────────────────────

func (m *CodeAgentModel) loadFileBrowserEntries(dir string, maxDepth int) []FileEntry {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	fileEntries := make([]FileEntry, 0)
	for _, entry := range entries {
		name := entry.Name()
		// Skip hidden files/directories
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

		if isDir && maxDepth > 1 {
			// Load children for preview (limited)
			fe.Children = m.loadFileBrowserChildren(entryPath, maxDepth-1)
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

	return fileEntries
}

func (m *CodeAgentModel) loadFileBrowserChildren(dir string, maxDepth int) []FileEntry {
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

// ─── Filter ─────────────────────────────────────────────────────────

func (m *CodeAgentModel) filterFileBrowserEntries(query string) []FileEntry {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return m.loadFileBrowserEntries(m.fileBrowserModalPath, 3)
	}

	// For search, we need to traverse the directory tree
	allFiles := m.walkFileBrowserDirectory(m.currentCwd, 3)
	filtered := make([]FileEntry, 0)
	for _, entry := range allFiles {
		if strings.Contains(strings.ToLower(entry.Name), query) {
			filtered = append(filtered, entry)
		}
	}

	// Sort: directories first, then alphabetically
	sort.Slice(filtered, func(i, j int) bool {
		if filtered[i].IsDir != filtered[j].IsDir {
			return filtered[i].IsDir
		}
		return strings.ToLower(filtered[i].Name) < strings.ToLower(filtered[j].Name)
	})

	return filtered
}

func (m *CodeAgentModel) walkFileBrowserDirectory(dir string, maxDepth int) []FileEntry {
	if maxDepth <= 0 {
		return nil
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	result := make([]FileEntry, 0)
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}

		entryPath := filepath.Join(dir, name)
		isDir := entry.IsDir()

		result = append(result, FileEntry{
			Name:  name,
			Path:  entryPath,
			IsDir: isDir,
		})

		if isDir {
			subEntries := m.walkFileBrowserDirectory(entryPath, maxDepth-1)
			result = append(result, subEntries...)
		}
	}

	return result
}

// ─── Navigation ───────────────────────────────────────────────────

func (m *CodeAgentModel) fileBrowserModalSelectedEntry() *FileEntry {
	if len(m.fileBrowserModalEntries) == 0 {
		return nil
	}
	if m.fileBrowserModalCursor < 0 {
		m.fileBrowserModalCursor = 0
	}
	if m.fileBrowserModalCursor >= len(m.fileBrowserModalEntries) {
		m.fileBrowserModalCursor = len(m.fileBrowserModalEntries) - 1
	}
	return &m.fileBrowserModalEntries[m.fileBrowserModalCursor]
}

// fileBrowserBreadcrumb returns a display-friendly path string.
func (m *CodeAgentModel) fileBrowserBreadcrumb() string {
	if m.fileBrowserModalPath == "" {
		return "/"
	}

	relPath, err := filepath.Rel(m.currentCwd, m.fileBrowserModalPath)
	if err != nil || relPath == "." {
		return "/"
	}

	if relPath == m.fileBrowserModalPath || strings.HasPrefix(relPath, "..") {
		return "/" + filepath.Base(m.fileBrowserModalPath)
	}

	return "/" + relPath
}

// ─── Rendering ─────────────────────────────────────────────────────

func (m *CodeAgentModel) renderFileBrowserModal() string {
	width := m.width
	height := m.height

	leftW := max((width-10)*35/100, 35)
	rightW := max((width-10)-leftW, 35)
	innerH := max(height-8, 8)

	// Get current entries
	entries := m.fileBrowserModalEntries
	if m.fileBrowserModalCursor < 0 {
		m.fileBrowserModalCursor = 0
	}
	if m.fileBrowserModalCursor >= len(entries) && len(entries) > 0 {
		m.fileBrowserModalCursor = len(entries) - 1
	}

	// Render left pane (file list)
	leftLines := []string{lipgloss.NewStyle().Bold(true).Render("Files")}

	// Breadcrumb
	breadcrumb := m.fileBrowserBreadcrumb()
	leftLines = append(leftLines, m.styles.MutedStyle.Render("📁 "+breadcrumb))

	// Search hint
	searchHint := "Type to search…"
	if m.fileBrowserModalSearch != "" {
		searchHint = "Search: " + m.fileBrowserModalSearch
	}
	leftLines = append(leftLines, m.styles.MutedStyle.Render(searchHint))

	// List entries
	listH := innerH - 5
	maxVisible := max(listH, 1)
	start := 0
	if m.fileBrowserModalCursor >= maxVisible {
		start = m.fileBrowserModalCursor - maxVisible + 1
	}
	end := min(start+maxVisible, len(entries))

	if len(entries) == 0 {
		leftLines = append(leftLines, m.styles.MutedStyle.Render("(empty or searching…)"))
		for len(leftLines) < innerH {
			leftLines = append(leftLines, "")
		}
	} else {
		for i := start; i < end; i++ {
			entry := entries[i]
			icon := "📄"
			if entry.IsDir {
				icon = "📁"
			}

			prefix := "  "
			if i == m.fileBrowserModalCursor {
				prefix = "▸ "
			}

			label := truncateLine(fmt.Sprintf("%s %s", icon, entry.Name), leftW-4)
			if i == m.fileBrowserModalCursor {
				leftLines = append(leftLines, m.styles.CommandHighlightStyle.Render(prefix+label))
			} else {
				leftLines = append(leftLines, m.styles.MutedStyle.Render(prefix+label))
			}
		}
		// Fill remaining space
		for len(leftLines) < innerH {
			leftLines = append(leftLines, "")
		}
	}

	leftContent := strings.Join(leftLines, "\n")
	leftPane := lipgloss.NewStyle().
		Width(leftW).
		Height(innerH).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(m.borderColor)).
		Padding(0, 1).
		Render(leftContent)

	// Render right pane (file preview)
	rightLines := []string{lipgloss.NewStyle().Bold(true).Render("Preview")}

	selected := m.fileBrowserModalSelectedEntry()
	if selected == nil {
		rightLines = append(rightLines, m.styles.MutedStyle.Render("(no selection)"))
		for len(rightLines) < innerH {
			rightLines = append(rightLines, "")
		}
	} else if selected.IsDir {
		rightLines = append(rightLines, m.styles.MutedStyle.Render("(directory)"))
		rightLines = append(rightLines, "")
		rightLines = append(rightLines, lipgloss.NewStyle().Foreground(m.styles.MutedStyle.GetForeground()).Render("Press Enter or → to open folder"))
		if !m.isFileBrowserAtRoot() {
			rightLines = append(rightLines, lipgloss.NewStyle().Foreground(m.styles.MutedStyle.GetForeground()).Render("Press ← to go back"))
		} else {
			rightLines = append(rightLines, lipgloss.NewStyle().Foreground(m.styles.MutedStyle.GetForeground()).Render("Press Esc to close"))
		}
		for len(rightLines) < innerH {
			rightLines = append(rightLines, "")
		}
	} else {
		// File preview - read and display content
		content, err := os.ReadFile(selected.Path)
		if err != nil {
			rightLines = append(rightLines, m.styles.MutedStyle.Render("(cannot read file)"))
			for len(rightLines) < innerH {
				rightLines = append(rightLines, "")
			}
		} else {
			// Split into lines and handle scrolling
			lines := strings.Split(string(content), "\n")
			maxOffset := max(len(lines)-innerH+1, 0)
			if m.fileBrowserModalOffset > maxOffset {
				m.fileBrowserModalOffset = maxOffset
			}
			scrollInfo := fmt.Sprintf("j/k or wheel scroll (%d/%d)", m.fileBrowserModalOffset+1, maxOffset+1)

			rightLines = append(rightLines, m.styles.MutedStyle.Render(scrollInfo))
			rightLines = append(rightLines, "")
			// Show visible lines
			viewportH := innerH - 3
			viewportEnd := min(m.fileBrowserModalOffset+viewportH, len(lines))
			if viewportEnd > m.fileBrowserModalOffset {
				rightLines = append(rightLines, lines[m.fileBrowserModalOffset:viewportEnd]...)
			}

			// Fill remaining space
			for len(rightLines) < innerH {
				rightLines = append(rightLines, "")
			}
			// Trim if too long
			if len(rightLines) > innerH {
				rightLines = rightLines[:innerH]
			}
		}
	}

	rightContent := strings.Join(rightLines, "\n")
	rightPane := lipgloss.NewStyle().
		Width(rightW).
		Height(innerH).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(m.styles.CommandHighlightStyle.GetForeground()).
		Padding(0, 1).
		Render(rightContent)

	// Compose header and footer
	head := lipgloss.NewStyle().
		Width(max(width-6, 20)).
		Align(lipgloss.Center).
		Bold(true).
		Foreground(m.styles.CommandHighlightStyle.GetForeground()).
		Render("Browse Files")
	foot := "← back  •  → enter  •  ↑↓ navigate list  •  j/k or wheel scroll preview  •  Enter select  •  Esc close"
	if m.fileBrowserModalSearch != "" {
		foot += "  •  Backspace clear search"
	}

	body := head + "\n\n" + lipgloss.JoinHorizontal(lipgloss.Top, leftPane, " ", rightPane) + "\n\n" + foot

	modal := lipgloss.NewStyle().
		Width(width).
		Height(height).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(m.styles.CommandHighlightStyle.GetForeground()).
		Padding(1, 2).
		Render(body)

	return modal
}
