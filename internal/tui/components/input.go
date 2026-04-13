package components

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/ashiqrniloy/synapta-cli/internal/core"
)

func normalizeKeyName(key string) string {
	lower := strings.ToLower(key)
	replacer := strings.NewReplacer("escape", "esc")
	return replacer.Replace(lower)
}

func (m *CodeAgentModel) getSubmitKey() string {
	if m.cfg != nil && m.cfg.Keybindings.Submit != "" {
		return normalizeKeyName(m.cfg.Keybindings.Submit)
	}
	return "enter"
}

func (m *CodeAgentModel) getQuitKey() string {
	if m.cfg != nil && m.cfg.Keybindings.Quit != "" {
		return normalizeKeyName(m.cfg.Keybindings.Quit)
	}
	return "ctrl+c"
}

func (m *CodeAgentModel) getStopKey() string {
	if m.cfg != nil && m.cfg.Keybindings.Stop != "" {
		return normalizeKeyName(m.cfg.Keybindings.Stop)
	}
	return "ctrl+q"
}


func (m *CodeAgentModel) getContextKey() string {
	if m.cfg != nil && m.cfg.Keybindings.Context != "" {
		return normalizeKeyName(m.cfg.Keybindings.Context)
	}
	return "ctrl+k"
}

func (m *CodeAgentModel) getFileBrowserKey() string {
	if m.cfg != nil && m.cfg.Keybindings.FileBrowser != "" {
		return normalizeKeyName(m.cfg.Keybindings.FileBrowser)
	}
	return "ctrl+f"
}

func (m *CodeAgentModel) getCommandKey() string {
	if m.cfg != nil && m.cfg.Keybindings.Command != "" {
		return normalizeKeyName(m.cfg.Keybindings.Command)
	}
	return "ctrl+p"
}

func (m *CodeAgentModel) getHelpKey() string {
	if m.cfg != nil && m.cfg.Keybindings.Help != "" {
		return normalizeKeyName(m.cfg.Keybindings.Help)
	}
	return "ctrl+j"
}

func (m *CodeAgentModel) shouldInsertNewline(msg tea.KeyPressMsg, keyStr string) bool {
	if keyStr == "ctrl+n" || (msg.Code == 'n' && msg.Mod.Contains(tea.ModCtrl)) {
		return true
	}
	return msg.Code == tea.KeyEnter && msg.Mod.Contains(tea.ModShift)
}

func (m *CodeAgentModel) getFilterText() string {
	return getFilterTextFromValue(m.commandInputValue())
}

func getFilterTextFromValue(value string) string {
	if strings.HasPrefix(value, ":") {
		return value[1:]
	}
	return ""
}

func normalizeCommandShortcut(value string) string {
	v := strings.ToLower(strings.TrimSpace(value))
	v = strings.TrimPrefix(v, ":")
	if strings.ContainsAny(v, " \t\n") {
		return ""
	}
	return strings.TrimSpace(v)
}

func (m *CodeAgentModel) commandShortcutCommandID() string {
	shortcut := normalizeCommandShortcut(m.commandInputValue())
	if shortcut == "" {
		return ""
	}
	if m.cfg != nil && len(m.cfg.CommandShortcuts) > 0 {
		if id, ok := m.cfg.CommandShortcuts[shortcut]; ok {
			return strings.TrimSpace(id)
		}
	}
	switch shortcut {
	case "q":
		return "quit"
	case "b":
		return "bash"
	case "h":
		return "help"
	case "k":
		return "context-manager"
	case "m":
		return "set-model"
	case "n":
		return "new-session"
	case "c":
		return "compact"
	case "r":
		return "resume-session"
	default:
		return ""
	}
}

func (m *CodeAgentModel) commandInputValue() string {
	if m.commandModalOpen {
		return m.commandModalInput.Value()
	}
	return m.ta.Value()
}

