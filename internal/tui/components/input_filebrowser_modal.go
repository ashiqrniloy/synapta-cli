package components

import (
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"
)

func (m *CodeAgentModel) handleFileBrowserModalKeyPress(msg tea.KeyPressMsg, keyStr string) (bool, tea.Cmd) {
	if !m.fileBrowserModalOpen {
		return false, nil
	}

	// Escape: close modal if at root, otherwise go back
	if keyStr == "esc" {
		if m.isFileBrowserAtRoot() {
			m.closeFileBrowserModal()
		} else {
			m.fileBrowserGoBack()
		}
		return true, nil
	}

	// Left arrow: go back to parent directory
	if keyStr == "left" {
		if !m.fileBrowserGoBack() {
			// At root, close the modal
			m.closeFileBrowserModal()
		}
		return true, nil
	}

	// Right arrow: navigate into directory
	if keyStr == "right" {
		selected := m.fileBrowserModalSelectedEntry()
		if selected != nil && selected.IsDir {
			m.fileBrowserNavigateInto(selected.Path)
		}
		return true, nil
	}

	// Up arrow: move cursor up in file list OR scroll preview up
	if keyStr == "up" {
		if m.fileBrowserModalCursor > 0 {
			m.fileBrowserModalCursor--
			// Reset preview offset when selection changes
			m.fileBrowserModalOffset = 0
		} else {
			// At top of list, scroll preview up if file is selected
			selected := m.fileBrowserModalSelectedEntry()
			if selected != nil && !selected.IsDir && m.fileBrowserModalOffset > 0 {
				m.fileBrowserModalOffset--
			}
		}
		return true, nil
	}

	// Down arrow: move cursor down in file list OR scroll preview down
	if keyStr == "down" {
		if m.fileBrowserModalCursor < len(m.fileBrowserModalEntries)-1 {
			m.fileBrowserModalCursor++
			// Reset preview offset when selection changes
			m.fileBrowserModalOffset = 0
		} else {
			// At bottom of list, scroll preview down if file is selected
			selected := m.fileBrowserModalSelectedEntry()
			if selected != nil && !selected.IsDir {
				// Calculate max offset based on file size
				content, err := os.ReadFile(selected.Path)
				if err == nil {
					lines := strings.Split(string(content), "\n")
					maxOffset := max(len(lines)-1, 0)
					if m.fileBrowserModalOffset < maxOffset {
						m.fileBrowserModalOffset++
					}
				}
			}
		}
		return true, nil
	}

	// Backspace: delete character from search or navigate back
	if keyStr == "backspace" {
		if len(m.fileBrowserModalSearch) > 0 {
			r := []rune(m.fileBrowserModalSearch)
			m.fileBrowserModalSearch = string(r[:len(r)-1])
			// Apply filter
			m.fileBrowserModalEntries = m.filterFileBrowserEntries(m.fileBrowserModalSearch)
			m.fileBrowserModalCursor = 0
			m.fileBrowserModalOffset = 0
		} else if !m.isFileBrowserAtRoot() {
			// If search is empty and not at root, go back
			m.fileBrowserGoBack()
		}
		return true, nil
	}

	// Enter: select file or navigate into directory
	if keyStr == "enter" {
		selected := m.fileBrowserModalSelectedEntry()
		if selected != nil {
			if selected.IsDir {
				m.fileBrowserNavigateInto(selected.Path)
			} else {
				// File selected - add to context and close modal
				m.closeFileBrowserModal()
				return true, func() tea.Msg {
					return FileAddedMsg{Path: selected.Path}
				}
			}
		}
		return true, nil
	}

	// Regular character input for search (single character keys)
	if len([]rune(keyStr)) == 1 {
		m.fileBrowserModalSearch += keyStr
		// Apply filter
		m.fileBrowserModalEntries = m.filterFileBrowserEntries(m.fileBrowserModalSearch)
		m.fileBrowserModalCursor = 0
		m.fileBrowserModalOffset = 0
		return true, nil
	}

	return true, nil
}
