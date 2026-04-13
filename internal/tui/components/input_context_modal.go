package components

import (
	"strings"

	tea "charm.land/bubbletea/v2"
)

func (m *CodeAgentModel) handleContextModalKeyPress(msg tea.KeyPressMsg, keyStr string) (bool, tea.Cmd) {
	if !m.contextModalOpen {
		return false, nil
	}
	if keyStr == "esc" {
		if m.contextModalEditMode {
			m.contextModalEditMode = false
			m.contextModalEditorHint = ""
		} else {
			m.closeContextModal()
		}
		return true, nil
	}
	if m.contextModalEditMode {
		if keyStr == "ctrl+s" {
			if m.contextModalSelection >= 0 && m.contextModalSelection < len(m.contextModalEntries) {
				entry := m.contextModalEntries[m.contextModalSelection]
				m.applyContextEntryEdit(entry, strings.TrimSpace(m.contextModalEditor.Value()))
				// applyContextEntryEdit mutates conversationHistory, which
				// marks the cache dirty. Force a fresh build now so the modal
				// list reflects the edit immediately.
				m.markContextEntriesDirty()
				m.contextModalEntries = m.contextEntries()
				m.contextModalEditMode = false
				m.contextModalEditorHint = "Saved"
			}
			return true, nil
		}
		var cmd tea.Cmd
		m.contextModalEditor, cmd = m.contextModalEditor.Update(msg)
		return true, cmd
	}

	if keyStr == "up" {
		if m.contextModalSelection > 0 {
			m.contextModalSelection--
			m.contextModalPreviewOffset = 0
		}
		return true, nil
	}
	if keyStr == "down" {
		if m.contextModalSelection < len(m.contextModalEntries)-1 {
			m.contextModalSelection++
			m.contextModalPreviewOffset = 0
		}
		return true, nil
	}
	if keyStr == "k" {
		if m.contextModalPreviewOffset > 0 {
			m.contextModalPreviewOffset--
		}
		return true, nil
	}
	if keyStr == "j" {
		if m.contextModalPreviewOffset < m.contextModalMaxPreviewOffset() {
			m.contextModalPreviewOffset++
		}
		return true, nil
	}
	if keyStr == "d" || keyStr == "backspace" {
		if m.contextModalSelection >= 0 && m.contextModalSelection < len(m.contextModalEntries) {
			entry := m.contextModalEntries[m.contextModalSelection]
			if entry.Removable {
				m.removeContextEntry(entry)
				// removeContextEntry mutates conversationHistory, which marks
				// the cache dirty. Force a fresh build for the modal.
				m.markContextEntriesDirty()
				m.contextModalEntries = m.contextEntries()
				if m.contextModalSelection >= len(m.contextModalEntries) && m.contextModalSelection > 0 {
					m.contextModalSelection--
				}
			}
		}
		return true, nil
	}
	if keyStr == "e" || keyStr == "enter" {
		if m.contextModalSelection >= 0 && m.contextModalSelection < len(m.contextModalEntries) {
			entry := m.contextModalEntries[m.contextModalSelection]
			if entry.Editable {
				m.contextModalEditMode = true
				m.contextModalEditor.SetValue(entry.Content)
				m.contextModalEditor.CursorEnd()
				m.contextModalEditorHint = ""
			}
		}
		return true, nil
	}
	if keyStr == "c" {
		m.recordContextAction("Manual compaction triggered from context manager")
		m.closeContextModal()
		m.appendSystemMessage("[Compact] Running manual compaction...", "working")
		return true, m.manualCompactCmd()
	}
	return true, nil
}
