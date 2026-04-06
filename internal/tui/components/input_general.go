package components

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/charmbracelet/x/ansi"
	"github.com/ashiqrniloy/synapta-cli/internal/core"
	"github.com/ashiqrniloy/synapta-cli/internal/llm"
)

func (m *CodeAgentModel) handleGeneralKeyPress(msg tea.KeyPressMsg, keyStr, quitKey string) (bool, tea.Cmd) {
	contextKey := m.getContextKey()
	helpKey := m.getHelpKey()
	extensionsKey := m.extensionKeybinding()

	if keyStr == helpKey {
		m.keybindingsModalOpen = true
		m.keybindingsSearch = ""
		m.keybindingsSelection = 0
		return true, nil
	}
	if keyStr == contextKey {
		m.openContextModal()
		return true, nil
	}
	if keyStr == quitKey {
		m.quit = true
		return true, tea.Sequence(tea.Raw(ansi.ResetModeMouseButtonEvent+ansi.ResetModeMouseAnyEvent+ansi.ResetModeMouseExtSgr), tea.Quit)
	}
	if keyStr == "esc" && m.inputMode == inputModeBash && strings.TrimSpace(m.ta.Value()) == "" {
		m.applyInputMode(inputModeChat)
		m.appendSystemMessage("[Bash] Mode disabled", "info")
		return true, nil
	}
	if keyStr == "ctrl+shift+c" || keyStr == "ctrl+y" {
		if err := m.copyLatestMessageToClipboard(); err != nil {
			m.appendSystemMessage("[Clipboard] ✗ "+err.Error(), "error")
		} else {
			m.appendSystemMessage("[Clipboard] ✓ Copied latest transcript block", "done")
		}
		return true, nil
	}
	if keyStr == "ctrl+left" || keyStr == "alt+up" {
		if m.stepSelectedTool(-1) {
			return true, nil
		}
	}
	if keyStr == "ctrl+right" || keyStr == "alt+down" {
		if m.stepSelectedTool(1) {
			return true, nil
		}
	}
	if keyStr == "ctrl+o" {
		if id, ok := m.selectedOrLastToolCallID(); ok {
			m.toolExpanded[id] = !m.toolExpanded[id]
			m.refreshChatViewport()
		}
		return true, nil
	}
	if keyStr == "ctrl+t" {
		next, ok := nextThinkingLevel(m.selectedThinkingLevel, thinkingLevelsForModel(m.selectedProvider, m.selectedModelID, m.selectedModelName))
		if !ok {
			m.appendSystemMessage("[Model] Thinking level control not available for this model", "info")
			return true, nil
		}
		m.selectedThinkingLevel = next
		m.appendSystemMessage("[Model] Thinking level: "+next, "done")
		return true, nil
	}
	if keyStr == "pgup" || keyStr == "pageup" || keyStr == "ctrl+up" || keyStr == "pgdown" || keyStr == "pagedown" || keyStr == "ctrl+down" {
		var cmd tea.Cmd
		m.chatViewport, cmd = m.chatViewport.Update(msg)
		m.chatAutoScroll = m.chatViewport.AtBottom()
		return true, cmd
	}
	if keyStr == m.getCommandKey() {
		m.enterCommandMode()
		return true, nil
	}
	if keyStr == extensionsKey {
		m.enterCommandMode()
		m.ta.SetValue(":extension")
		m.picker.Filter("extension")
		m.recalculateLayout()
		return true, nil
	}
	if keyStr == ":" && m.ta.Value() == "" {
		m.enterCommandMode()
		return true, nil
	}
	if keyStr == "@" && m.inputMode == inputModeChat && m.skillPicker != nil && len(m.availableSkills) > 0 {
		var cmd tea.Cmd
		m.ta, cmd = m.ta.Update(msg)
		m.activateSkillPicker()
		m.updateSkillPickerFromInput()
		m.recalculateLayout()
		return true, cmd
	}
	if keyStr == m.getSubmitKey() {
		return m.handleSubmitKeyPress()
	}
	if msg.Code == tea.KeyEnter && msg.Mod.Contains(tea.ModShift) {
		m.ta.InsertRune('\n')
		m.recalculateLayout()
		return true, nil
	}
	if keyStr == "ctrl+m" || (msg.Code == 'm' && msg.Mod.Contains(tea.ModCtrl)) {
		return true, nil
	}
	return false, nil
}

func (m *CodeAgentModel) handleSubmitKeyPress() (bool, tea.Cmd) {
	if m.isWorking || m.isExecutingBash {
		return true, nil
	}
	text := strings.TrimSpace(m.ta.Value())
	if text == "" || strings.HasPrefix(text, ":") {
		return true, nil
	}

	expandedText := text
	usedSkills := []core.Skill{}
	if m.inputMode == inputModeChat {
		var err error
		expandedText, usedSkills, err = core.ExpandSkillReferencesWithCache(text, m.availableSkills, m.skillCatalogCache)
		if err != nil {
			m.appendSystemMessage("[Skills] ✗ "+err.Error(), "error")
			return true, nil
		}
	}

	if m.inputMode == inputModeBash {
		started := time.Now()
		m.appendChatMessage(ChatMessage{Role: "user", Content: "$ " + text})
		m.ta.SetValue("")
		m.isExecutingBash = true
		m.workingFrame = 0
		m.setWorkingSystemMessage(m.bashWorkingStatusText())
		m.refreshChatViewport()
		return true, tea.Batch(m.executeBashCommand(text, started), workingTickCmd())
	}

	if m.selectedProvider == "" || m.selectedModelID == "" {
		m.appendSystemMessage("Select a model first via :set-model", "error")
		return true, nil
	}

	m.appendChatMessage(ChatMessage{Role: "user", Content: text})
	if len(usedSkills) > 0 {
		names := make([]string, 0, len(usedSkills))
		for _, skill := range usedSkills {
			names = append(names, "@"+skill.Name)
		}
		m.appendSystemMessage("[Skills] Loaded "+strings.Join(names, ", "), "info")
		m.recordContextAction("Skills loaded: " + strings.Join(names, ", "))
	}
	m.conversationHistory = append(m.conversationHistory, llm.Message{Role: "user", Content: expandedText})
	if m.sessionStore != nil {
		_ = m.sessionStore.AppendMessage(llm.Message{Role: "user", Content: expandedText})
	}
	history, err := m.chatHistoryAsLLM()
	if err != nil {
		m.appendSystemMessage("✗ Failed to build context: "+err.Error(), "error")
		return true, nil
	}

	m.activeAssistantIdx = -1
	m.isWorking = true
	m.chatAutoScroll = true
	m.activeToolIndices = map[string]int{}
	m.streamStartedAt = time.Now()
	m.firstChunkAt = time.Time{}
	m.streamChunkCount = 0
	m.streamCharCount = 0
	m.ta.SetValue("")
	m.currentAssistantText.Reset()
	m.workingFrame = 0
	m.setWorkingSystemMessage(m.chatWorkingStatusText())
	m.recalculateLayout()
	m.refreshChatViewport()

	cmd := m.startChatStream(history)
	return true, tea.Batch(cmd, workingTickCmd())
}
