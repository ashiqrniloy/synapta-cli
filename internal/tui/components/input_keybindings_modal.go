package components

import tea "charm.land/bubbletea/v2"

func (m *CodeAgentModel) handleKeybindingsModalKeyPress(_ tea.KeyPressMsg, keyStr, helpKey string) (bool, tea.Cmd) {
	if !m.keybindingsModalOpen {
		return false, nil
	}
	if keyStr == "esc" || keyStr == helpKey {
		m.keybindingsModalOpen = false
		return true, nil
	}
	if keyStr == "up" && m.keybindingsSelection > 0 {
		m.keybindingsSelection--
		return true, nil
	}
	if keyStr == "down" {
		rows := m.filteredKeybindingRows()
		if m.keybindingsSelection < len(rows)-1 {
			m.keybindingsSelection++
		}
		return true, nil
	}
	if keyStr == "pgup" || keyStr == "pageup" {
		m.keybindingsSelection -= 8
		if m.keybindingsSelection < 0 {
			m.keybindingsSelection = 0
		}
		return true, nil
	}
	if keyStr == "pgdown" || keyStr == "pagedown" {
		rows := m.filteredKeybindingRows()
		m.keybindingsSelection += 8
		if m.keybindingsSelection >= len(rows) {
			m.keybindingsSelection = max(len(rows)-1, 0)
		}
		return true, nil
	}
	if keyStr == "backspace" {
		r := []rune(m.keybindingsSearch)
		if len(r) > 0 {
			m.keybindingsSearch = string(r[:len(r)-1])
			m.keybindingsSelection = 0
		}
		return true, nil
	}
	if len([]rune(keyStr)) == 1 {
		m.keybindingsSearch += keyStr
		m.keybindingsSelection = 0
		return true, nil
	}
	return true, nil
}
