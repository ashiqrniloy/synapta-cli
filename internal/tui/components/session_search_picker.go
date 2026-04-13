package components

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"charm.land/lipgloss/v2"

	"github.com/ashiqrniloy/synapta-cli/internal/tui/theme"
)

// SearchMatch represents a single search match in the chat transcript.
type SearchMatch struct {
	MessageIdx   int    // Index into m.chatMessages
	LineIdx      int    // Which rendered line within the message
	RenderedLine int    // Absolute line number in the full rendered transcript
	Content      string // The text content of this line
	Context      string // Truncated context for display
	Role         string // "user", "assistant", "tool", "system"
	ToolName     string // Tool name if role is "tool"
}

// SessionSearchPicker manages the inline session search state.
type SessionSearchPicker struct {
	active      bool
	matches     []SearchMatch
	filtered    []SearchMatch
	cursor      int
	styles      *theme.Styles
	maxVisible  int
	searchQuery string
}

// NewSessionSearchPicker creates a new session search picker.
func NewSessionSearchPicker(styles *theme.Styles) *SessionSearchPicker {
	return &SessionSearchPicker{styles: styles, maxVisible: 6}
}

// IsActive returns true if the search picker is active.
func (sp *SessionSearchPicker) IsActive() bool {
	return sp.active
}

// Activate starts the search mode with the given rendered transcript lines.
func (sp *SessionSearchPicker) Activate(messages []ChatMessage, renderedLines []string, messageStartLines []int) {
	sp.active = true
	sp.cursor = 0
	sp.searchQuery = ""
	sp.matches = nil
	sp.filtered = nil

	// Build matches by finding all lines that belong to each message
	if len(messages) > 0 && len(messageStartLines) > 0 {
		for msgIdx, msg := range messages {
			var startLine, endLine int
			if msgIdx < len(messageStartLines)-1 {
				startLine = messageStartLines[msgIdx]
				endLine = messageStartLines[msgIdx+1] - 1
			} else if msgIdx < len(renderedLines) {
				startLine = messageStartLines[msgIdx]
				endLine = len(renderedLines) - 1
			}

			role := msg.Role
			toolName := msg.ToolName
			if role == "" {
				role = "unknown"
			}

			for lineIdx := startLine; lineIdx <= endLine && lineIdx < len(renderedLines); lineIdx++ {
				line := renderedLines[lineIdx]
				if strings.TrimSpace(line) == "" {
					continue
				}

				// Strip ANSI codes for content matching
				plainLine := ansi.Strip(line)
				if strings.TrimSpace(plainLine) == "" {
					continue
				}

				match := SearchMatch{
					MessageIdx:   msgIdx,
					LineIdx:      lineIdx - startLine,
					RenderedLine: lineIdx,
					Content:      plainLine,
					Role:         role,
					ToolName:     toolName,
				}

				// Create display context
				displayLine := plainLine
				if len(displayLine) > 80 {
					displayLine = displayLine[:80] + "…"
				}
				match.Context = displayLine

				sp.matches = append(sp.matches, match)
			}
		}
	}
	sp.filtered = sp.matches
}

// Deactivate exits search mode.
func (sp *SessionSearchPicker) Deactivate() {
	sp.active = false
	sp.matches = nil
	sp.filtered = nil
	sp.cursor = 0
	sp.searchQuery = ""
}

// Filter updates the filtered matches based on the query.
func (sp *SessionSearchPicker) Filter(query string) {
	sp.searchQuery = query
	query = strings.ToLower(strings.TrimSpace(query))

	if query == "" {
		sp.filtered = sp.matches
	} else {
		filtered := make([]SearchMatch, 0)
		for _, match := range sp.matches {
			if strings.Contains(strings.ToLower(match.Content), query) {
				filtered = append(filtered, match)
			}
		}
		sp.filtered = filtered
	}

	// Reset cursor if out of bounds
	if sp.cursor >= len(sp.filtered) {
		sp.cursor = 0
	}
}

// MoveUp moves the cursor up.
func (sp *SessionSearchPicker) MoveUp() {
	if sp.cursor > 0 {
		sp.cursor--
	}
}

// MoveDown moves the cursor down.
func (sp *SessionSearchPicker) MoveDown() {
	if sp.cursor < len(sp.filtered)-1 {
		sp.cursor++
	}
}

// Selected returns the currently selected match, or nil if none.
func (sp *SessionSearchPicker) Selected() *SearchMatch {
	if len(sp.filtered) == 0 {
		return nil
	}
	return &sp.filtered[sp.cursor]
}

// Cursor returns the current cursor index.
func (sp *SessionSearchPicker) Cursor() int {
	return sp.cursor
}

