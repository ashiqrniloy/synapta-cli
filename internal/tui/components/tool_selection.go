package components

import "strings"

func (m *CodeAgentModel) selectedOrLastToolCallID() (string, bool) {
	if id := strings.TrimSpace(m.selectedToolCallID); id != "" {
		for i := len(m.chatMessages) - 1; i >= 0; i-- {
			msg := m.chatMessages[i]
			if msg.Role == "tool" && msg.ToolCallID == id {
				return id, true
			}
		}
		m.selectedToolCallID = ""
	}
	for i := len(m.chatMessages) - 1; i >= 0; i-- {
		msg := m.chatMessages[i]
		if msg.Role == "tool" && msg.ToolCallID != "" {
			return msg.ToolCallID, true
		}
	}
	return "", false
}

func (m *CodeAgentModel) toolMessageIndices() []int {
	indices := make([]int, 0)
	for i, msg := range m.chatMessages {
		if msg.Role == "tool" && strings.TrimSpace(msg.ToolCallID) != "" {
			indices = append(indices, i)
		}
	}
	return indices
}

func (m *CodeAgentModel) selectedToolPosition() (int, bool) {
	id := strings.TrimSpace(m.selectedToolCallID)
	if id == "" {
		return 0, false
	}
	indices := m.toolMessageIndices()
	for pos, idx := range indices {
		if m.chatMessages[idx].ToolCallID == id {
			return pos, true
		}
	}
	m.selectedToolCallID = ""
	return 0, false
}

func (m *CodeAgentModel) stepSelectedTool(delta int) bool {
	indices := m.toolMessageIndices()
	if len(indices) == 0 {
		return false
	}
	curPos, ok := m.selectedToolPosition()
	if !ok {
		if delta < 0 {
			curPos = len(indices) - 1
		} else {
			curPos = 0
		}
	} else {
		curPos += delta
		if curPos < 0 {
			curPos = 0
		}
		if curPos >= len(indices) {
			curPos = len(indices) - 1
		}
	}
	newID := m.chatMessages[indices[curPos]].ToolCallID
	if strings.TrimSpace(newID) == "" {
		return false
	}
	m.selectedToolCallID = newID
	m.chatAutoScroll = false
	m.refreshChatViewport()
	return true
}
