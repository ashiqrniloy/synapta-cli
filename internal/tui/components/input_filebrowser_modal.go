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

	if keyStr == "esc" {
		if m.isFileBrowserAtRoot() {
			m.closeFileBrowserModal()
		} else {
			m.fileBrowserGoBack()
		}
		return true, nil
	}

	if keyStr == "left" {
		if !m.fileBrowserGoBack() {
			m.closeFileBrowserModal()
		}
		return true, nil
	}

	if keyStr == "right" {
		selected := m.fileBrowserModalSelectedEntry()
		if selected != nil && selected.IsDir {
			m.fileBrowserNavigateInto(selected.Path)
		}
		return true, nil
	}

	if keyStr == "up" {
		if m.fileBrowserModalCursor > 0 {
			m.fileBrowserModalCursor--
			m.fileBrowserModalOffset = 0
		} else {
			selected := m.fileBrowserModalSelectedEntry()
			if selected != nil && !selected.IsDir && m.fileBrowserModalOffset > 0 {
				m.fileBrowserModalOffset--
			}
		}
		return true, nil
	}

	if keyStr == "down" {
		if m.fileBrowserModalCursor < len(m.fileBrowserModalEntries)-1 {
			m.fileBrowserModalCursor++
			m.fileBrowserModalOffset = 0
		} else {
			selected := m.fileBrowserModalSelectedEntry()
			if selected != nil && !selected.IsDir {
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

	// Vim-style preview scroll
	if keyStr == "j" {
		m.fileBrowserModalOffset++
		return true, nil
	}
	if keyStr == "k" {
		if m.fileBrowserModalOffset > 0 {
			m.fileBrowserModalOffset--
		}
		return true, nil
	}

	if keyStr == "backspace" {
		if len(m.fileBrowserModalSearch) > 0 {
			r := []rune(m.fileBrowserModalSearch)
			m.fileBrowserModalSearch = string(r[:len(r)-1])
			m.fileBrowserModalEntries = m.filterFileBrowserEntries(m.fileBrowserModalSearch)
			m.fileBrowserModalCursor = 0
			m.fileBrowserModalOffset = 0
		} else if !m.isFileBrowserAtRoot() {
			m.fileBrowserGoBack()
		}
		return true, nil
	}

	if keyStr == "enter" {
		selected := m.fileBrowserModalSelectedEntry()
		if selected != nil {
			if selected.IsDir {
				m.fileBrowserNavigateInto(selected.Path)
			} else {
				m.closeFileBrowserModal()
				return true, func() tea.Msg { return FileAddedMsg{Path: selected.Path} }
			}
		}
		return true, nil
	}

	if len([]rune(keyStr)) == 1 {
		m.fileBrowserModalSearch += keyStr
		m.fileBrowserModalEntries = m.filterFileBrowserEntries(m.fileBrowserModalSearch)
		m.fileBrowserModalCursor = 0
		m.fileBrowserModalOffset = 0
		return true, nil
	}

	return true, nil
}