// TotalMatches returns the total number of filtered matches.
func (sp *SessionSearchPicker) TotalMatches() int {
	return len(sp.filtered)
}

// Query returns the current search query.
func (sp *SessionSearchPicker) Query() string {
	return sp.searchQuery
}

// VisibleWindow returns the currently visible matches and their start index.
func (sp *SessionSearchPicker) VisibleWindow() ([]SearchMatch, int) {
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
	if end > len(sp.filtered) {
		end = len(sp.filtered)
	}
	return sp.filtered[start:end], start
}

// View returns only the results panel (not the input - input is handled by textarea).
func (sp *SessionSearchPicker) View(width int) string {
	if !sp.active {
		return ""
	}

	styles := sp.styles
	fgColor := styles.CommandHighlightStyle.GetForeground()
	highlightBg := lipgloss.Color("238")
	mutedFg := styles.MutedStyle.GetForeground()

	lines := []string{}

	if len(sp.filtered) == 0 && sp.searchQuery != "" {
		lines = append(lines, lipgloss.NewStyle().Foreground(mutedFg).Render("  No matches found"))
		lines = append(lines, "")
		lines = append(lines, lipgloss.NewStyle().Foreground(mutedFg).Render("  ↑↓ navigate  •  Enter jump  •  Esc close"))
		return strings.Join(lines, "\n")
	}

	// Render matches
	visible, start := sp.VisibleWindow()
	for i, match := range visible {
		absoluteIdx := start + i
		prefix := "  "
		if absoluteIdx == sp.cursor && len(sp.filtered) > 0 {
			prefix = "▸ "
		}

		// Role badge
		roleLabel := match.Role
		if match.Role == "tool" && match.ToolName != "" {
			roleLabel = strings.ToUpper(match.ToolName)
		}

		roleColor := mutedFg
		switch match.Role {
		case "user":
			roleColor = lipgloss.Color("12") // Blue
		case "assistant":
			roleColor = lipgloss.Color("10") // Green
		case "tool":
			roleColor = lipgloss.Color("13") // Magenta/purple
		case "system":
			roleColor = lipgloss.Color("14") // Cyan
		}

		// Highlight the matching text in the content
		content := match.Context
		if sp.searchQuery != "" {
			content = highlightSearchMatch(content, sp.searchQuery)
		}

		if absoluteIdx == sp.cursor && len(sp.filtered) > 0 {
			// Selected row
			rowStyle := lipgloss.NewStyle().
				Foreground(fgColor).
				Background(highlightBg)
			line := prefix + lipgloss.NewStyle().Foreground(roleColor).Bold(true).Render(roleLabel) + "  " + content
			padded := truncateSearchLine(line, width)
			padding := max(width-len(ansi.Strip(padded)), 1)
			lines = append(lines, rowStyle.Render(padded+strings.Repeat(" ", padding)))
		} else {
			// Normal row
			line := prefix + lipgloss.NewStyle().Foreground(roleColor).Render(roleLabel) + "  " + content
			lines = append(lines, lipgloss.NewStyle().Foreground(mutedFg).Render(truncateSearchLine(line, width)))
		}
	}

	// Navigation hint
	if len(lines) > 0 {
		lines = append(lines, "")
	}
	meta := "↑↓ navigate  •  Enter jump  •  Esc close"
	if len(sp.filtered) > sp.maxVisible {
		meta = fmt.Sprintf("%d-%d of %d  •  %s", start+1, start+len(visible), len(sp.filtered), meta)
	} else if len(sp.filtered) > 0 {
		meta = fmt.Sprintf("%d matches  •  %s", len(sp.filtered), meta)
	}
	lines = append(lines, lipgloss.NewStyle().Foreground(mutedFg).Render(meta))

	return strings.Join(lines, "\n")
}

// highlightSearchMatch wraps the matching substring with Lipgloss Bold for highlighting.
func highlightSearchMatch(text, query string) string {
	lowerText := strings.ToLower(text)
	lowerQuery := strings.ToLower(query)
	idx := strings.Index(lowerText, lowerQuery)
	if idx < 0 {
		return text
	}

	before := text[:idx]
	match := text[idx : idx+len(query)]
	after := text[idx+len(query):]

	highlightStyle := lipgloss.NewStyle().Bold(true).Underline(true)
	return before + highlightStyle.Render(match) + after
}

// truncateSearchLine truncates a String to maxLen, accounting for ANSI codes.
func truncateSearchLine(s string, maxLen int) string {
	if maxLen <= 0 {
		return ""
	}
	// Strip ANSI codes for length calculation
	plain := ansi.Strip(s)
	if len(plain) <= maxLen {
		return s
	}
	// Truncate plain text
	plain = plain[:maxLen-1] + "…"
	return plain
}
