package components

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"charm.land/bubbles/v2/textarea"
)

func (m *CodeAgentModel) Init() tea.Cmd {
	switch m.selectedProvider {
	case "kilo", "github-copilot":
		return tea.Batch(textarea.Blink, m.fetchProviderBalanceCmd(m.selectedProvider))
	default:
		return textarea.Blink
	}
}

func (m *CodeAgentModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.recalculateLayout()
		m.refreshChatViewport()
		return m, nil
	case CommandActionMsg:
		return m.handleCommandAction(msg)
	case ModelsLoadedMsg:
		return m.handleModelsLoaded(msg)
	case ModelsLoadErrMsg:
		return m.handleModelsLoadErr(msg)
	case SessionsLoadedMsg:
		return m.handleSessionsLoaded(msg)
	case SessionsLoadErrMsg:
		return m.handleSessionsLoadErr(msg)
	case KiloAuthProgressMsg:
		return m.handleKiloAuthProgress(msg)
	case providerBalanceMsg:
		return m.handleProviderBalance(msg)
	case CopilotAuthProgressMsg:
		return m.handleCopilotAuthProgress(msg)
	case CopilotAuthPromptMsg:
		return m.handleCopilotAuthPrompt(msg)
	case KiloAuthCompleteMsg:
		return m.handleKiloAuthComplete(msg)
	case CopilotAuthCompleteMsg:
		return m.handleCopilotAuthComplete(msg)
	case authFlowDoneMsg:
		return m.handleAuthFlowDone()
	case extensionProcessDoneMsg:
		return m.handleExtensionProcessDone(msg)
	case chatStreamChunkMsg:
		return m.handleChatStreamChunk(msg)
	case assistantToolCallsMsg:
		return m.handleAssistantToolCalls(msg)
	case toolEventMsg:
		return m.handleToolEvent(msg)
	case toolTickMsg:
		return m.handleToolTick()
	case chatStreamDoneMsg:
		return m.handleChatStreamDone()
	case chatStreamCancelledMsg:
		return m.handleChatStreamCancelled()
	case chatStreamErrMsg:
		return m.handleChatStreamErr(msg)
	case compactDoneMsg:
		return m.handleCompactDone(msg)
	case newSessionDoneMsg:
		return m.handleNewSessionDone(msg)
	case resumeSessionDoneMsg:
		return m.handleResumeSessionDone(msg)
	case bashCommandDoneMsg:
		return m.handleBashCommandDone(msg)
	case workingTickMsg:
		return m.handleWorkingTick()
	case FileAddedMsg:
		return m.handleFileAdded(msg)
	case tea.MouseWheelMsg:
		return m.handleMouseWheel(msg)
	case tea.KeyPressMsg:
		return m.handleKeyPress(msg)
	}

	var cmd tea.Cmd
	m.ta, cmd = m.ta.Update(msg)
	m.recalculateLayout()
	return m, cmd
}

func (m *CodeAgentModel) View() tea.View {
	if m.quit {
		return tea.NewView("")
	}
	if m.height < 1 || m.width < 1 {
		v := tea.NewView("")
		v.AltScreen = true
		v.MouseMode = tea.MouseModeCellMotion
		v.KeyboardEnhancements.ReportEventTypes = true
		return v
	}

	header := strings.TrimRight(m.renderHeaderBar(), "\n")
	inputBox := strings.TrimRight(m.renderInputBox(), "\n")
	modelFooter := strings.TrimRight(m.renderModelFooter(), "\n")

	chatView := lipgloss.NewStyle().
		Width(m.chatPaneWidth).
		Height(m.chatViewport.Height()).
		AlignVertical(lipgloss.Top).
		Render(m.chatViewport.View())

	divider := lipgloss.NewStyle().Foreground(m.styles.MutedStyle.GetForeground()).Render(strings.Repeat("─", max(m.width, 1)))
	sections := []string{header, divider, chatView, inputBox}
	if modelFooter != "" {
		sections = append(sections, modelFooter)
	}
	base := strings.Join(sections, "\n")
	if m.contextModalOpen {
		base = m.renderContextModal()
	}
	if m.keybindingsModalOpen {
		base = m.renderKeybindingsModal()
	}
	if m.fileBrowserModalOpen {
		base = m.renderFileBrowserModal()
	}
	if m.commandModalOpen {
		base = m.renderCommandModal(base)
	}

	v := tea.NewView(base)
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	v.KeyboardEnhancements.ReportEventTypes = true
	return v
}
