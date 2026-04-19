package components

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// handleSessionSearchKeyPress handles keyboard events when session search is active.
func (m *CodeAgentModel) handleSessionSearchKeyPress(msg tea.KeyPressMsg, keyStr string) (bool, tea.Cmd) {
	if m.sessionSearch == nil || !m.sessionSearch.IsActive() {
		return false, nil
	}

	if keyStr == "esc" {
		m.sessionSearch.Deactivate()
		m.sessionSearchHighlightLine = -1
		m.invalidateTranscriptCacheAll()
		m.ta.SetValue("")
		m.ta.Placeholder = "Type your message... (Enter=send, Shift+Enter/Ctrl+N=newline)"
		m.recalculateLayout()
		return true, nil
	}

	if keyStr == "up" {
		m.sessionSearch.MoveUp()
		m.updateSearchHighlightLine()
		m.invalidateTranscriptCacheAll()
		m.refreshChatViewport()
		return true, nil
	}

	if keyStr == "down" {
		m.sessionSearch.MoveDown()
		m.updateSearchHighlightLine()
		m.invalidateTranscriptCacheAll()
		m.refreshChatViewport()
		return true, nil
	}

	if keyStr == "enter" {
		selected := m.sessionSearch.Selected()
		if selected != nil {
			// Jump to the selected match in the chat viewport
			m.jumpToSearchMatch(selected)
			m.sessionSearchHighlightLine = selected.RenderedLine
			m.sessionSearch.Deactivate()
			m.invalidateTranscriptCacheAll()
			m.ta.SetValue("")
			m.ta.Placeholder = "Type your message... (Enter=send, Shift+Enter/Ctrl+N=newline)"
			m.recalculateLayout()
		}
		return true, nil
	}

	if keyStr == "backspace" && len(m.sessionSearch.Query()) > 0 {
		// Delete last character from search query
		newQuery := m.sessionSearch.Query()[:len(m.sessionSearch.Query())-1]
		m.sessionSearch.Filter(newQuery)
		// Update textarea to show updated search query
		if newQuery == "" {
			m.ta.SetValue("> ")
		} else {
			m.ta.SetValue("> " + newQuery)
		}
		m.updateSearchHighlightLine()
		m.recalculateLayout()
		return true, nil
	}

	// Handle regular character input for search
	// In Bubbletea v2, msg.Text is a string (not []rune)
	text := msg.Text
	if text != "" {
		// Don't allow navigation keys to be typed
		if keyStr == "up" || keyStr == "down" || keyStr == "left" || keyStr == "right" ||
			keyStr == "tab" || keyStr == "backtab" || keyStr == "enter" {
			return true, nil
		}

		newQuery := m.sessionSearch.Query() + text
		m.sessionSearch.Filter(newQuery)
		// Update textarea to show the search query with "> " prefix
		m.ta.SetValue("> " + newQuery)
		m.updateSearchHighlightLine()
		m.recalculateLayout()
		return true, nil
	}

	return true, nil
}

// updateSearchHighlightLine updates the highlight line based on current cursor position.
func (m *CodeAgentModel) updateSearchHighlightLine() {
	selected := m.sessionSearch.Selected()
	if selected != nil {
		m.sessionSearchHighlightLine = selected.RenderedLine
	} else {
		m.sessionSearchHighlightLine = -1
	}
}

// jumpToSearchMatch scrolls the chat viewport to show the matched line.
func (m *CodeAgentModel) jumpToSearchMatch(match *SearchMatch) {
	if match == nil {
		return
	}

	// Ensure transcript caches/line offsets are up to date.
	_ = m.renderChatTranscript()

	targetLine := match.RenderedLine
	if targetLine < 0 {
		targetLine = 0
	}
	m.chatViewport.SetYOffset(targetLine)
	m.chatAutoScroll = false

	// Optionally select the message containing the match.
	if match.MessageIdx >= 0 && match.MessageIdx < len(m.chatMessages) {
		m.selectedToolCallID = ""
		msg := m.chatMessages[match.MessageIdx]
		if msg.Role == "tool" && msg.ToolCallID != "" {
			m.selectedToolCallID = msg.ToolCallID
		}
	}

	m.invalidateTranscriptCacheAll()
	m.refreshChatViewport()
}

// activateSessionSearch activates the session search mode.
func (m *CodeAgentModel) activateSessionSearch() {
	if m.sessionSearch == nil {
		return
	}

	// Build rendered lines and message start positions from current chat messages
	m.buildChatRenderedLines()

	// Activate the search picker with the current transcript
	m.sessionSearch.Activate(m.chatMessages, m.chatRenderedLines, m.chatMessageStartLines)

	// Update textarea to show search prompt - use "> " prefix
	m.ta.SetValue("> ")
	m.ta.Placeholder = "Search session..."
	m.recalculateLayout()
}

// buildChatRenderedLines builds the flattened list of rendered lines from chat messages.
// This is used to map search matches back to viewport positions.
func (m *CodeAgentModel) buildChatRenderedLines() {
	transcript := m.renderChatTranscript()
	_ = transcript
	if len(m.transcriptBlocks) == 0 {
		m.chatRenderedLines = nil
		m.chatMessageStartLines = nil
		return
	}

	sep := "\n\n"
	if m.isCompactDensity() {
		sep = "\n"
	}

	lines := make([]string, 0, countLines(m.transcriptContent))
	for i, block := range m.transcriptBlocks {
		clean := extractSearchContentLines(block, max(m.chatViewport.Width(), 20))
		lines = append(lines, clean...)
		if i < len(m.transcriptBlocks)-1 {
			lines = append(lines, strings.Split(sep, "\n")...)
		}
	}
	m.chatRenderedLines = lines
	m.chatMessageStartLines = append([]int(nil), m.transcriptMessageStartLine...)
}

// extractSearchContentLines extracts clean text lines from a rendered block.
func extractSearchContentLines(rendered string, maxWidth int) []string {
	// Remove border lines (those starting with box-drawing characters)
	lines := strings.Split(rendered, "\n")
	cleanLines := make([]string, 0, len(lines))

	for _, line := range lines {
		// Skip empty lines at start
		if len(cleanLines) == 0 && strings.TrimSpace(line) == "" {
			continue
		}
		// Strip ANSI codes for search matching
		plain := ansi.Strip(line)
		cleanLines = append(cleanLines, plain)
	}

	// Trim trailing empty lines
	for len(cleanLines) > 0 && strings.TrimSpace(cleanLines[len(cleanLines)-1]) == "" {
		cleanLines = cleanLines[:len(cleanLines)-1]
	}

	if len(cleanLines) == 0 {
		return []string{""}
	}

	return cleanLines
}
