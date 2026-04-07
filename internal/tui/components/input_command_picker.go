package components

import tea "charm.land/bubbletea/v2"

func (m *CodeAgentModel) handleCommandPickerKeyPress(msg tea.KeyPressMsg, keyStr, quitKey string) (bool, tea.Cmd) {
	if !m.picker.IsActive() {
		return false, nil
	}
	if keyStr == quitKey || keyStr == "ctrl+q" {
		m.clearCommandMode()
		return true, nil
	}
	if keyStr == "esc" {
		if !m.picker.HandleBack() {
			m.clearCommandMode()
		} else {
			m.recalculateLayout()
		}
		return true, nil
	}
	if keyStr == "up" {
		m.picker.MoveUp()
		return true, nil
	}
	if keyStr == "down" {
		m.picker.MoveDown()
		return true, nil
	}
	if keyStr == "enter" {
		if commandID := m.commandShortcutCommandID(); commandID != "" {
			m.picker.Deactivate()
			return true, func() tea.Msg {
				return CommandActionMsg{Path: []CommandStep{{Name: commandID, ID: commandID}}}
			}
		}
		selected := m.picker.Selected()
		completed := m.picker.HandleSelect()
		if completed {
			path := m.picker.Path()
			m.picker.Deactivate()
			return true, func() tea.Msg { return CommandActionMsg{Path: path} }
		}
		if selected != nil && selected.ID == "set-model" {
			return true, m.loadModels()
		}
		if selected != nil && selected.ID == "resume-session" {
			return true, m.loadSessions()
		}
		m.ta.SetValue(":")
		return true, nil
	}
	if keyStr == "backspace" && len(m.ta.Value()) <= 1 {
		m.clearCommandMode()
		return true, nil
	}
	var cmd tea.Cmd
	m.ta, cmd = m.ta.Update(msg)
	m.picker.Filter(m.getFilterText())
	m.recalculateLayout()
	return true, cmd
}
