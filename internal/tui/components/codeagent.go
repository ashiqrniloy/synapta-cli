package components

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"charm.land/bubbles/v2/textarea"

	"github.com/synapta/synapta-cli/internal/config"
	"github.com/synapta/synapta-cli/internal/tui/theme"
)

// CodeAgentModel is the main TUI model for the Synapta Code agent.
type CodeAgentModel struct {
	width       int
	height      int
	styles      *theme.Styles
	ta          textarea.Model
	quit        bool
	borderColor string
	msgs        []string
	cfg         *config.AppConfig

	// Inline command picker
	picker *CommandPicker
}

// NewCodeAgentModel creates the model using the loaded AppConfig.
func NewCodeAgentModel(cfg *config.AppConfig) *CodeAgentModel {
	t := cfg.ActiveTheme()
	styles := theme.NewStyles(t)
	return &CodeAgentModel{
		styles:      styles,
		ta:          buildTextarea(t, cfg),
		borderColor: t.Border,
		cfg:         cfg,
		picker:      NewCommandPicker(styles),
	}
}

func buildTextarea(t config.Theme, cfg *config.AppConfig) textarea.Model {
	ta := textarea.New()
	ta.Placeholder = "Type your message... (Enter=send, Shift+Enter=newline)"
	ta.ShowLineNumbers = false

	// Enable dynamic height - textarea will grow/shrink based on content
	ta.DynamicHeight = true
	ta.MinHeight = 1
	ta.MaxHeight = 15 // Leave room for title and padding

	// Clean, minimal styling using lipgloss v2
	noBg := lipgloss.NewStyle().Foreground(lipgloss.Color(t.Foreground))
	placeholder := lipgloss.NewStyle().Foreground(lipgloss.Color(t.Muted))
	empty := lipgloss.NewStyle()

	// v2 uses ta.Styles() method instead of ta.FocusedStyle
	styles := ta.Styles()
	styles.Focused.Base = noBg
	styles.Focused.Text = noBg
	styles.Focused.CursorLine = empty
	styles.Focused.CursorLineNumber = empty
	styles.Focused.EndOfBuffer = empty
	styles.Focused.Placeholder = placeholder
	styles.Focused.Prompt = empty
	styles.Focused.LineNumber = empty

	styles.Blurred.Base = noBg
	styles.Blurred.Text = noBg
	styles.Blurred.CursorLine = empty
	styles.Blurred.CursorLineNumber = empty
	styles.Blurred.EndOfBuffer = empty
	styles.Blurred.Placeholder = placeholder
	styles.Blurred.Prompt = empty
	styles.Blurred.LineNumber = empty
	ta.SetStyles(styles)

	ta.SetWidth(80)
	ta.Focus()

	// In v2, Enter is the default for newline
	// We want Shift+Enter for newline and Enter for submit
	// So we disable the textarea's newline handling on Enter
	// and handle it ourselves
	ta.KeyMap.InsertNewline.SetKeys() // Remove all keys for insert newline
	// Shift+Enter will be handled in Update

	return ta
}

// normalizeKeyName converts config key names to bubbletea v2 format
func normalizeKeyName(key string) string {
	lower := strings.ToLower(key)
	replacer := strings.NewReplacer(
		"escape", "esc",
	)
	return replacer.Replace(lower)
}

// getSubmitKey returns the configured submit key (default: enter)
func (m *CodeAgentModel) getSubmitKey() string {
	if m.cfg != nil && m.cfg.Keybindings.Submit != "" {
		return normalizeKeyName(m.cfg.Keybindings.Submit)
	}
	return "enter"
}

// getQuitKey returns the configured quit key
func (m *CodeAgentModel) getQuitKey() string {
	if m.cfg != nil && m.cfg.Keybindings.Quit != "" {
		return normalizeKeyName(m.cfg.Keybindings.Quit)
	}
	return "ctrl+c"
}

// getFilterText returns the text used for filtering (after the ':' prefix).
func (m *CodeAgentModel) getFilterText() string {
	value := m.ta.Value()
	if strings.HasPrefix(value, ":") {
		return value[1:] // everything after ':'
	}
	return ""
}

// clearCommandMode clears the input and exits command mode.
func (m *CodeAgentModel) clearCommandMode() {
	m.ta.SetValue("")
	m.picker.Deactivate()
}

// ─── tea.Model ───────────────────────────────────────────────────────

func (m CodeAgentModel) Init() tea.Cmd {
	return textarea.Blink
}