func (m *CodeAgentModel) setCommandInputValue(value string) {
	if m.commandModalOpen {
		m.commandModalInput.SetValue(value)
		return
	}
	m.ta.SetValue(value)
}

func (m *CodeAgentModel) updateCommandInput(msg tea.KeyPressMsg) tea.Cmd {
	if m.commandModalOpen {
		var cmd tea.Cmd
		m.commandModalInput, cmd = m.commandModalInput.Update(msg)
		return cmd
	}
	var cmd tea.Cmd
	m.ta, cmd = m.ta.Update(msg)
	return cmd
}

func (m *CodeAgentModel) clearCommandMode() {
	if m.commandModalOpen {
		m.commandModalOpen = false
		m.commandModalInput.SetValue("")
		m.commandModalInput.Blur()
		m.ta.Focus()
	} else {
		m.ta.SetValue("")
	}
	m.picker.Deactivate()
	if m.skillPicker != nil {
		m.skillPicker.Deactivate()
	}
	m.applyInputMode(m.inputMode)
	m.recalculateLayout()
}

func (m *CodeAgentModel) enterCommandMode() {
	if m.skillPicker != nil {
		m.skillPicker.Deactivate()
	}
	m.picker.Activate()
	m.ta.SetValue(":")
	m.picker.Filter(getFilterTextFromValue(m.ta.Value()))
	m.ta.Placeholder = "Command mode… type to filter"
	m.recalculateLayout()
}

func (m *CodeAgentModel) openCommandModal() {
	if m.skillPicker != nil {
		m.skillPicker.Deactivate()
	}
	m.commandModalOpen = true
	m.picker.Activate()
	m.commandModalInput.SetValue(":")
	m.commandModalInput.Placeholder = "Command mode… type to filter"
	m.commandModalInput.Focus()
	m.ta.Blur()
	m.picker.Filter(getFilterTextFromValue(m.commandModalInput.Value()))
	m.recalculateLayout()
}

func (m *CodeAgentModel) applyInputMode(mode string) {
	m.inputMode = mode
	switch mode {
	case inputModeBash:
		if m.skillPicker != nil {
			m.skillPicker.Deactivate()
		}
		m.ta.Placeholder = "bash> Enter command (Enter=run, Esc=exit bash mode)"
	default:
		m.inputMode = inputModeChat
		m.ta.Placeholder = "Type your message... (Enter=send, Shift+Enter/Ctrl+N=newline)"
	}
}

func (m *CodeAgentModel) reloadAvailableSkills() {
	opts := core.LoadSkillsOptions{CWD: m.currentCwd, AgentDir: m.agentDir, IncludeDefaults: true}
	if m.skillCatalogCache != nil {
		m.availableSkills = m.skillCatalogCache.Load(opts).Skills
	} else {
		m.availableSkills = core.LoadSkills(opts).Skills
	}
	if m.contextManager != nil {
		m.contextManager.InvalidateSkills()
	}
}

func (m *CodeAgentModel) activateSkillPicker() {
	if m.skillPicker == nil || m.inputMode != inputModeChat || len(m.availableSkills) == 0 {
		return
	}
	m.skillPicker.Activate(m.availableSkills)
}

func (m *CodeAgentModel) updateSkillPickerFromInput() {
	if m.skillPicker == nil || !m.skillPicker.IsActive() {
		return
	}
	query, ok := activeSkillMentionQuery(m.ta.Value())
	if !ok {
		m.skillPicker.Deactivate()
		return
	}
	m.skillPicker.Filter(query)
}

func activeSkillMentionQuery(text string) (string, bool) {
	idx := strings.LastIndex(text, "@")
	if idx < 0 || idx >= len(text)-1 {
		if idx >= 0 && idx == len(text)-1 {
			return "", true
		}
		return "", false
	}
	if idx > 0 {
		prev := text[idx-1]
		if prev != ' ' && prev != '\n' && prev != '\t' {
			return "", false
		}
	}
	query := text[idx+1:]
	if strings.ContainsAny(query, " \n\t") {
		return "", false
	}
	return query, true
}

