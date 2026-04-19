package components

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/ashiqrniloy/synapta-cli/internal/core"
	"github.com/ashiqrniloy/synapta-cli/internal/llm"
	"github.com/charmbracelet/x/ansi"
)

var attachmentTokenRE = regexp.MustCompile("`([^`\\n]+)`")

func (m *CodeAgentModel) shutdownLifecycle() {
	if m.cancelStream != nil {
		m.cancelStream()
		m.cancelStream = nil
	}
	if m.cancelLifecycle != nil {
		m.cancelLifecycle()
		m.cancelLifecycle = nil
	}
	if m.sessionStore != nil {
		_ = m.sessionStore.Close()
	}
}
func (m *CodeAgentModel) handleGeneralKeyPress(msg tea.KeyPressMsg, keyStr, quitKey string) (bool, tea.Cmd) {
	contextKey := m.getContextKey()
	fileBrowserKey := m.getFileBrowserKey()
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
	if keyStr == fileBrowserKey {
		m.openFileBrowserModal()
		return true, nil
	}
	if keyStr == quitKey {
		m.quit = true
		m.shutdownLifecycle()
		return true, tea.Sequence(tea.Raw(ansi.ResetModeMouseButtonEvent+ansi.ResetModeMouseAnyEvent+ansi.ResetModeMouseExtSgr), tea.Quit)
	}

	if keyStr == "esc" && m.inputMode == inputModeBash && strings.TrimSpace(m.ta.Value()) == "" {
		m.applyInputMode(inputModeChat)
		m.appendSystemMessage("[Shell] Mode disabled", "info")
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
			m.invalidateTranscriptForToolCallID(id)
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
		m.openCommandModal()
		return true, nil
	}
	if keyStr == extensionsKey {
		if m.commandModalOpen {
			m.setCommandInputValue(":extension")
		} else {
			m.enterCommandMode()
			m.ta.SetValue(":extension")
		}
		m.picker.Filter("extension")
		m.recalculateLayout()
		return true, nil
	}
	if keyStr == ":" && m.ta.Value() == "" {
		m.enterCommandMode()
		return true, nil
	}
	// Activate file browser when "/" is typed at the start
	if keyStr == "/" && m.inputMode == inputModeChat && m.fileBrowser != nil && m.ta.Value() == "" {
		var cmd tea.Cmd
		m.ta, cmd = m.ta.Update(msg)
		m.fileBrowser.Activate(m.currentCwd)
		m.recalculateLayout()
		return true, cmd
	}
	if keyStr == "@" && m.inputMode == inputModeChat && m.skillPicker != nil && len(m.availableSkills) > 0 {
		var cmd tea.Cmd
		m.ta, cmd = m.ta.Update(msg)
		m.activateSkillPicker()
		m.updateSkillPickerFromInput()
		m.recalculateLayout()
		return true, cmd
	}
	// Activate session search when ">" is typed at the start
	if keyStr == ">" && m.inputMode == inputModeChat && m.sessionSearch != nil && m.ta.Value() == "" {
		var cmd tea.Cmd
		m.ta, cmd = m.ta.Update(msg)
		m.activateSessionSearch()
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
	// Steering: if the LLM is working, capture the message and immediately
	// cancel the current stream.  The cancelled stream handler will inject
	// the steering message and restart the request right away.
	if m.isWorking {
		text := strings.TrimSpace(m.ta.Value())
		if text != "" && !strings.HasPrefix(text, ":") {
			// Replace (don't append) any previously queued steer message so
			// the most recent instruction is always what gets injected.
			m.pendingUserMessage = text
			m.ta.SetValue("")
			m.appendSystemMessage("[Steer] Message received — interrupting current task to steer…", "info")
			// Cancel the running stream; handleChatStreamCancelled will pick
			// up pendingUserMessage and restart with it in context.
			if m.cancelStream != nil {
				m.cancelStream()
				m.cancelStream = nil
			}
			return true, nil
		}
		return true, nil
	}
	if m.isExecutingBash {
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

	if m.inputMode == inputModeChat {
		withAttachments, attachedPaths, err := m.expandAttachmentTokens(expandedText)
		if err != nil {
			m.appendSystemMessage("[Files] ✗ "+err.Error(), "error")
			return true, nil
		}
		expandedText = withAttachments
		if len(attachedPaths) > 0 {
			m.recordContextAction("File attachments added: " + strings.Join(attachedPaths, ", "))
		}
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
	m.markContextEntriesDirty()
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

func (m *CodeAgentModel) resolveAttachmentPath(raw string) (string, bool) {
	candidate := strings.TrimSpace(raw)
	if candidate == "" {
		return "", false
	}
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(m.currentCwd, candidate)
	}
	candidate = filepath.Clean(candidate)
	info, err := os.Stat(candidate)
	if err != nil || info.IsDir() {
		return "", false
	}
	return candidate, true
}

func (m *CodeAgentModel) extractAttachmentTokenPaths(text string) []string {
	matches := attachmentTokenRE.FindAllStringSubmatch(text, -1)
	if len(matches) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	paths := make([]string, 0, len(matches))
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		if path, ok := m.resolveAttachmentPath(match[1]); ok {
			if _, exists := seen[path]; exists {
				continue
			}
			seen[path] = struct{}{}
			paths = append(paths, path)
		}
	}
	return paths
}

func (m *CodeAgentModel) expandAttachmentTokens(text string) (string, []string, error) {
	paths := m.extractAttachmentTokenPaths(text)
	if len(paths) == 0 {
		return text, nil, nil
	}

	var b strings.Builder
	b.WriteString(text)
	for _, path := range paths {
		content, err := m.readFileForContext(path)
		if err != nil {
			return "", nil, fmt.Errorf("failed to attach %s: %w", path, err)
		}
		b.WriteString("\n\n<attached_file path=\"")
		b.WriteString(path)
		b.WriteString("\">\n")
		b.WriteString(content)
		b.WriteString("\n</attached_file>")
	}

	return b.String(), paths, nil
}
