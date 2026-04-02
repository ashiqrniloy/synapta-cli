package components

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/bubbles/v2/textarea"
	"charm.land/lipgloss/v2"

	"github.com/synapta/synapta-cli/internal/config"
	"github.com/synapta/synapta-cli/internal/tui/theme"
)

// Command represents an executable command in the palette.
type Command struct {
	ID          string
	Name        string
	Description string
}

// DefaultCommands returns the built-in command list.
func DefaultCommands() []Command {
	return []Command{
		{
			ID:          "add-provider",
			Name:        "Add Provider",
			Description: "Add a new LLM provider",
		},
		{
			ID:          "set-provider",
			Name:        "Set Provider",
			Description: "Change the active LLM provider",
		},
		{
			ID:          "set-model",
			Name:        "Set Model",
			Description: "Change the active model",
		},
	}
}

// CommandExecutedMsg is sent when a command is executed.
type CommandExecutedMsg struct {
	Command Command
}

// CommandModal is a modal overlay for selecting and executing commands.
type CommandModal struct {
	width     int
	height    int
	filter    textarea.Model
	commands  []Command
	filtered  []Command
	cursor    int
	styles    *theme.Styles
	t         config.Theme
	executed  bool
}

// NewCommandModal creates a new command modal.
func NewCommandModal(styles *theme.Styles, t config.Theme) *CommandModal {
	ta := textarea.New()
	ta.Placeholder = "Type to filter commands..."
	ta.ShowLineNumbers = false
	ta.DynamicHeight = false
	ta.SetHeight(1)

	// Style the textarea to match the main input
	placeholder := lipgloss.NewStyle().Foreground(lipgloss.Color(t.Muted))
	noBg := lipgloss.NewStyle().Foreground(lipgloss.Color(t.Foreground))
	empty := lipgloss.NewStyle()

	s := ta.Styles()
	s.Focused.Base = noBg
	s.Focused.Text = noBg
	s.Focused.CursorLine = empty
	s.Focused.CursorLineNumber = empty
	s.Focused.EndOfBuffer = empty
	s.Focused.Placeholder = placeholder
	s.Focused.Prompt = empty
	s.Focused.LineNumber = empty
	s.Blurred.Base = noBg
	s.Blurred.Text = noBg
	s.Blurred.CursorLine = empty
	s.Blurred.CursorLineNumber = empty
	s.Blurred.EndOfBuffer = empty
	s.Blurred.Placeholder = placeholder
	s.Blurred.Prompt = empty
	s.Blurred.LineNumber = empty
	ta.SetStyles(s)

	ta.Focus()

	cmds := DefaultCommands()
	return &CommandModal{
		filter:   ta,
		commands: cmds,
		filtered: cmds,
		styles:   styles,
		t:        t,
	}
}

// SetSize updates the modal dimensions.
func (m *CommandModal) SetSize(width, height int) {
	m.width = width
	m.height = height
	// Modal width is 60% of screen, max 60 chars
	modalWidth := min(width*60/100, 60)
	// Modal inner width: modalWidth - 2 (border) - 4 (padding) = modalWidth - 6
	// Filter box inner: needs to fit within modalInnerWidth - 4 (filter border + padding)
	// So filter textarea width = modalInnerWidth - 4 - 2 = modalWidth - 12
	m.filter.SetWidth(modalWidth - 12)
}

// filterCommands returns commands matching the current filter text.
func (m *CommandModal) filterCommands() {
	query := strings.ToLower(strings.TrimSpace(m.filter.Value()))
	if query == "" {
		m.filtered = m.commands
		return
	}

	var result []Command
	for _, cmd := range m.commands {
		name := strings.ToLower(cmd.Name)
		desc := strings.ToLower(cmd.Description)
		if strings.Contains(name, query) || strings.Contains(desc, query) {
			result = append(result, cmd)
		}
	}
	m.filtered = result

	// Reset cursor if out of bounds
	if m.cursor >= len(m.filtered) {
		m.cursor = 0
	}
	if len(m.filtered) > 0 && m.cursor < 0 {
		m.cursor = 0
	}
}

// Init implements tea.Model.
func (m *CommandModal) Init() tea.Cmd {
	return textarea.Blink
}

// Update implements tea.Model.
func (m *CommandModal) Update(msg tea.Msg) (*CommandModal, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		keyStr := msg.String()

		switch keyStr {
		case "up":
			if m.cursor > 0 {
				m.cursor--
			}
			return m, nil

		case "down":
			if m.cursor < len(m.filtered)-1 {
				m.cursor++
			}
			return m, nil

		case "enter":
			if len(m.filtered) > 0 {
				m.executed = true
				selected := m.filtered[m.cursor]
				return m, func() tea.Msg {
					return CommandExecutedMsg{Command: selected}
				}
			}
			return m, nil
		}

		// Pass other keys to the filter textarea
		var cmd tea.Cmd
		m.filter, cmd = m.filter.Update(msg)
		m.filterCommands()
		return m, cmd
	}

	return m, nil
}