func replaceActiveSkillMention(text, skillName string) string {
	idx := strings.LastIndex(text, "@")
	if idx < 0 {
		return text
	}
	return text[:idx] + "@" + skillName + " "
}

func (m *CodeAgentModel) recordContextAction(message string) {
	m.contextActions = append(m.contextActions, ContextAction{At: time.Now(), Message: message})
	if len(m.contextActions) > 200 {
		m.contextActions = m.contextActions[len(m.contextActions)-200:]
	}
}

func (m *CodeAgentModel) handleMouseWheel(msg tea.MouseWheelMsg) (tea.Model, tea.Cmd) {
	if m.picker.IsActive() {
		return m, nil
	}
	if m.fileBrowserModalOpen {
		if msg.Button == tea.MouseWheelUp {
			if m.fileBrowserModalOffset > 0 {
				m.fileBrowserModalOffset--
			}
			return m, nil
		}
		if msg.Button == tea.MouseWheelDown {
			m.fileBrowserModalOffset++
			return m, nil
		}
	}
	if m.contextModalOpen && !m.contextModalEditMode {
		if msg.Button == tea.MouseWheelUp {
			if m.contextModalPreviewOffset > 0 {
				m.contextModalPreviewOffset--
			}
			return m, nil
		}
		if msg.Button == tea.MouseWheelDown {
			if m.contextModalPreviewOffset < m.contextModalMaxPreviewOffset() {
				m.contextModalPreviewOffset++
			}
			return m, nil
		}
	}
	var cmd tea.Cmd
	m.chatViewport, cmd = m.chatViewport.Update(msg)
	m.chatAutoScroll = m.chatViewport.AtBottom()
	return m, cmd
}

func (m *CodeAgentModel) handleKeyPress(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	keyStr := msg.String()
	quitKey := m.getQuitKey()
	helpKey := m.getHelpKey()
	stopKey := m.getStopKey()

	// Handle stop key - only works when agentic task is running
	if keyStr == stopKey && m.isWorking {
		m.stopRequested = true
		m.appendSystemMessage("[Agent] Stopping after current operation completes...", "working")
		return m, nil
	}

	if m.shouldInsertNewline(msg, keyStr) {
		if (m.inputMode == inputModeChat || m.inputMode == inputModeBash) && !m.picker.IsActive() && (m.skillPicker == nil || !m.skillPicker.IsActive()) {
			m.ta.InsertRune('\n')
			m.recalculateLayout()
			return m, nil
		}
	}

	if handled, cmd := m.handleKeybindingsModalKeyPress(msg, keyStr, helpKey); handled {
		return m, cmd
	}
	if handled, cmd := m.handleContextModalKeyPress(msg, keyStr); handled {
		return m, cmd
	}
	if handled, cmd := m.handleFileBrowserModalKeyPress(msg, keyStr); handled {
		return m, cmd
	}
	if handled, cmd := m.handleCommandPickerKeyPress(msg, keyStr, quitKey); handled {
		return m, cmd
	}
	if handled, cmd := m.handleSkillPickerKeyPress(msg, keyStr); handled {
		return m, cmd
	}
	if handled, cmd := m.handleSessionSearchKeyPress(msg, keyStr); handled {
		return m, cmd
	}
	if handled, cmd := m.handleFileBrowserKeyPress(msg, keyStr); handled {
		return m, cmd
	}
	if handled, cmd := m.handleGeneralKeyPress(msg, keyStr, quitKey); handled {
		return m, cmd
	}

	if m.commandModalOpen {
		cmd := m.updateCommandInput(msg)
		m.picker.Filter(m.getFilterText())
		m.recalculateLayout()
		return m, cmd
	}

	var cmd tea.Cmd
	m.ta, cmd = m.ta.Update(msg)
	m.recalculateLayout()
	return m, cmd
}
