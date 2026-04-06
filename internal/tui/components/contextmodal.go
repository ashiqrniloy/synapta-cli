package components

import (
	"fmt"
	"strings"
	"time"

	"github.com/ashiqrniloy/synapta-cli/internal/llm"
)

func (m *CodeAgentModel) openContextModal() {
	m.contextModalOpen = true
	m.contextModalEditMode = false
	m.contextModalSelection = 0
	m.contextModalPreviewOffset = 0
	m.contextModalEntries = m.buildContextEntries()
}

func (m *CodeAgentModel) closeContextModal() {
	m.contextModalOpen = false
	m.contextModalEditMode = false
	m.contextModalEntries = nil
	m.contextModalEditorHint = ""
	m.contextModalPreviewOffset = 0
}

func (m *CodeAgentModel) buildContextEntries() []ContextEntry {
	if m.contextManager == nil {
		return nil
	}
	msgs, err := m.contextManager.Build(m.conversationHistory)
	if err != nil {
		return nil
	}
	historyIdx := make([]int, 0)
	for i, msg := range m.conversationHistory {
		if (msg.Role == "user" || msg.Role == "assistant") && strings.TrimSpace(msg.Content) != "" {
			historyIdx = append(historyIdx, i)
		}
	}
	timestamps := []time.Time{}
	if m.sessionStore != nil {
		timestamps = m.sessionStore.ContextMessageTimestamps()
	}
	entries := make([]ContextEntry, 0, len(msgs))
	hPos := 0
	order := 0
	for i, msg := range msgs {
		category := categorizeContextMessage(msg)
		if strings.TrimSpace(msg.Content) == "" && msg.Role != "system" {
			continue
		}
		entry := ContextEntry{
			Order:           order + 1,
			ContextIndex:    i,
			Role:            msg.Role,
			Content:         strings.TrimSpace(msg.Content),
			HistoryIndex:    -1,
			RawHistoryIndex: -1,
			Category:        category,
			Timestamp:       time.Time{},
			EstimatedTokens: estimateTextTokens(strings.TrimSpace(msg.Content)),
		}
		if msg.Role == "system" && category == "system-prompt" {
			entry.Editable = true
			entry.Removable = false
		} else if msg.Role != "system" && hPos < len(historyIdx) {
			entry.HistoryIndex = hPos
			entry.RawHistoryIndex = historyIdx[hPos]
			entry.Editable = true
			entry.Removable = true
			if hPos < len(timestamps) {
				entry.Timestamp = timestamps[hPos]
			}
			hPos++
		}
		entry.Label = contextEntryLabel(msg, category, entry.Timestamp)
		if entry.Category == "compacted-output" {
			entry.Editable = false
			entry.Removable = false
		}
		entries = append(entries, entry)
		order++
	}
	return entries
}

func categorizeContextMessage(msg llm.Message) string {
	content := strings.TrimSpace(msg.Content)
	if msg.Role == "system" {
		return "system-prompt"
	}
	if strings.HasPrefix(content, "The conversation history before this point was compacted") {
		return "compacted-output"
	}
	if strings.HasPrefix(content, "<summary>") || strings.HasPrefix(content, "## Goal") {
		return "compacted-output"
	}
	if strings.Contains(content, "<skill name=") {
		return "skills"
	}
	if msg.Role == "tool" {
		switch strings.ToLower(strings.TrimSpace(msg.Name)) {
		case "read":
			return "files-read"
		case "write":
			return "files-written"
		case "bash":
			return "tool-bash"
		default:
			return "tool-output"
		}
	}
	if msg.Role == "assistant" {
		return "llm-output"
	}
	if msg.Role == "user" {
		return "user-input"
	}
	return "context"
}

func contextEntryLabel(msg llm.Message, category string, ts time.Time) string {
	content := strings.TrimSpace(msg.Content)
	stamp := ""
	if ts.IsZero() {
		ts = time.Now()
	}
	stamp = ts.Local().Format("15:04:05")

	switch category {
	case "user-input":
		return "User Input · " + stamp
	case "llm-output":
		return "LLM Output · " + stamp
	case "compacted-output":
		return "Compacted Summary"
	case "skills":
		if name := extractBetween(content, "<skill name=\"", "\""); name != "" {
			return "Skill: " + name
		}
		return "Skill"
	case "files-read":
		if p := extractAfterPrefixLine(content, "Path: "); p != "" {
			return "read · " + p
		}
		return "read"
	case "files-written":
		if p := extractAfterPrefixLine(content, "Path: "); p != "" {
			return "write · " + p
		}
		return "write"
	case "tool-bash":
		if cmd := extractAfterPrefixLine(content, "Command: "); cmd != "" {
			return "bash · " + cmd
		}
		return "bash"
	case "tool-output":
		tool := strings.TrimSpace(msg.Name)
		if tool == "" {
			tool = "tool"
		}
		return tool
	case "system-prompt":
		return "System Prompt"
	default:
		return "Context"
	}
}

