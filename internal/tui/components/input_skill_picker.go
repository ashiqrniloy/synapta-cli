package components

import tea "charm.land/bubbletea/v2"

func (m *CodeAgentModel) handleSkillPickerKeyPress(msg tea.KeyPressMsg, keyStr string) (bool, tea.Cmd) {
	if m.skillPicker == nil || !m.skillPicker.IsActive() {
		return false, nil
	}
	if keyStr == "esc" {
		m.skillPicker.Deactivate()
		m.recalculateLayout()
		return true, nil
	}
	if keyStr == "up" {
		m.skillPicker.MoveUp()
		return true, nil
	}
	if keyStr == "down" {
		m.skillPicker.MoveDown()
		return true, nil
	}
	if keyStr == "enter" {
		selected := m.skillPicker.Selected()
		if selected != nil {
			m.ta.SetValue(replaceActiveSkillMention(m.ta.Value(), selected.Name))
		}
		m.skillPicker.Deactivate()
		m.recalculateLayout()
		return true, nil
	}
	var cmd tea.Cmd
	m.ta, cmd = m.ta.Update(msg)
	m.updateSkillPickerFromInput()
	m.recalculateLayout()
	return true, cmd
}
