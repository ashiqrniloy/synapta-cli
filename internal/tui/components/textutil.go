package components

import (
	"strings"

	"charm.land/lipgloss/v2"
)

func countLines(s string) int {
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return 0
	}
	return strings.Count(s, "\n") + 1
}

func limitLines(lines []string, maxLines int) []string {
	if maxLines <= 0 {
		return []string{}
	}
	if len(lines) <= maxLines {
		return lines
	}
	if maxLines == 1 {
		return []string{truncateLine(lines[0], 1)}
	}
	out := append([]string{}, lines[:maxLines-1]...)
	out = append(out, lipgloss.NewStyle().Foreground(lipgloss.Color("244")).Render("…"))
	return out
}

func truncateLine(s string, maxLen int) string {
	if maxLen <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= maxLen {
		return s
	}
	return lipgloss.NewStyle().MaxWidth(maxLen).Render(s)
}

func looksLikeMarkdown(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	if strings.Contains(s, "```") {
		return true
	}
	markers := []string{"# ", "## ", "### ", "- ", "* ", "1. ", "> ", "|", "**", "__", "`"}
	for _, marker := range markers {
		if strings.Contains(s, "\n"+marker) || strings.HasPrefix(s, marker) || strings.Contains(s, marker) {
			return true
		}
	}
	return false
}

func wrapMultiline(text string, width int) []string {
	if text == "" {
		return []string{""}
	}
	rows := strings.Split(text, "\n")
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		wrapped := wordWrap(row, width)
		if len(wrapped) == 0 {
			out = append(out, "")
			continue
		}
		out = append(out, wrapped...)
	}
	return out
}

func wordWrap(text string, width int) []string {
	if width <= 0 || len(text) == 0 {
		return []string{text}
	}

	var lines []string
	for len(text) > width {
		// Find last space before width
		breakAt := width
		for i := width; i > 0; i-- {
			if text[i-1] == ' ' {
				breakAt = i
				break
			}
		}
		lines = append(lines, text[:breakAt])
		text = strings.TrimLeft(text[breakAt:], " ")
	}
	if len(text) > 0 {
		lines = append(lines, text)
	}
	return lines
}
