package components

import (
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/synapta/synapta-cli/internal/config"
	"github.com/synapta/synapta-cli/internal/tui/theme"
)

// TitleArt is the ASCII art "Synapta Code" title.
const TitleArt = `  ________              ______      _ _ _            _____
 / /_  __/_  ____  ____/ / __ \____(_) (_)___  ____/_  __/
/ / / / / / / __ \/ __  / /_/ / ___/ / / / __ \/ __ \/ /___
/ / / / /_/ / /_/ / /_/ / ____/ /  / / / / / / /_/ / / ___/
/_/ /_/\__,_/\____/\__,_/_/   /_/  /_/_/_/ /_/\____/_/_____/

         ____          __        __
        / __ \__  __  / /_  ___ / /___ _   _____  _____
       / / / / / / / / __ \/ _ \ / / __ \ | / / _ \/ ___/
      / ____/ /_/ / / /_/ /  __/ / / /_/ / |/ /  __/ /
     /_/    \__, /_/_.___/\___/_/_/\____/|___/\___/_/
          /____/                                     
`

// CodeAgentModel is the main TUI model for the Synapta Code agent.
type CodeAgentModel struct {
	width  int
	height int
	styles *theme.Styles
	ta     textarea.Model
	quit   bool
	msgs   []string // conversation history
}

// NewCodeAgentModel creates the model using the loaded AppConfig.
func NewCodeAgentModel(cfg *config.AppConfig) *CodeAgentModel {
	t := cfg.ActiveTheme()
	return &CodeAgentModel{
		styles: theme.NewStyles(t),
		ta:     buildTextarea(cfg, t),
	}
}

func buildTextarea(cfg *config.AppConfig, t config.Theme) textarea.Model {
	ta := textarea.New()
	ta.Placeholder = "Type your message…  (Shift+Enter for new line · Enter to send)"
	ta.ShowLineNumbers = false
	ta.FocusedStyle.Base = lipgloss.NewStyle().Foreground(lipgloss.Color(t.Foreground))
	ta.FocusedStyle.Text = lipgloss.NewStyle().Foreground(lipgloss.Color(t.Foreground))
	ta.FocusedStyle.CursorLine = lipgloss.NewStyle().
		Foreground(lipgloss.Color(t.CursorFG)).Background(lipgloss.Color(t.CursorBG)).Bold(true)
	ta.FocusedStyle.Placeholder = lipgloss.NewStyle().Foreground(lipgloss.Color(t.Muted))
	ta.FocusedStyle.Prompt = lipgloss.NewStyle().Foreground(lipgloss.Color(t.Primary))

	// Swap Enter / Shift+Enter behaviour:
	//   Enter    → submit  (handled at model level, so we remove it from textarea)
	//   ShiftEnt  → newline (bind it here)
	ta.KeyMap.InsertNewline.SetKeys("shift+enter")
	ta.SetWidth(80)
	ta.SetHeight(4)
	ta.Focus()
	return ta
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
		m.ta.SetWidth(max(msg.Width-4, 40))
		return m, nil

	case tea.KeyMsg:
		// Quit
		if msg.String() == "ctrl+c" {
			m.quit = true
			return m, tea.Quit
		}
		// Submit — enter pressed (not consumed by textarea since we bound
		// InsertNewline to shift+enter only).
		if msg.String() == "enter" || msg.Type == tea.KeyEnter {
			text := m.ta.Value()
			if text != "" {
				m.msgs = append(m.msgs, text)
			}
			m.ta.SetValue("")
			m.ta.Focus()
			return m, nil
		}
	}

	var cmd tea.Cmd
	m.ta, cmd = m.ta.Update(msg)
	return m, cmd
}

func (m CodeAgentModel) View() string {
	if m.quit {
		return ""
	}

	title := m.styles.TitleStyle.Render(TitleArt)
	input := m.styles.BorderStyle.Render(m.ta.View())

	// Spacer to push input toward the bottom
	main := lipgloss.JoinVertical(lipgloss.Left, title, input)
	if m.height > 0 {
		gap := m.height - lipgloss.Height(title) - lipgloss.Height(input) - 2
		for i := 0; i < max(gap, 0); i++ {
			main += "\n"
		}
	}

	return m.styles.BaseStyle.Render(main)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
