package theme

import (
	"charm.land/lipgloss/v2"

	"github.com/synapta/synapta-cli/internal/config"
)

// Styles holds all lipgloss styles derived from the active theme.
type Styles struct {
	TitleStyle    lipgloss.Style
	SubtitleStyle lipgloss.Style
	BorderStyle   lipgloss.Style
	ErrorStyle    lipgloss.Style
	SuccessStyle  lipgloss.Style
	MutedStyle    lipgloss.Style
	BaseStyle     lipgloss.Style
}

// NewStyles creates a Styles set from the given theme config.
func NewStyles(t config.Theme) *Styles {
	primary := lipgloss.Color(t.Primary)
	muted := lipgloss.Color(t.Muted)
	border := lipgloss.Color(t.Border)
	errorC := lipgloss.Color(t.Error)

	return &Styles{
		BaseStyle: lipgloss.NewStyle(),
		TitleStyle: lipgloss.NewStyle().
			Foreground(primary).
			Bold(true).
			PaddingLeft(1),
		SubtitleStyle: lipgloss.NewStyle().
			Foreground(muted),
		BorderStyle: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(border).
			Padding(1, 2),
		ErrorStyle: lipgloss.NewStyle().
			Foreground(errorC),
		SuccessStyle: lipgloss.NewStyle().
			Foreground(primary),
		MutedStyle: lipgloss.NewStyle().
			Foreground(muted),
	}
}