func (m CodeAgentModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.ta.SetWidth(max(msg.Width-6, 40))
		return m, nil

	case CommandActionMsg:
		// Handle completed command
		m.clearCommandMode()

		if len(msg.Path) == 0 {
			return m, nil
		}

		commandID := msg.Path[0].ID
		switch commandID {
		case "add-provider":
			if len(msg.Path) > 1 {
				m.msgs = append(m.msgs, "[Add Provider] Selected: "+msg.Path[1].Name)
			}
		case "set-model":
			m.msgs = append(m.msgs, "[Command] Set Model — coming soon")
		}
		return m, nil

	case tea.KeyPressMsg:
		keyStr := msg.String()
		quitKey := m.getQuitKey()

		// ─── Command Mode Active ────────────────────────────────
		if m.picker.IsActive() {
			// Ctrl+Q exits command mode
			if keyStr == quitKey || keyStr == "ctrl+q" {
				m.clearCommandMode()
				return m, nil
			}

			// ESC goes back one level (or exits if at root)
			if keyStr == "esc" {
				if !m.picker.HandleBack() {
					// At root, exit command mode
					m.clearCommandMode()
				}
				return m, nil
			}

			// Up/Down navigation
			if keyStr == "up" {
				m.picker.MoveUp()
				return m, nil
			}
			if keyStr == "down" {
				m.picker.MoveDown()
				return m, nil
			}

			// Enter selects the highlighted item
			if keyStr == "enter" {
				completed := m.picker.HandleSelect()
				if completed {
					// Command fully selected
					path := m.picker.Path()
					m.picker.Deactivate()
					return m, func() tea.Msg {
						return CommandActionMsg{Path: path}
					}
				}
				// Navigated deeper, clear filter for new level
				m.ta.SetValue(":")
				return m, nil
			}

			// All other keys go to the textarea for filtering
			// But first, check if user deleted the ':'
			if keyStr == "backspace" {
				value := m.ta.Value()
				if len(value) <= 1 {
					// Deleting the ':', exit command mode
					m.clearCommandMode()
					return m, nil
				}
			}

			// Pass to textarea
			var cmd tea.Cmd
			m.ta, cmd = m.ta.Update(msg)

			// Update filter based on new text
			m.picker.Filter(m.getFilterText())
			return m, cmd
		}

		// ─── Normal Mode ────────────────────────────────────────

		// Check for quit key (default: ctrl+c)
		if keyStr == quitKey {
			m.quit = true
			return m, tea.Quit
		}

		// Check for ':' as first character to enter command mode
		if keyStr == ":" {
			value := m.ta.Value()
			if value == "" {
				// Enter command mode
				m.picker.Activate()
				m.ta.SetValue(":")
				return m, nil
			}
		}

		// Check for submit key (default: enter)
		submitKey := m.getSubmitKey()
		if keyStr == submitKey {
			// Only submit if we have text
			text := m.ta.Value()
			if text != "" && !strings.HasPrefix(text, ":") {
				m.msgs = append(m.msgs, text)
				m.ta.SetValue("")
			}
			return m, nil
		}

		// Handle Shift+Enter for newline
		if msg.Code == tea.KeyEnter && msg.Mod.Contains(tea.ModShift) {
			m.ta.InsertRune('\n')
			return m, nil
		}

		// Handle Ctrl+M (which some terminals send for Enter with modifiers)
		if keyStr == "ctrl+m" || (msg.Code == 'm' && msg.Mod.Contains(tea.ModCtrl)) {
			return m, nil
		}
	}

	// Let the textarea process other keys
	var cmd tea.Cmd
	m.ta, cmd = m.ta.Update(msg)

	return m, cmd
}

func (m CodeAgentModel) View() tea.View {
	if m.quit {
		return tea.NewView("")
	}

	// ── Title ──
	title := m.styles.TitleStyle.Render("Synapta Code")

	// ── Input box with border ──
	taView := m.ta.View()
	borderStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(m.borderColor)).
		Padding(0, 1)
	inputBox := borderStyle.Render(taView)

	// ── Command picker (inline, below input) ──
	pickerView := ""
	if m.picker.IsActive() {
		pickerView = m.picker.View(m.width - 6) // match input width
	}

	// ── Compose view ──
	var content string
	instructions := lipgloss.NewStyle().
		Foreground(m.styles.MutedStyle.GetForeground()).
		Render("  ↑/↓ navigate   Shift+Enter=newline   Enter=send   :=commands   Ctrl+C=quit")

	if m.height > 0 {
		titleH := lipgloss.Height(title)
		inputH := lipgloss.Height(inputBox)
		pickerH := lipgloss.Height(pickerView)
		instructionsH := lipgloss.Height(instructions)

		var totalContentH int
		if pickerView != "" {
			// Command mode: title, spacer, picker, input
			totalContentH = titleH + pickerH + inputH + 3
		} else {
			// Normal mode: title, instructions, spacer, input
			totalContentH = titleH + instructionsH + inputH + 3
		}

		availableH := m.height - totalContentH
		spacer := ""
		if availableH > 0 {
			spacer = strings.Repeat("\n", availableH)
		}

		if pickerView != "" {
			// Command mode: title, spacer (push picker+input to bottom), picker, input
			content = title + "\n" + spacer + pickerView + "\n\n" + inputBox
		} else {
			// Normal mode: title, instructions, spacer, input at bottom
			content = title + "\n\n" + instructions + "\n" + spacer + inputBox
		}
	} else {
		if pickerView != "" {
			content = title + "\n\n" + pickerView + "\n" + inputBox
		} else {
			content = title + "\n\n" + inputBox
		}
	}

	v := tea.NewView(content)
	v.AltScreen = true

	// Request keyboard enhancements for modifier key support (Shift+Enter, etc.)
	v.KeyboardEnhancements.ReportEventTypes = true

	return v
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
