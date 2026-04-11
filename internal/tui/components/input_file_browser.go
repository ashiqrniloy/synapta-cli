package components

import tea "charm.land/bubbletea/v2"

// fileBrowserFilterQuery returns the filter query from the textarea value.
// When the file browser is active, we expect "/" to be at the start.
func fileBrowserFilterQuery(text string) string {
	if text == "/" {
		return ""
	}
	if len(text) > 1 && text[0] == '/' {
		return text[1:]
	}
	return ""
}

func (m *CodeAgentModel) handleFileBrowserKeyPress(msg tea.KeyPressMsg, keyStr string) (bool, tea.Cmd) {
	if m.fileBrowser == nil || !m.fileBrowser.IsActive() {
		return false, nil
	}

	if keyStr == "esc" {
		m.fileBrowser.Deactivate()
		m.ta.SetValue("")
		m.recalculateLayout()
		return true, nil
	}

	if keyStr == "left" {
		if m.fileBrowser.NavigateBack() {
			m.recalculateLayout()
		}
		return true, nil
	}

	if keyStr == "up" {
		m.fileBrowser.MoveUp()
		return true, nil
	}

	if keyStr == "down" {
		m.fileBrowser.MoveDown()
		return true, nil
	}

	if keyStr == "enter" {
		selected := m.fileBrowser.Selected()
		if selected == nil {
			return true, nil
		}

		if selected.IsDir {
			// Navigate into the directory
			if m.fileBrowser.NavigateInto(selected.Path) {
				// Update the input to reflect current path
				query := fileBrowserFilterQuery(m.ta.Value())
				m.ta.SetValue("/" + query)
				m.recalculateLayout()
			}
		} else {
			// File selected - send message to add it to context
			m.fileBrowser.Deactivate()
			m.ta.SetValue("")
			m.recalculateLayout()
			// Return the selected file path as a message
			return true, func() tea.Msg {
				return FileAddedMsg{Path: selected.Path}
			}
		}
		return true, nil
	}

	// Backspace at the beginning - deactivate file browser
	if keyStr == "backspace" && len(m.ta.Value()) <= 1 {
		m.fileBrowser.Deactivate()
		m.ta.SetValue("")
		m.recalculateLayout()
		return true, nil
	}

	// Regular text input - filter the file list
	var cmd tea.Cmd
	m.ta, cmd = m.ta.Update(msg)
	
	// Update filter query
	query := fileBrowserFilterQuery(m.ta.Value())
	m.fileBrowser.Filter(query)
	m.recalculateLayout()
	return true, cmd
}