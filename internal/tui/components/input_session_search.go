package components

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
	tea "charm.land/bubbletea/v2"
)

// handleSessionSearchKeyPress handles keyboard events when session search is active.
func (m *CodeAgentModel) handleSessionSearchKeyPress(msg tea.KeyPressMsg, keyStr string) (bool, tea.Cmd) {
	if m.sessionSearch == nil || !m.sessionSearch.IsActive() {
		return false, nil
	}

	if keyStr == "esc" {
		m.sessionSearch.Deactivate()
		m.sessionSearchHighlightLine = -1
		m.ta.SetValue("")
		m.ta.Placeholder = "Type your message... (Enter=send, Shift+Enter/Ctrl+N=newline)"
		m.recalculateLayout()
		return true, nil
	}

	if keyStr == "up" {
		m.sessionSearch.MoveUp()
		m.updateSearchHighlightLine()
		m.refreshChatViewport()
		return true, nil
	}

	if keyStr == "down" {
		m.sessionSearch.MoveDown()
		m.updateSearchHighlightLine()
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

	// First, build the rendered lines to get accurate line numbers
	m.chatRenderedLines = nil
	m.chatMessageStartLines = nil

	// Calculate the actual line offset in the rendered transcript
	// by simulating what renderChatTranscript does
	sep := "\n\n"
	if m.isCompactDensity() {
		sep = "\n"
	}

	var currentLine int
	highlightFound := false

	for msgIdx, msg := range m.chatMessages {
		m.chatMessageStartLines = append(m.chatMessageStartLines, currentLine)

		var rendered string
		switch msg.Role {
		case "user":
			rendered = m.renderUserMessage(msg.Content)
		case "assistant":
			rendered = m.renderAssistantMessage(msg.Content)
		case "tool":
			rendered = m.renderToolMessage(msg)
		case "system":
			rendered = m.renderSystemMessage(msg)
		default:
			rendered = msg.Content
		}

		// Count lines in this rendered block
		msgLines := strings.Split(rendered, "\n")

		// Check if the match is in this message by comparing content
		matchContent := ansi.Strip(match.Content)
		for lineIdx, line := range msgLines {
			plainLine := ansi.Strip(line)
			// Check if this line matches our search match
			if strings.Contains(matchContent, plainLine) || strings.Contains(plainLine, matchContent) {
				if !highlightFound && currentLine+lineIdx == match.RenderedLine {
					// Found the matching line in the rendered output
					m.chatViewport.SetYOffset(currentLine + lineIdx)
					highlightFound = true
					break
				}
			}
		}

		currentLine += len(msgLines)

		// Add separator between messages
		if msgIdx < len(m.chatMessages)-1 {
			sepLines := strings.Split(sep, "\n")
			currentLine += len(sepLines)
		}
	}

	// Fallback: if we couldn't find exact match, just use the RenderedLine from search
	if !highlightFound {
		// Try to find the line by matching the original search match
		matchContent := strings.ToLower(ansi.Strip(match.Content))
		currentLine = 0
		for _, msg := range m.chatMessages {
			var rendered string
			switch msg.Role {
			case "user":
				rendered = m.renderUserMessage(msg.Content)
			case "assistant":
				rendered = m.renderAssistantMessage(msg.Content)
			case "tool":
				rendered = m.renderToolMessage(msg)
			case "system":
				rendered = m.renderSystemMessage(msg)
			default:
				rendered = msg.Content
			}

			msgLines := strings.Split(rendered, "\n")
			for lineIdx, line := range msgLines {
				plainLine := strings.ToLower(ansi.Strip(line))
				if strings.Contains(plainLine, matchContent) {
					m.chatViewport.SetYOffset(currentLine + lineIdx)
					m.sessionSearchHighlightLine = currentLine + lineIdx
					m.chatAutoScroll = false
					return
				}
			}
			currentLine += len(msgLines)
		}
	}

	m.chatAutoScroll = false

	// Optionally select the message containing the match
	if match.MessageIdx >= 0 && match.MessageIdx < len(m.chatMessages) {
		// Clear existing selection
		m.selectedToolCallID = ""
		// Select this message's tool if it has one
		msg := m.chatMessages[match.MessageIdx]
		if msg.Role == "tool" && msg.ToolCallID != "" {
			m.selectedToolCallID = msg.ToolCallID
		}
	}

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
	m.chatRenderedLines = nil
	m.chatMessageStartLines = nil

	if len(m.chatMessages) == 0 {
		return
	}

	// Get max width for rendering
	maxWidth := max(m.chatViewport.Width(), 20)
	sep := "\n\n"
	if m.isCompactDensity() {
		sep = "\n"
	}

	for msgIdx, msg := range m.chatMessages {
		// Record the starting line for this message
		m.chatMessageStartLines = append(m.chatMessageStartLines, len(m.chatRenderedLines))

		var rendered string
		switch msg.Role {
		case "user":
			rendered = m.renderUserMessage(msg.Content)
		case "assistant":
			rendered = m.renderAssistantMessage(msg.Content)
		case "tool":
			rendered = m.renderToolMessage(msg)
		case "system":
			rendered = m.renderSystemMessage(msg)
		default:
			rendered = msg.Content
		}

		// Strip the border styling to get clean lines
		lines := extractSearchContentLines(rendered, maxWidth)
		m.chatRenderedLines = append(m.chatRenderedLines, lines...)

		// Add separator between messages
		if msgIdx < len(m.chatMessages)-1 {
			sepLines := strings.Split(sep, "\n")
			m.chatRenderedLines = append(m.chatRenderedLines, sepLines...)
		}
	}
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
