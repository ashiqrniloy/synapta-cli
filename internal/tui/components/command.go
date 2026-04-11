package components

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"charm.land/lipgloss/v2"

	"github.com/ashiqrniloy/synapta-cli/internal/tui/theme"
)

// ─── Types ──────────────────────────────────────────────────────────

// CommandItem represents a selectable item in the command picker.
type CommandItem struct {
	ID   string // unique identifier
	Name string // display name
}

// CommandStep represents a selection made in the command path.
type CommandStep struct {
	Name string
	ID   string
}

// CommandDisplayName returns the display label for a command ID.
func CommandDisplayName(id string) string {
	for _, item := range DefaultCommands() {
		if item.ID == id {
			return item.Name
		}
	}
	return id
}

// ─── Data ──────────────────────────────────────────────────────────

// DefaultCommands returns all built-in commands, sorted alphabetically.
func DefaultCommands() []CommandItem {
	return []CommandItem{
		{ID: "add-provider", Name: "Add Provider"},
		{ID: "bash", Name: "Bash"},
		{ID: "browse-files", Name: "Browse Files"},
		{ID: "compact", Name: "Compact"},
		{ID: "context-manager", Name: "Context Manager"},
		{ID: "help", Name: "Help"},
		{ID: "new-session", Name: "New Session"},
		{ID: "resume-session", Name: "Resume Session"},
		{ID: "set-model", Name: "Set Model"},
	}
}

// AvailableProviders returns the list of providers.
func AvailableProviders() []CommandItem {
	return []CommandItem{
		{ID: "github-copilot", Name: "GitHub Copilot"},
		{ID: "kilo", Name: "Kilo Gateway"},
	}
}

// ModelsFromSlice converts a slice of ModelInfo to CommandItems.
func ModelsFromSlice(models []ModelInfo) []CommandItem {
	items := make([]CommandItem, len(models))
	for i, m := range models {
		items[i] = CommandItem{ID: m.Key(), Name: m.DisplayName()}
	}
	return items
}

// ModelInfo holds model display information.
type ModelInfo struct {
	Provider string
	ID       string
	Name     string
}

func (m ModelInfo) Key() string {
	return m.Provider + "::" + m.ID
}

func (m ModelInfo) DisplayName() string {
	return m.Name + " (" + m.Provider + ")"
}

// ─── Messages ──────────────────────────────────────────────────────

// CommandActionMsg is sent when a command is fully selected.
type CommandActionMsg struct {
	Path []CommandStep // breadcrumb path of selections
}

// ─── CommandPicker ─────────────────────────────────────────────────

// CommandPicker manages the inline command picker state.
type CommandPicker struct {
	active     bool
	rootItems  []CommandItem
	items      []CommandItem // current list
	filtered   []CommandItem // filtered list
	cursor     int           // selected index
	path       []CommandStep // selections made so far
	styles     *theme.Styles
	maxVisible int // items to show (fixed at 5)
}

// NewCommandPicker creates a new command picker.
func NewCommandPicker(styles *theme.Styles) *CommandPicker {
	root := DefaultCommands()
	return &CommandPicker{
		rootItems:  append([]CommandItem(nil), root...),
		items:      append([]CommandItem(nil), root...),
		filtered:   append([]CommandItem(nil), root...),
		styles:     styles,
		maxVisible: 5,
	}
}

// SetRootItems sets top-level items shown when command mode is activated.
func (cp *CommandPicker) SetRootItems(items []CommandItem) {
	if len(items) == 0 {
		items = DefaultCommands()
	}
	cp.rootItems = append([]CommandItem(nil), items...)
	if !cp.active || len(cp.path) == 0 {
		cp.items = append([]CommandItem(nil), cp.rootItems...)
		cp.filtered = append([]CommandItem(nil), cp.rootItems...)
		if cp.cursor >= len(cp.filtered) {
			cp.cursor = 0
		}
	}
}

// IsActive returns true if the command picker is active.
func (cp *CommandPicker) IsActive() bool {
	return cp.active
}

// Activate starts command mode with the initial command list.
func (cp *CommandPicker) Activate() {
	cp.active = true
	if len(cp.rootItems) == 0 {
		cp.rootItems = DefaultCommands()
	}
	cp.items = append([]CommandItem(nil), cp.rootItems...)
	cp.filtered = append([]CommandItem(nil), cp.rootItems...)
	cp.cursor = 0
	cp.path = nil
}

// BeginSubmenu puts the picker directly into a sub-menu for a command.
func (cp *CommandPicker) BeginSubmenu(commandID, commandName string) {
	if !cp.active {
		cp.Activate()
	}
	cp.path = []CommandStep{{Name: commandName, ID: commandID}}
	cp.items = nil
	cp.filtered = nil
	cp.cursor = 0
}

// Deactivate exits command mode.
func (cp *CommandPicker) Deactivate() {
	cp.active = false
	cp.items = nil
	cp.filtered = nil
	cp.cursor = 0
	cp.path = nil
}

// Filter updates the filtered list based on the query.
func (cp *CommandPicker) Filter(query string) {
	query = strings.ToLower(strings.TrimSpace(query))
	if len(cp.path) == 0 && utf8.RuneCountInString(query) < 3 {
		cp.filtered = nil
		cp.cursor = 0
		return
	}
	if query == "" {
		cp.filtered = cp.items
	} else {
		var result []CommandItem
		for _, item := range cp.items {
			if strings.Contains(strings.ToLower(item.Name), query) {
				result = append(result, item)
			}
		}
		cp.filtered = result
	}
	// Reset cursor if out of bounds
	if cp.cursor >= len(cp.filtered) {
		cp.cursor = 0
	}
}

