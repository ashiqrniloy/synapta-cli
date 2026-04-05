package theme

import (
	"image/color"

	"charm.land/lipgloss/v2"

	"github.com/synapta/synapta-cli/internal/config"
)

// Styles holds all lipgloss styles derived from the active theme.
type Styles struct {
	TitleStyle                lipgloss.Style
	SubtitleStyle             lipgloss.Style
	BorderStyle               lipgloss.Style
	ErrorStyle                lipgloss.Style
	SuccessStyle              lipgloss.Style
	MutedStyle                lipgloss.Style
	BaseStyle                 lipgloss.Style
	CommandHighlightStyle     lipgloss.Style
	InteractionHighlightStyle lipgloss.Style
	SystemMessageStyle        lipgloss.Style
}

// NewStyles creates a Styles set from the given theme config.
func NewStyles(t config.Theme) *Styles {
	primary := lipgloss.Color(t.Primary)
	muted := lipgloss.Color(t.Muted)
	border := lipgloss.Color(t.Border)
	errorC := lipgloss.Color(t.Error)

	// Highlight style: blend highlight color toward theme background
	// Higher opacity = more visible highlight color
	highlightColor := t.HighlightColor
	if highlightColor == "" {
		highlightColor = t.Primary
	}
	highlightOpacity := t.HighlightOpacity
	if highlightOpacity == 0 {
		highlightOpacity = 0.2
	}
	fgColor := lipgloss.Color(t.Foreground)
	baseBgColor := lipgloss.Color(t.Background)
	hlColor := lipgloss.Color(highlightColor)

	// Create highlight background by blending highlight color toward base background
	// opacity 0.0 = pure background, opacity 1.0 = pure highlight color
	highlightBg := blendToward(hlColor, baseBgColor, 1.0-highlightOpacity)

	// Interaction highlight style (for user messages)
	interactionHighlightColor := t.InteractionHighlightColor
	if interactionHighlightColor == "" {
		interactionHighlightColor = t.Accent
	}
	interactionHighlightOpacity := t.InteractionHighlightOpacity
	if interactionHighlightOpacity == 0 {
		interactionHighlightOpacity = 0.15
	}
	intHlColor := lipgloss.Color(interactionHighlightColor)
	interactionBg := blendToward(intHlColor, baseBgColor, 1.0-interactionHighlightOpacity)

	systemMessageColor := t.SystemMessageColor
	if systemMessageColor == "" {
		systemMessageColor = t.Secondary
	}
	systemMessageOpacity := t.SystemMessageOpacity
	if systemMessageOpacity == 0 {
		systemMessageOpacity = 0.22
	}
	sysColor := lipgloss.Color(systemMessageColor)
	systemBg := blendToward(sysColor, baseBgColor, 1.0-systemMessageOpacity)

	return &Styles{
		BaseStyle: lipgloss.NewStyle(),
		TitleStyle: lipgloss.NewStyle().
			Foreground(primary).
			Bold(true),
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
		CommandHighlightStyle: lipgloss.NewStyle().
			Foreground(fgColor).
			Background(highlightBg).
			Bold(true),
		InteractionHighlightStyle: lipgloss.NewStyle().
			Foreground(fgColor).
			Background(interactionBg),
		SystemMessageStyle: lipgloss.NewStyle().
			Foreground(fgColor).
			Background(systemBg).
			Padding(0, 1),
	}
}

// blendToward blends color 'a' toward color 'b' by the given ratio.
// ratio 0.0 returns pure 'a', ratio 1.0 returns pure 'b'.
func blendToward(a, b color.Color, ratio float64) color.Color {
	aR, aG, aB, _ := a.RGBA()
	bR, bG, bB, _ := b.RGBA()

	// Convert from 0-65535 to 0-255 range
	aR8, aG8, aB8 := uint8(aR>>8), uint8(aG>>8), uint8(aB>>8)
	bR8, bG8, bB8 := uint8(bR>>8), uint8(bG>>8), uint8(bB>>8)

	rr := uint8(float64(aR8)*(1-ratio) + float64(bR8)*ratio)
	gg := uint8(float64(aG8)*(1-ratio) + float64(bG8)*ratio)
	bb := uint8(float64(aB8)*(1-ratio) + float64(bB8)*ratio)

	return color.RGBA{R: rr, G: gg, B: bb, A: 255}
}
