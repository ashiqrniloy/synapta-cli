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

	filteredHistoryRawIdx := make([]int, 0, len(m.conversationHistory))
	filteredHistoryMsgs := make([]llm.Message, 0, len(m.conversationHistory))
	for i, msg := range m.conversationHistory {
		if !isContextRoleLocal(msg.Role) || !hasContextPayloadLocal(msg) {
			continue
		}
		filteredHistoryRawIdx = append(filteredHistoryRawIdx, i)
		filteredHistoryMsgs = append(filteredHistoryMsgs, msg)
	}

	timestamps := []time.Time{}
	if m.sessionStore != nil {
		timestamps = m.sessionStore.ContextMessageTimestamps()
	}
	toolMetaByCallID := buildToolInvocationMetaByCallID(m.conversationHistory)

	entries := make([]ContextEntry, 0, len(msgs))
	order := 0
	hPos := 0
	for i, msg := range msgs {
		category := categorizeContextMessage(msg)
		content := strings.TrimSpace(msg.Content)
		if content == "" && msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
			content = assistantToolCallsContent(msg.ToolCalls)
		}
		if content == "" && msg.Role == "tool" {
			content = "(no output)"
		}
		if content == "" && msg.Role != "system" {
			continue
		}

		entry := ContextEntry{
			Order:           order + 1,
			ContextIndex:    i,
			Role:            msg.Role,
			Content:         content,
			HistoryIndex:    -1,
			RawHistoryIndex: -1,
			Category:        category,
			Timestamp:       time.Time{},
			EstimatedTokens: estimateTextTokens(content),
		}

		isHistoryMessage := hPos < len(filteredHistoryMsgs) && contextMessageEquals(msg, filteredHistoryMsgs[hPos])
		if isHistoryMessage {
			entry.HistoryIndex = hPos
			entry.RawHistoryIndex = filteredHistoryRawIdx[hPos]
			entry.Editable = msg.Role != "system"
			entry.Removable = msg.Role != "system"
			if hPos < len(timestamps) {
				entry.Timestamp = timestamps[hPos]
			}
			hPos++
		}
		if msg.Role == "system" && category == "system-prompt" {
			entry.Editable = true
			entry.Removable = false
		}

		entry.Label = m.contextEntryLabel(msg, category, entry.Timestamp, toolMetaByCallID)
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

func (m *CodeAgentModel) contextEntryLabel(msg llm.Message, category string, ts time.Time, toolMetaByCallID map[string]toolInvocationMeta) string {
	content := strings.TrimSpace(msg.Content)
	stamp := ""
	if ts.IsZero() {
		ts = time.Now()
	}
	stamp = ts.Local().Format("15:04:05")
	toolMeta := toolMetaByCallID[strings.TrimSpace(msg.ToolCallID)]

	switch category {
	case "user-input":
		return "User Input · " + stamp
	case "llm-output":
		if len(msg.ToolCalls) > 0 && strings.TrimSpace(msg.Content) == "" {
			return "Assistant Tool Calls · " + stamp
		}
		return "LLM Output · " + stamp
	case "compacted-output":
		return "Compacted Summary"
	case "skills":
		if name := extractBetween(content, "<skill name=\"", "\""); name != "" {
			return "Skill: " + name
		}
		return "Skill"
	case "files-read":
		if p := strings.TrimSpace(toolMeta.Path); p != "" {
			return "read · " + p
		}
		if p := extractAfterPrefixLine(content, "Path: "); p != "" {
			return "read · " + p
		}
		return "read"
	case "files-written":
		if p := strings.TrimSpace(toolMeta.Path); p != "" {
			return "write · " + p
		}
		if p := extractAfterPrefixLine(content, "Path: "); p != "" {
			return "write · " + p
		}
		return "write"
	case "tool-bash":
		if cmd := strings.TrimSpace(toolMeta.Command); cmd != "" {
			return "bash · " + cmd
		}
		if cmd := extractAfterPrefixLine(content, "Command: "); cmd != "" {
			return "bash · " + cmd
		}
		return "bash"
	case "tool-output":
		tool := strings.TrimSpace(msg.Name)
		if tool == "" {
			tool = strings.TrimSpace(toolMeta.Name)
		}
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

func assistantToolCallsContent(calls []llm.ToolCall) string {
	if len(calls) == 0 {
		return ""
	}
	lines := make([]string, 0, len(calls))
	for _, tc := range calls {
		name := strings.TrimSpace(tc.Function.Name)
		if name == "" {
			name = "tool"
		}
		args := strings.TrimSpace(tc.Function.Arguments)
		if args == "" {
			lines = append(lines, fmt.Sprintf("tool call: %s", name))
			continue
		}
		lines = append(lines, fmt.Sprintf("tool call: %s\nargs: %s", name, args))
	}
	return strings.Join(lines, "\n\n")
}

func hasContextPayloadLocal(msg llm.Message) bool {
	hasContent := strings.TrimSpace(msg.Content) != ""
	switch msg.Role {
	case "assistant":
		return hasContent || len(msg.ToolCalls) > 0
	case "tool":
		return hasContent || strings.TrimSpace(msg.ToolCallID) != "" || strings.TrimSpace(msg.Name) != ""
	default:
		return hasContent
	}
}

func isContextRoleLocal(role string) bool {
	switch role {
	case "user", "assistant", "tool", "system":
		return true
	default:
		return false
	}
}

func contextMessageEquals(a, b llm.Message) bool {
	if a.Role != b.Role || strings.TrimSpace(a.Content) != strings.TrimSpace(b.Content) || strings.TrimSpace(a.ToolCallID) != strings.TrimSpace(b.ToolCallID) || strings.TrimSpace(a.Name) != strings.TrimSpace(b.Name) {
		return false
	}
	if len(a.ToolCalls) != len(b.ToolCalls) {
		return false
	}
	for i := range a.ToolCalls {
		at, bt := a.ToolCalls[i], b.ToolCalls[i]
		if strings.TrimSpace(at.ID) != strings.TrimSpace(bt.ID) || strings.TrimSpace(at.Type) != strings.TrimSpace(bt.Type) || strings.TrimSpace(at.Function.Name) != strings.TrimSpace(bt.Function.Name) || strings.TrimSpace(at.Function.Arguments) != strings.TrimSpace(bt.Function.Arguments) {
			return false
		}
	}
	return true
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
	leftW := max((width-10)*25/100, 30)
	rightW := max((width-10)-leftW, 30)
	innerH := max(height-8, 8)
	fixedLines := 11
	contentViewportH := max(innerH-fixedLines, 1)
	contentLines := wrapMultiline(strings.TrimSpace(selected.Content), max(rightW-4, 20))
	if len(contentLines) <= contentViewportH {
		return 0
	}
	return len(contentLines) - contentViewportH
}