func extractAfterPrefixLine(text, prefix string) string {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(line, prefix))
		}
	}
	return ""
}

func extractBetween(text, start, end string) string {
	i := strings.Index(text, start)
	if i < 0 {
		return ""
	}
	s := text[i+len(start):]
	j := strings.Index(s, end)
	if j < 0 {
		return ""
	}
	return strings.TrimSpace(s[:j])
}

func (m *CodeAgentModel) applyContextEntryEdit(entry ContextEntry, content string) {
	if entry.Role == "system" && entry.Category == "system-prompt" {
		if m.contextManager != nil {
			m.contextManager.SetSessionSystemPromptOverride(content)
			m.recordContextAction("System prompt override updated (session-local)")
		}
		return
	}
	if entry.HistoryIndex < 0 {
		return
	}
	if m.sessionStore != nil && !m.contextOverrideActive {
		if err := m.sessionStore.UpdateContextMessageAt(entry.HistoryIndex, content); err == nil {
			m.conversationHistory = m.sessionStore.ContextMessages()
			m.rebuildTranscriptFromHistory()
			m.recordContextAction(fmt.Sprintf("Context edited: #%d %s", entry.Order, entry.Category))
			return
		}
	}
	if entry.RawHistoryIndex < 0 || entry.RawHistoryIndex >= len(m.conversationHistory) {
		return
	}
	m.conversationHistory[entry.RawHistoryIndex].Content = content
	m.contextOverrideActive = true
	m.rebuildTranscriptFromHistory()
	m.recordContextAction(fmt.Sprintf("Context edited (session-local): #%d %s", entry.Order, entry.Category))
}

func (m *CodeAgentModel) removeContextEntry(entry ContextEntry) {
	if entry.HistoryIndex < 0 {
		return
	}
	if m.sessionStore != nil && !m.contextOverrideActive {
		if err := m.sessionStore.RemoveContextMessageAt(entry.HistoryIndex); err == nil {
			m.conversationHistory = m.sessionStore.ContextMessages()
			m.rebuildTranscriptFromHistory()
			m.recordContextAction(fmt.Sprintf("Context removed: #%d %s", entry.Order, entry.Category))
			return
		}
	}
	if entry.RawHistoryIndex < 0 || entry.RawHistoryIndex >= len(m.conversationHistory) {
		return
	}
	m.conversationHistory = append(m.conversationHistory[:entry.RawHistoryIndex], m.conversationHistory[entry.RawHistoryIndex+1:]...)
	m.contextOverrideActive = true
	m.rebuildTranscriptFromHistory()
	m.recordContextAction(fmt.Sprintf("Context removed (session-local): #%d %s", entry.Order, entry.Category))
}

func (m *CodeAgentModel) contextModalSelectedEntry() *ContextEntry {
	if len(m.contextModalEntries) == 0 {
		return nil
	}
	if m.contextModalSelection < 0 {
		m.contextModalSelection = 0
	}
	if m.contextModalSelection >= len(m.contextModalEntries) {
		m.contextModalSelection = len(m.contextModalEntries) - 1
	}
	return &m.contextModalEntries[m.contextModalSelection]
}

func (m *CodeAgentModel) contextModalMaxPreviewOffset() int {
	selected := m.contextModalSelectedEntry()
	if selected == nil {
		return 0
	}
	width := m.width
	height := m.height
	leftW := max((width-10)*45/100, 30)
	rightW := max((width-10)-leftW, 30)
	innerH := max(height-8, 8)
	fixedLines := 11
	contentViewportH := max(innerH-fixedLines, 1)
	contentLines := wordWrap(strings.TrimSpace(selected.Content), max(rightW-4, 20))
	if len(contentLines) <= contentViewportH {
		return 0
	}
	return len(contentLines) - contentViewportH
}