// View implements tea.Model.
func (m *CommandModal) View() string {
	if m.executed {
		return ""
	}

	// Modal dimensions
	modalWidth := min(m.width*60/100, 60)
	modalHeight := min(m.height*50/100, 20)

	borderColor := lipgloss.Color(m.t.Border)
	fgColor := lipgloss.Color(m.t.Foreground)
	mutedColor := lipgloss.Color(m.t.Muted)

	// Title
	// Modal inner width: modalWidth - 2 (border) - 4 (padding 1,2) = modalWidth - 6
	modalInnerWidth := modalWidth - 6

	titleStyle := lipgloss.NewStyle().
		Foreground(fgColor).
		Bold(true).
		Width(modalInnerWidth - 2). // -2 for padding(0,1)
		Padding(0, 1)

	title := titleStyle.Render("Commands")

	// Filter input - must fit within modal inner width
	// Filter box: Width + 2 (border) + 2 (padding) = modalInnerWidth
	// So filter Width = modalInnerWidth - 4
	filterBorder := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Width(modalInnerWidth - 4).
		Padding(0, 1)

	filterView := filterBorder.Render(m.filter.View())

	// Command list
	listHeight := modalHeight - lipgloss.Height(title) - lipgloss.Height(filterView) - 4
	if listHeight < 1 {
		listHeight = 1
	}

	// Build list items
	var items []string
	for i, cmd := range m.filtered {
		if i >= listHeight {
			break
		}

		nameStyle := lipgloss.NewStyle().
			Foreground(fgColor).
			Bold(true)

		descStyle := lipgloss.NewStyle().
			Foreground(mutedColor)

		// Build the line
		name := nameStyle.Render(cmd.Name)
		desc := descStyle.Render(" — " + cmd.Description)
		line := name + desc

		// Pad to full width (modal inner width)
		lineWidth := lipgloss.Width(line)
		padding := max(modalInnerWidth-lineWidth, 1)
		line = line + strings.Repeat(" ", padding)

		// Apply highlight style to the selected item
		if i == m.cursor {
			line = m.styles.CommandHighlightStyle.Render(
				strings.Repeat(" ", modalInnerWidth),
			)
			// Re-render name and desc on top of highlighted background
			name = lipgloss.NewStyle().
				Foreground(fgColor).
				Bold(true).
				Background(m.styles.CommandHighlightStyle.GetBackground()).
				Render(cmd.Name)
			desc = lipgloss.NewStyle().
				Foreground(mutedColor).
				Background(m.styles.CommandHighlightStyle.GetBackground()).
				Render(" — " + cmd.Description)
			line = name + desc
			linePad := max(modalInnerWidth-lipgloss.Width(line), 1)
			line = lipgloss.NewStyle().
				Background(m.styles.CommandHighlightStyle.GetBackground()).
				Render(line + strings.Repeat(" ", linePad))
		}

		items = append(items, line)
	}

	// Fill empty slots
	for len(items) < listHeight {
		items = append(items, "")
	}

	listView := strings.Join(items, "\n")

	// Compose modal
	modalBorder := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Width(modalWidth).
		Padding(1, 2)

	modalContent := title + "\n" + filterView + "\n" + listView
	modal := modalBorder.Render(modalContent)

	// Center the modal on screen
	modalH := lipgloss.Height(modal)
	modalW := lipgloss.Width(modal)

	// Vertical centering
	topPad := max((m.height-modalH)/2, 0)
	// Horizontal centering
	leftPad := max((m.width-modalW)/2, 0)

	// Build the overlay with background dimming
	lines := strings.Split(modal, "\n")
	var result strings.Builder

	// Background overlay style (semi-transparent using background color)
	overlayBg := lipgloss.NewStyle().
		Foreground(mutedColor)

	// Top padding
	for i := 0; i < topPad; i++ {
		if m.width > 0 {
			result.WriteString(overlayBg.Render(strings.Repeat("·", m.width)))
		}
		result.WriteString("\n")
	}

	// Modal lines
	for _, line := range lines {
		if leftPad > 0 {
			result.WriteString(overlayBg.Render(strings.Repeat("·", leftPad)))
		}
		result.WriteString(line)
		// Right padding
		lineW := lipgloss.Width(line)
		rightPad := max(m.width-leftPad-lineW, 0)
		if rightPad > 0 {
			result.WriteString(overlayBg.Render(strings.Repeat("·", rightPad)))
		}
		result.WriteString("\n")
	}

	// Bottom padding
	for i := topPad + modalH; i < m.height; i++ {
		if m.width > 0 {
			result.WriteString(overlayBg.Render(strings.Repeat("·", m.width)))
		}
		result.WriteString("\n")
	}

	return strings.TrimRight(result.String(), "\n")
}
