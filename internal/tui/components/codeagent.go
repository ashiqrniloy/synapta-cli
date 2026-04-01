package components

import (
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

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
}

// NewCodeAgentModel creates the model using the loaded AppConfig.
func NewCodeAgentModel(cfg *config.AppConfig) *CodeAgentModel {
	t := cfg.ActiveTheme()
	return &CodeAgentModel{
		styles:      theme.NewStyles(t),
		ta:          buildTextarea(t),
		borderColor: t.Border,
	}
}

func buildTextarea(t config.Theme) textarea.Model {
	ta := textarea.New()
	ta.Placeholder = "Type your message…"
	ta.ShowLineNumbers = false

	// Clean, minimal styling — no background fill, no cursor-line highlight.
	noBg := lipgloss.NewStyle().Foreground(lipgloss.Color(t.Foreground))
	placeholder := lipgloss.NewStyle().Foreground(lipgloss.Color(t.Muted))
	empty := lipgloss.NewStyle()

	ta.FocusedStyle.Base = noBg
	ta.FocusedStyle.Text = noBg
	ta.FocusedStyle.CursorLine = empty
	ta.FocusedStyle.CursorLineNumber = empty
	ta.FocusedStyle.EndOfBuffer = empty
	ta.FocusedStyle.Placeholder = placeholder
	ta.FocusedStyle.Prompt = empty
	ta.FocusedStyle.LineNumber = empty

	ta.BlurredStyle.Base = noBg
	ta.BlurredStyle.Text = noBg
	ta.BlurredStyle.CursorLine = empty
	ta.BlurredStyle.CursorLineNumber = empty
	ta.BlurredStyle.EndOfBuffer = empty
	ta.BlurredStyle.Placeholder = placeholder
	ta.BlurredStyle.Prompt = empty
	ta.BlurredStyle.LineNumber = empty

	// We'll handle Shift+Enter at the model level
	ta.KeyMap.InsertNewline.SetKeys() // Clear default keys

	ta.SetWidth(80)
	ta.SetHeight(1)
	ta.Focus()
	return ta
}

// isShiftEnter checks if the key message is Shift+Enter
func isShiftEnter(msg tea.KeyMsg) bool {
	// Shift+Enter produces KeyRunes with '\n' in most terminals
	return msg.Type == tea.KeyRunes && len(msg.Runes) > 0 && msg.Runes[0] == '\n'
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

	case tea.KeyMsg:
		// Ctrl+C to quit
		if msg.String() == "ctrl+c" {
			m.quit = true
			return m, tea.Quit
		}

		// Enter (without Shift) to submit
		if msg.Type == tea.KeyEnter {
			text := m.ta.Value()
			if text != "" {
				m.msgs = append(m.msgs, text)
			}
			m.ta.SetValue("")
			m.ta.SetHeight(1)
			return m, nil
		}

		// Shift+Enter to insert newline
		if isShiftEnter(msg) {
			// Pre-grow height BEFORE inserting newline
			// This ensures the viewport shows all lines including the new one
			newLineCount := m.ta.LineCount() + 1
			if newLineCount > m.ta.Height() {
				maxLines := 12
				if m.height > 0 && m.height < 30 {
					maxLines = m.height - 5
				}
				if maxLines < 3 {
					maxLines = 3
				}
				m.ta.SetHeight(min(newLineCount, maxLines))
			}
			// Now insert the newline
			m.ta, _ = m.ta.Update(tea.KeyMsg{
				Type:  tea.KeyRunes,
				Runes: []rune{'\n'},
			})
			return m, nil
		}
	}

	// Let the textarea process other keys
	var cmd tea.Cmd
	m.ta, cmd = m.ta.Update(msg)
	m = m.adjustHeight()

	return m, cmd
}

// adjustHeight ensures the textarea height accommodates all content
func (m CodeAgentModel) adjustHeight() CodeAgentModel {
	numLines := m.ta.LineCount()
	if numLines < 1 {
		numLines = 1
	}

	// Calculate max allowed height
	maxLines := 12
	if m.height > 0 && m.height < 30 {
		maxLines = m.height - 5
	}
	if maxLines < 3 {
		maxLines = 3
	}

	// Grow if needed
	currentHeight := m.ta.Height()
	if numLines > currentHeight {
		m.ta.SetHeight(min(numLines, maxLines))
	}

	return m
}

func (m CodeAgentModel) View() string {
	if m.quit {
		return ""
	}

	// ── Title ──
	title := m.styles.TitleStyle.Render("Synapta Code")

	// ── Input box ──
	// The border grows naturally with the textarea content.
	// We do NOT set an explicit height on the bordered style —
	// this follows Golden Rule #4 (let content determine size).
	taView := m.ta.View()
	borderStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(m.borderColor)).
		Padding(0, 1)
	inputBox := borderStyle.Render(taView)

	// ── Vertical spacer — push input to the bottom ──
	titleH := lipgloss.Height(title)
	inputH := lipgloss.Height(inputBox)

	spacer := ""
	if m.height > 0 {
		needed := m.height - titleH - inputH
		if needed > 0 {
			spacer = strings.Repeat("\n", needed)
		}
	}

	return title + spacer + inputBox
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
