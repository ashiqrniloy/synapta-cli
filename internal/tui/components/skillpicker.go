package components

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/ashiqrniloy/synapta-cli/internal/core"
	"github.com/ashiqrniloy/synapta-cli/internal/tui/theme"
)

type SkillPicker struct {
	active     bool
	skills     []core.Skill
	filtered   []core.Skill
	cursor     int
	styles     *theme.Styles
	maxVisible int
}

func NewSkillPicker(styles *theme.Styles) *SkillPicker {
	return &SkillPicker{styles: styles, maxVisible: 5}
}

func (sp *SkillPicker) Activate(skills []core.Skill) {
	sp.active = true
	sp.skills = append([]core.Skill(nil), skills...)
	sp.filtered = append([]core.Skill(nil), skills...)
	sp.cursor = 0
}

func (sp *SkillPicker) Deactivate() {
	sp.active = false
	sp.skills = nil
	sp.filtered = nil
	sp.cursor = 0
}

func (sp *SkillPicker) IsActive() bool { return sp.active }

func (sp *SkillPicker) Filter(query string) {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		sp.filtered = append([]core.Skill(nil), sp.skills...)
	} else {
		filtered := make([]core.Skill, 0)
		for _, skill := range sp.skills {
			if strings.Contains(strings.ToLower(skill.Name), query) || strings.Contains(strings.ToLower(skill.Description), query) {
				filtered = append(filtered, skill)
			}
		}
		sp.filtered = filtered
	}
	if sp.cursor >= len(sp.filtered) {
		sp.cursor = 0
	}
}

func (sp *SkillPicker) MoveUp() {
	if sp.cursor > 0 {
		sp.cursor--
	}
}

func (sp *SkillPicker) MoveDown() {
	if sp.cursor < len(sp.filtered)-1 {
		sp.cursor++
	}
}

func (sp *SkillPicker) Selected() *core.Skill {
	if len(sp.filtered) == 0 {
		return nil
	}
	return &sp.filtered[sp.cursor]
}

func (sp *SkillPicker) VisibleWindow() ([]core.Skill, int) {
	if len(sp.filtered) == 0 {
		return nil, 0
	}
	if sp.maxVisible <= 0 || len(sp.filtered) <= sp.maxVisible {
		return sp.filtered, 0
	}
	start := sp.cursor - sp.maxVisible/2
	if start < 0 {
		start = 0
	}
	maxStart := len(sp.filtered) - sp.maxVisible
	if start > maxStart {
		start = maxStart
	}
	end := start + sp.maxVisible
	return sp.filtered[start:end], start
}

func (sp *SkillPicker) View(width int) string {
	if !sp.active {
		return ""
	}

	styles := sp.styles
	fgColor := styles.CommandHighlightStyle.GetForeground()
	highlightBg := styles.CommandHighlightStyle.GetBackground()
	mutedFg := styles.MutedStyle.GetForeground()

	lines := []string{
		lipgloss.NewStyle().Foreground(mutedFg).Bold(true).Width(width).Render("Skills (@name)"),
	}

	visible, start := sp.VisibleWindow()
	for i, skill := range visible {
		absoluteIdx := start + i
		label := fmt.Sprintf("%s — %s", skill.Name, skill.Description)
		if absoluteIdx == sp.cursor {
			line := "▸ " + label
			rendered := lipgloss.NewStyle().Foreground(fgColor).Background(highlightBg).Bold(true).Render(line)
			padding := max(width-lipgloss.Width(rendered), 1)
			lines = append(lines, lipgloss.NewStyle().Background(highlightBg).Render(line+strings.Repeat(" ", padding)))
		} else {
			lines = append(lines, lipgloss.NewStyle().Foreground(mutedFg).Render("  "+label))
		}
	}

	meta := "↑↓ navigate  •  Enter select  •  Esc cancel"
	if len(sp.filtered) > sp.maxVisible {
		meta = fmt.Sprintf("%s  •  %d-%d of %d", meta, start+1, start+len(visible), len(sp.filtered))
	}
	lines = append(lines, lipgloss.NewStyle().Foreground(mutedFg).Render(meta))
	return strings.Join(lines, "\n")
}
