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
}

// NewCodeAgentModel creates the model using the loaded AppConfig.
func NewCodeAgentModel(cfg *config.AppConfig) *CodeAgentModel {
	t := cfg.ActiveTheme()
	return &CodeAgentModel{
		styles:      theme.NewStyles(t),
		ta:          buildTextarea(t, cfg),
		borderColor: t.Border,
		cfg:         cfg,
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

	case tea.KeyPressMsg:
		keyStr := msg.String()

		// Check for quit key (default: ctrl+c)
		quitKey := m.getQuitKey()
		if keyStr == quitKey {
			m.quit = true
			return m, tea.Quit
		}

		// Check for submit key (default: enter)
		submitKey := m.getSubmitKey()
		if keyStr == submitKey {
			// Only submit if we have text
			text := m.ta.Value()
			if text != "" {
				m.msgs = append(m.msgs, text)
				m.ta.SetValue("")
			}
			return m, nil
		}

		// Handle Shift+Enter for newline
		// In v2 with keyboard enhancements, we can detect modifiers
		if msg.Code == tea.KeyEnter && msg.Mod.Contains(tea.ModShift) {
			// Insert newline manually
			m.ta.InsertRune('\n')
			return m, nil
		}

		// Handle Ctrl+M (which some terminals send for Enter with modifiers)
		// This is a fallback for terminals that don't support Kitty protocol
		if keyStr == "ctrl+m" || (msg.Code == 'm' && msg.Mod.Contains(tea.ModCtrl)) {
			// Check if shift is also held (via the text representation)
			// This is a workaround for terminals that don't report modifiers properly
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

	// ── Compose view ──
	var content string
	if m.height > 0 {
		// Calculate space needed
		titleH := lipgloss.Height(title)
		inputH := lipgloss.Height(inputBox)
		instructions := lipgloss.NewStyle().
			Foreground(m.styles.MutedStyle.GetForeground()).
			Render("  ↑/↓ navigate   Shift+Enter=newline   Enter=send   Ctrl+C=quit")
		instructionsH := lipgloss.Height(instructions)

		totalContentH := titleH + inputH + instructionsH + 2 // 2 for spacing
		availableH := m.height - totalContentH

		spacer := ""
		if availableH > 0 {
			spacer = strings.Repeat("\n", availableH)
		}

		content = title + "\n\n" + instructions + "\n" + spacer + inputBox
	} else {
		content = title + "\n\n" + inputBox
	}

	v := tea.NewView(content)
	v.AltScreen = true

	// Request keyboard enhancements for modifier key support (Shift+Enter, etc.)
	// This enables Kitty protocol support on compatible terminals
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