// MoveUp moves the cursor up.
func (cp *CommandPicker) MoveUp() {
	if cp.cursor > 0 {
		cp.cursor--
	}
}

// MoveDown moves the cursor down.
func (cp *CommandPicker) MoveDown() {
	if cp.cursor < len(cp.filtered)-1 {
		cp.cursor++
	}
}

// Selected returns the currently selected item, or nil if none.
func (cp *CommandPicker) Selected() *CommandItem {
	if len(cp.filtered) == 0 {
		return nil
	}
	return &cp.filtered[cp.cursor]
}

// HandleSelect processes a selection and returns what action to take.
// Returns true if a command was fully executed, false if we navigated deeper.
func (cp *CommandPicker) HandleSelect() (completed bool) {
	selected := cp.Selected()
	if selected == nil {
		return false
	}

	// Record this selection in the path
	cp.path = append(cp.path, CommandStep{Name: selected.Name, ID: selected.ID})

	// Determine what to do based on the current level
	switch len(cp.path) {
	case 1:
		// Top-level command selected
		if selected.ID == "add-provider" {
			// Navigate to provider list
			cp.items = AvailableProviders()
			cp.filtered = cp.items
			cp.cursor = 0
			return false
		}
		if selected.ID == "set-model" || selected.ID == "resume-session" {
			// Return false - caller will load dynamic items via LoadItems()
			return false
		}
		// Other commands (including extensions) are complete
		return true
	case 2:
		// Second level (provider selected, model selected, etc.)
		return true
	default:
		return true
	}
}

// LoadItems loads items into the picker for selection.
func (cp *CommandPicker) LoadItems(items []CommandItem) {
	cp.items = items
	cp.filtered = items
	cp.cursor = 0
}

// LoadModels loads models into the picker for selection.
func (cp *CommandPicker) LoadModels(models []ModelInfo) {
	cp.LoadItems(ModelsFromSlice(models))
}

// HandleBack goes back one level in the command path.
// Returns true if we can go back, false if we're at the root.
func (cp *CommandPicker) HandleBack() bool {
	if len(cp.path) == 0 {
		// At root, nothing to go back to
		return false
	}

	// Remove last selection
	cp.path = cp.path[:len(cp.path)-1]

	// Reset to appropriate level
	if len(cp.path) == 0 {
		// Back to root commands
		cp.items = append([]CommandItem(nil), cp.rootItems...)
	} else {
		// Would need to handle deeper levels here if we add more
		cp.items = append([]CommandItem(nil), cp.rootItems...)
	}

	cp.filtered = cp.items
	cp.cursor = 0
	return true
}

// Path returns the current breadcrumb path.
func (cp *CommandPicker) Path() []CommandStep {
	return cp.path
}

// VisibleWindow returns the currently visible items and their start index.
func (cp *CommandPicker) VisibleWindow() ([]CommandItem, int) {
	if len(cp.filtered) == 0 {
		return nil, 0
	}
	if cp.maxVisible <= 0 || len(cp.filtered) <= cp.maxVisible {
		return cp.filtered, 0
	}

	start := cp.cursor - cp.maxVisible/2
	if start < 0 {
		start = 0
	}
	maxStart := len(cp.filtered) - cp.maxVisible
	if start > maxStart {
		start = maxStart
	}
	end := start + cp.maxVisible
	return cp.filtered[start:end], start
}

// Cursor returns the current cursor index.
func (cp *CommandPicker) Cursor() int {
	return cp.cursor
}

// TotalItems returns the total number of filtered items.
func (cp *CommandPicker) TotalItems() int {
	return len(cp.filtered)
}

// ─── View ──────────────────────────────────────────────────────────

// View renders the command picker.
func (cp *CommandPicker) View(width int) string {
	if !cp.active {
		return ""
	}

	styles := cp.styles
	fgColor := styles.CommandHighlightStyle.GetForeground()
	highlightBg := styles.CommandHighlightStyle.GetBackground()
	mutedFg := styles.MutedStyle.GetForeground()

	var lines []string

	// Header + breadcrumb
	header := "Commands"
	if len(cp.path) > 0 {
		parts := make([]string, 0, len(cp.path)+1)
		parts = append(parts, "Commands")
		for _, step := range cp.path {
			parts = append(parts, step.Name)
		}
		header = strings.Join(parts, "  ›  ")
	}
	headerStyle := lipgloss.NewStyle().
		Foreground(mutedFg).
		Bold(true).
		Width(width)
	lines = append(lines, headerStyle.Render(header))

	// Items
	visible, start := cp.VisibleWindow()
	for i, item := range visible {
		var line string
		absoluteIdx := start + i

		if absoluteIdx == cp.cursor {
			// Highlighted item
			nameStyle := lipgloss.NewStyle().
				Foreground(fgColor).
				Bold(true).
				Background(highlightBg)

			name := "▸ " + item.Name
			rendered := nameStyle.Render(name)

			// Pad to full width with highlight background
			lineWidth := lipgloss.Width(rendered)
			padding := max(width-lineWidth, 1)
			line = lipgloss.NewStyle().
				Background(highlightBg).
				Render(name + strings.Repeat(" ", padding))
		} else {
			// Normal item
			nameStyle := lipgloss.NewStyle().
				Foreground(mutedFg)
			line = nameStyle.Render("  " + item.Name)
		}

		lines = append(lines, line)
	}

	meta := fmt.Sprintf("↑↓ navigate  •  Enter select  •  Esc back")
	if len(cp.filtered) > cp.maxVisible {
		meta = fmt.Sprintf("%s  •  %d-%d of %d", meta, start+1, start+len(visible), len(cp.filtered))
	}
	lines = append(lines, lipgloss.NewStyle().Foreground(mutedFg).Render(meta))

	return strings.Join(lines, "\n")
}
