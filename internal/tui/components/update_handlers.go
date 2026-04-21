package components

import (
	"fmt"
	"os"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/ashiqrniloy/synapta-cli/internal/core"
	"github.com/ashiqrniloy/synapta-cli/internal/core/tools"
	"github.com/ashiqrniloy/synapta-cli/internal/llm"
	"github.com/charmbracelet/x/ansi"
)

func (m *CodeAgentModel) handleCommandAction(msg CommandActionMsg) (tea.Model, tea.Cmd) {
	if len(msg.Path) == 0 {
		m.clearCommandMode()
		return m, nil
	}

	commandID := msg.Path[0].ID
	if extID, ok := parseExtensionCommandID(commandID); ok {
		m.clearCommandMode()
		ext, found := m.extensionByID(extID)
		if !found {
			m.appendSystemMessage("[Extension] Unknown extension: "+extID, "error")
			return m, nil
		}
		m.appendSystemMessage("[Extension] Launching: "+m.extensionLaunchLabel(ext), "working")
		touchExtensionLastLaunched(ext.Source)
		return m, m.launchExtensionCmd(ext)
	}

	switch commandID {
	case "quit":
		m.clearCommandMode()
		m.shutdownLifecycle()
		m.quit = true
		return m, tea.Sequence(tea.Raw(ansi.ResetModeMouseButtonEvent+ansi.ResetModeMouseAnyEvent+ansi.ResetModeMouseExtSgr), tea.Quit)
	case "shell", "bash":
		m.clearCommandMode()
		m.applyInputMode(inputModeBash)
		m.appendSystemMessage("[Shell] Mode enabled. Enter a command and press Enter.", "info")
		return m, nil
	case "help":
		m.clearCommandMode()
		m.keybindingsModalOpen = true
		m.keybindingsSearch = ""
		m.keybindingsSelection = 0
		return m, nil
	case "context-manager":
		m.clearCommandMode()
		m.openContextModal()
		return m, nil
	case "browse-files":
		m.clearCommandMode()
		m.openFileBrowserModal()
		return m, nil
	case "add-provider":
		m.clearCommandMode()
		if len(msg.Path) > 1 {
			providerID := msg.Path[1].ID
			switch providerID {
			case "kilo":
				m.appendSystemMessage("[Kilo] Starting authentication...", "working")
				return m, m.startKiloAuth()
			case "github-copilot":
				m.appendSystemMessage("[GitHub Copilot] Starting device authentication...", "working")
				return m, m.startCopilotAuth()
			default:
				m.appendSystemMessage("[Add Provider] Selected: "+msg.Path[1].Name, "info")
			}
		}
	case "set-model":
		if len(msg.Path) > 1 {
			providerID, modelID, ok := parseModelSelectionKey(msg.Path[1].ID)
			if !ok {
				m.appendSystemMessage("[Model] Invalid selection", "error")
				m.clearCommandMode()
				return m, nil
			}

			m.selectedProvider = providerID
			m.selectedModelID = modelID
			m.selectedModelName = msg.Path[1].Name
			m.selectedThinkingLevel = inferThinkingLevel(modelID, msg.Path[1].Name)
			m.selectedContextWindow = resolveModelContextWindow(providerID, modelID)
			if m.cfg != nil {
				if err := m.cfg.SetProvider(providerID, modelID); err != nil {
					m.appendSystemMessage("[Model] Warning: failed to save config: "+err.Error(), "error")
				}
			}
			m.clearCommandMode()
			m.appendSystemMessage("[Model] Selected: "+msg.Path[1].Name, "done")
			m.providerBalance = ""
			if providerID == "kilo" || providerID == "github-copilot" {
				return m, m.fetchProviderBalanceCmd(providerID)
			}
		} else {
			return m, m.loadModels()
		}
	case "compact":
		m.clearCommandMode()
		m.appendSystemMessage("[Compact] Running manual compaction...", "working")
		return m, m.manualCompactCmd()
	case "new-session":
		m.clearCommandMode()
		m.appendSystemMessage("[Session] Starting new session...", "working")
		return m, m.newSessionCmd()
	case "resume-session":
		if len(msg.Path) > 1 {
			m.clearCommandMode()
			m.appendSystemMessage("[Session] Resuming selected session...", "working")
			return m, m.resumeSessionCmd(msg.Path[1].ID)
		}
		return m, m.loadSessions()
	}

	return m, nil
}

func (m *CodeAgentModel) handleModelsLoaded(msg ModelsLoadedMsg) (tea.Model, tea.Cmd) {
	if len(msg.Models) == 0 {
		m.appendSystemMessage("[Model] No models available. Add a provider first.", "info")
		m.clearCommandMode()
		return m, nil
	}
	m.picker.LoadModels(msg.Models)
	m.setCommandInputValue(":")
	m.recalculateLayout()
	return m, nil
}

func (m *CodeAgentModel) handleModelsLoadErr(msg ModelsLoadErrMsg) (tea.Model, tea.Cmd) {
	m.appendSystemMessage("[Model] ✗ "+msg.Err.Error(), "error")
	m.clearCommandMode()
	return m, nil
}

func (m *CodeAgentModel) handleSessionsLoaded(msg SessionsLoadedMsg) (tea.Model, tea.Cmd) {
	if len(msg.Sessions) == 0 {
		m.appendSystemMessage("[Session] No previous sessions found", "info")
		m.clearCommandMode()
		return m, nil
	}
	items := make([]CommandItem, 0, len(msg.Sessions))
	for _, s := range msg.Sessions {
		preview := strings.ReplaceAll(strings.TrimSpace(s.FirstMessage), "\n", " ")
		if len(preview) > 60 {
			preview = preview[:60] + "..."
		}
		cwdLabel := s.CWD
		if cwdLabel == "" {
			cwdLabel = "(unknown cwd)"
		}
		name := fmt.Sprintf("%s  (%d msgs)  %s  [%s]", s.Modified.Format("2006-01-02 15:04"), s.MessageCount, preview, cwdLabel)
		items = append(items, CommandItem{ID: s.Path, Name: name})
	}
	m.picker.LoadItems(items)
	m.setCommandInputValue(":")
	m.recalculateLayout()
	return m, nil
}

func (m *CodeAgentModel) handleSessionsLoadErr(msg SessionsLoadErrMsg) (tea.Model, tea.Cmd) {
	m.appendSystemMessage("[Session] ✗ "+msg.Err.Error(), "error")
	m.clearCommandMode()
	return m, nil
}

func (m *CodeAgentModel) handleKiloAuthProgress(msg KiloAuthProgressMsg) (tea.Model, tea.Cmd) {
	m.appendSystemMessage("[Kilo] "+string(msg), "info")
	return m, nil
}

func (m *CodeAgentModel) handleProviderBalance(msg providerBalanceMsg) (tea.Model, tea.Cmd) {
	if msg.ProviderID == m.selectedProvider {
		if msg.Err != nil {
			m.providerBalance = ""
		} else {
			m.providerBalance = msg.Balance
		}
	}
	return m, nil
}

func (m *CodeAgentModel) handleCopilotAuthProgress(msg CopilotAuthProgressMsg) (tea.Model, tea.Cmd) {
	m.appendSystemMessage("[GitHub Copilot] "+string(msg), "info")
	return m, waitForAuthMsg(m.authCh)
}

func (m *CodeAgentModel) handleCopilotAuthPrompt(msg CopilotAuthPromptMsg) (tea.Model, tea.Cmd) {
	if strings.TrimSpace(msg.VerificationURL) != "" {
		m.appendSystemMessage("[GitHub Copilot] Open: "+msg.VerificationURL, "info")
	}
	if strings.TrimSpace(msg.UserCode) != "" {
		m.appendSystemMessage("[GitHub Copilot] Device code: "+msg.UserCode, "done")
	}
	m.appendSystemMessage("[GitHub Copilot] Authorize this device in your browser, then return here.", "info")
	return m, waitForAuthMsg(m.authCh)
}

func (m *CodeAgentModel) handleKiloAuthComplete(msg KiloAuthCompleteMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		errLines := strings.Split(msg.Err.Error(), "\n")
		m.appendSystemMessage("[Kilo] ✗ Authentication failed:", "error")
		for _, line := range errLines {
			m.appendSystemMessage("[Kilo]   "+line, "error")
		}
		return m, nil
	}
	m.appendSystemMessage("[Kilo] ✓ Authentication successful! "+msg.Email, "done")
	m.appendSystemMessage("[Kilo] ✓ "+fmt.Sprintf("%d models available", msg.ModelCount), "done")
	if m.chatController != nil {
		m.chatController.InvalidateProviderCache()
	}
	return m, m.fetchProviderBalanceCmd("kilo")
}

func (m *CodeAgentModel) handleCopilotAuthComplete(msg CopilotAuthCompleteMsg) (tea.Model, tea.Cmd) {
	m.authCh = nil
	if msg.Err != nil {
		errLines := strings.Split(msg.Err.Error(), "\n")
		m.appendSystemMessage("[GitHub Copilot] ✗ Authentication failed:", "error")
		for _, line := range errLines {
			m.appendSystemMessage("[GitHub Copilot]   "+line, "error")
		}
		return m, nil
	}
	m.appendSystemMessage("[GitHub Copilot] ✓ Authentication successful!", "done")
	m.appendSystemMessage("[GitHub Copilot] ✓ "+fmt.Sprintf("%d models available", msg.ModelCount), "done")
	if m.chatController != nil {
		m.chatController.InvalidateProviderCache()
	}
	return m, m.fetchProviderBalanceCmd("github-copilot")
}

func (m *CodeAgentModel) handleAuthFlowDone() (tea.Model, tea.Cmd) {
	m.authCh = nil
	return m, nil
}

func (m *CodeAgentModel) handleExtensionProcessDone(msg extensionProcessDoneMsg) (tea.Model, tea.Cmd) {
	m.handleExtensionDone(msg)
	return m, nil
}

func (m *CodeAgentModel) handleChatStreamChunk(msg chatStreamChunkMsg) (tea.Model, tea.Cmd) {
	if m.firstChunkAt.IsZero() {
		m.firstChunkAt = time.Now()
	}
	m.streamChunkCount++
	m.streamCharCount += len(msg.Text)
	m.currentAssistantText.WriteString(msg.Text)
	if m.activeAssistantIdx < 0 || m.activeAssistantIdx >= len(m.chatMessages) {
		m.activeAssistantIdx = m.appendChatMessage(ChatMessage{Role: "assistant", Content: ""})
	}
	m.chatMessages[m.activeAssistantIdx].Content += msg.Text
	m.refreshChatViewportIncremental(m.activeAssistantIdx)
	return m, waitForStreamMsg(m.streamCh)
}

func (m *CodeAgentModel) handleAssistantToolCalls(msg assistantToolCallsMsg) (tea.Model, tea.Cmd) {
	assistantMsg := msg.Message
	if len(assistantMsg.ToolCalls) > 0 {
		assistantMsg.ToolCalls = append([]llm.ToolCall(nil), assistantMsg.ToolCalls...)
	}
	m.conversationHistory = append(m.conversationHistory, assistantMsg)
	m.markContextEntriesDirty()
	if m.sessionStore != nil {
		_ = m.sessionStore.AppendMessage(assistantMsg)
	}
	return m, waitForStreamMsg(m.streamCh)
}

func (m *CodeAgentModel) handleToolEvent(msg toolEventMsg) (tea.Model, tea.Cmd) {
	e := msg.Event
	refreshFrom := -1
	switch e.Type {
	case core.ToolEventStart:
		m.activeAssistantIdx = -1
		idx := m.appendChatMessage(ChatMessage{Role: "tool", ToolCallID: e.CallID, ToolName: e.ToolName, ToolPath: e.Path, ToolCommand: e.Command, ToolLibrary: e.Library, ToolVersion: e.Version, ToolQuery: e.Query, ToolState: "running", Content: "", IsPartial: true, ToolStartedAt: time.Now(), ToolInvocation: e.Invocation})
		m.activeToolIndices[e.CallID] = idx
		m.toolExpanded[e.CallID] = false
		m.refreshChatViewportIncremental(idx)
		return m, tea.Batch(waitForStreamMsg(m.streamCh), toolTickCmd())
	case core.ToolEventUpdate:
		if idx, ok := m.activeToolIndices[e.CallID]; ok && idx >= 0 && idx < len(m.chatMessages) {
			m.chatMessages[idx].Content = e.Output
			m.chatMessages[idx].IsPartial = true
			m.chatMessages[idx].ToolState = "running"
			refreshFrom = idx
		}
	case core.ToolEventEnd:
		if idx, ok := m.activeToolIndices[e.CallID]; ok && idx >= 0 && idx < len(m.chatMessages) {
			if m.chatMessages[idx].ToolCallID == e.CallID {
				m.invalidateTranscriptFrom(idx)
			}
			if strings.TrimSpace(e.Output) != "" {
				m.chatMessages[idx].Content = e.Output
			}
			if strings.TrimSpace(e.Path) != "" {
				m.chatMessages[idx].ToolPath = e.Path
			}
			if strings.TrimSpace(e.Command) != "" {
				m.chatMessages[idx].ToolCommand = e.Command
			}
			if strings.TrimSpace(e.Library) != "" {
				m.chatMessages[idx].ToolLibrary = e.Library
			}
			if strings.TrimSpace(e.Version) != "" {
				m.chatMessages[idx].ToolVersion = e.Version
			}
			if strings.TrimSpace(e.Query) != "" {
				m.chatMessages[idx].ToolQuery = e.Query
			}
			if strings.TrimSpace(e.Invocation.Name) != "" {
				m.chatMessages[idx].ToolInvocation = e.Invocation
			}
			m.chatMessages[idx].ToolResult = e.ResultSummary
			m.chatMessages[idx].IsPartial = false
			m.chatMessages[idx].ToolEndedAt = time.Now()
			if e.IsError {
				m.chatMessages[idx].ToolState = "error"
				m.toolExpanded[e.CallID] = true
			} else {
				m.chatMessages[idx].ToolState = "done"
			}
			toolContent := strings.TrimSpace(e.ContextContent)
			if toolContent == "" {
				toolContent = strings.TrimSpace(formatToolContextContent(e))
			}
			if toolContent != "" {
				callID := strings.TrimSpace(e.CallID)
				if callID != "" {
					meta := normalizeInvocationMeta(e.Invocation)
					if meta.Name == "" {
						meta.Name = strings.TrimSpace(e.ToolName)
					}
					m.toolInvocationByCallID[callID] = meta
					m.toolResultByCallID[callID] = e.ResultSummary
				}
				toolMsg := llm.Message{Role: "tool", ToolCallID: e.CallID, Name: e.ToolName, Content: toolContent}
				m.conversationHistory = append(m.conversationHistory, toolMsg)
				m.markContextEntriesDirty()
				if m.sessionStore != nil {
					_ = m.sessionStore.AppendMessage(toolMsg)
				}
			}
			delete(m.activeToolIndices, e.CallID)
			refreshFrom = idx
		}
	}
	if refreshFrom >= 0 {
		m.refreshChatViewportIncremental(refreshFrom)
	} else {
		m.refreshChatViewport()
	}
	return m, waitForStreamMsg(m.streamCh)
}

func (m *CodeAgentModel) handleToolTick() (tea.Model, tea.Cmd) {
	if m.hasRunningTool() {
		m.refreshChatViewport()
		return m, toolTickCmd()
	}
	return m, nil
}

// flushStreamState resets all stream-related fields after a stream ends for
// any reason (done, cancelled, error).  It also saves any partial assistant
// text that was already streamed to the conversation history so context is
// preserved across a cancel/restart.
func (m *CodeAgentModel) flushStreamState(savePartialText bool) {
	if savePartialText {
		assistantText := strings.TrimSpace(m.currentAssistantText.String())
		if assistantText != "" {
			needAppend := true
			if n := len(m.conversationHistory); n > 0 {
				last := m.conversationHistory[n-1]
				if last.Role == "assistant" && strings.TrimSpace(last.Content) == assistantText {
					needAppend = false
				}
			}
			if needAppend {
				assistantMsg := llm.Message{Role: "assistant", Content: assistantText}
				m.conversationHistory = append(m.conversationHistory, assistantMsg)
				m.markContextEntriesDirty()
				if m.sessionStore != nil {
					_ = m.sessionStore.AppendMessage(assistantMsg)
				}
			}
		}
	}
	m.currentAssistantText.Reset()
	m.activeAssistantIdx = -1
	m.streamCh = nil
	m.cancelStream = nil
	m.streamStartedAt = time.Time{}
	m.firstChunkAt = time.Time{}
	m.streamChunkCount = 0
	m.streamCharCount = 0
	m.activeToolIndices = map[string]int{}
	if m.toolInvocationByCallID == nil {
		m.toolInvocationByCallID = map[string]core.ToolInvocationMeta{}
	}
	if m.toolResultByCallID == nil {
		m.toolResultByCallID = map[string]core.ToolResultSummary{}
	}
	m.isWorking = false
	m.workingFrame = 0
}

func (m *CodeAgentModel) handleChatStreamDone() (tea.Model, tea.Cmd) {
	elapsed := time.Duration(0)
	if !m.streamStartedAt.IsZero() {
		elapsed = time.Since(m.streamStartedAt)
	}
	m.flushStreamState(true)

	if elapsed > 0 {
		m.finalizeWorkingSystemMessage(fmt.Sprintf("✓ Done in %s", elapsed.Round(time.Millisecond)), "done")
	} else {
		m.finalizeWorkingSystemMessage("✓ Done", "done")
	}
	m.refreshChatViewport()
	return m, nil
}

// handleChatStreamCancelled is called when the in-flight stream was cancelled
// (via Ctrl+C or a steering interrupt).  If a pending steering message exists
// it is injected into the conversation and a new stream is started immediately.
func (m *CodeAgentModel) handleChatStreamCancelled() (tea.Model, tea.Cmd) {
	// Save whatever the assistant had streamed so far — it becomes part of the
	// conversation context for the new (or resumed) request.
	m.flushStreamState(true)

	if m.pendingUserMessage != "" {
		pendingMsg := m.pendingUserMessage
		m.pendingUserMessage = ""
		m.appendSystemMessage("[Steer] Steering: injecting your message now…", "info")
		return m, m.continueWithPendingMessage(pendingMsg)
	}

	// Plain Ctrl+C cancel — just show a cancelled notice.
	m.finalizeWorkingSystemMessage("⊘ Cancelled", "info")
	m.refreshChatViewport()
	return m, nil
}

func (m *CodeAgentModel) handleChatStreamErr(msg chatStreamErrMsg) (tea.Model, tea.Cmd) {
	m.flushStreamState(false)
	m.finalizeWorkingSystemMessage("✗ "+msg.Err.Error(), "error")
	m.refreshChatViewport()
	return m, nil
}

func (m *CodeAgentModel) handleCompactDone(msg compactDoneMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		m.appendSystemMessage("[Compact] ✗ "+msg.Err.Error(), "error")
		return m, nil
	}
	if msg.History != nil {
		m.conversationHistory = append([]llm.Message(nil), msg.History...)
		m.markContextEntriesDirty()
	}
	if msg.Compacted {
		methodLabel := "model"
		if msg.Method == core.CompactionMethodDeterministic {
			methodLabel = "fallback synthetic"
		}
		m.appendSystemMessage("[Compact] ✓ Session compacted ("+methodLabel+")", "done")
		m.contextActions = nil
		m.contextModalEntries = nil
		m.lastPromptFingerprint = core.PromptFingerprint{}
		m.lastPromptHash = ""
		m.recordContextAction("Compaction applied")
	} else {
		m.appendSystemMessage("[Compact] No compaction needed", "info")
	}
	m.refreshChatViewport()
	return m, nil
}

func (m *CodeAgentModel) handleNewSessionDone(msg newSessionDoneMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		m.appendSystemMessage("[Session] ✗ "+msg.Err.Error(), "error")
		return m, nil
	}
	if m.sessionStore != nil {
		_ = m.sessionStore.Close()
	}
	m.sessionStore = msg.Store
	if m.sessionService != nil {
		m.sessionService.SetSessionStore(m.sessionStore)
	}
	if m.contextManager != nil {
		m.contextManager.ClearSessionSystemPromptOverride()
	}
	m.conversationHistory = m.sessionStore.ContextMessages()
	m.markContextEntriesDirty()
	m.currentAssistantText.Reset()
	m.rebuildTranscriptFromHistory()
	m.appendSystemMessage("[Session] ✓ New session started ("+msg.SessionID+")", "done")
	m.recordContextAction("New session started")
	return m, nil
}

func (m *CodeAgentModel) handleResumeSessionDone(msg resumeSessionDoneMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		m.appendSystemMessage("[Session] ✗ "+msg.Err.Error(), "error")
		return m, nil
	}
	if m.sessionStore != nil {
		_ = m.sessionStore.Close()
	}
	m.sessionStore = msg.Store
	sessionCWD := m.sessionStore.CWD()
	if strings.TrimSpace(sessionCWD) != "" {
		m.currentCwd = sessionCWD
		m.currentGitBranch = detectGitBranch(m.currentCwd)
		m.chatService = core.NewChatService(m.authStorage, tools.NewToolSet(m.currentCwd))
		if m.chatController != nil {
			m.chatController.SetChatService(m.chatService)
		}
		if m.providerService != nil {
			m.providerService.SetChatController(m.chatController)
		}
		if m.sessionService != nil {
			m.sessionService.SetChatController(m.chatController)
			m.sessionService.SetContextManager(m.contextManager)
			m.sessionService.SetSessionStore(m.sessionStore)
		}
	}
	if m.contextManager != nil {
		m.contextManager.SetCWD(m.currentCwd)
		m.contextManager.ClearSessionSystemPromptOverride()
	}
	m.reloadAvailableSkills()
	m.reloadAvailableExtensions()
	m.conversationHistory = m.sessionStore.ContextMessages()
	m.markContextEntriesDirty()
	m.currentAssistantText.Reset()
	m.rebuildTranscriptFromHistory()
	m.appendSystemMessage("[Session] ✓ Session resumed ("+m.sessionStore.SessionID()+")", "done")
	m.recordContextAction("Session resumed")
	return m, nil
}

func (m *CodeAgentModel) handleBashCommandDone(msg bashCommandDoneMsg) (tea.Model, tea.Cmd) {
	m.isExecutingBash = false
	m.workingFrame = 0
	if msg.IsCD && msg.Err == nil {
		if err := os.Chdir(msg.NewCwd); err != nil {
			msg.Err = err
			msg.Output = "Failed to change directory: " + err.Error()
		} else {
			m.currentCwd = msg.NewCwd
			m.currentGitBranch = detectGitBranch(m.currentCwd)
			m.chatService = core.NewChatService(m.authStorage, tools.NewToolSet(m.currentCwd))
			if m.chatController != nil {
				m.chatController.SetChatService(m.chatService)
			}
			if m.providerService != nil {
				m.providerService.SetChatController(m.chatController)
			}
			if m.sessionService != nil {
				m.sessionService.SetChatController(m.chatController)
				m.sessionService.SetContextManager(m.contextManager)
				m.sessionService.SetSessionStore(m.sessionStore)
			}
			if m.contextManager != nil {
				m.contextManager.SetCWD(m.currentCwd)
				m.markContextEntriesDirty()
			}
			m.reloadAvailableSkills()
			m.reloadAvailableExtensions()
		}
	}

	state := "done"
	if msg.Err != nil {
		state = "error"
	}
	if msg.Err != nil {
		m.finalizeWorkingSystemMessage("[Shell] ✗ Command failed", "error")
	} else {
		m.finalizeWorkingSystemMessage("[Shell] ✓ Command finished", "done")
	}
	idx := m.appendChatMessage(ChatMessage{Role: "tool", ToolName: "shell", ToolCommand: msg.Command, ToolState: state, Content: msg.Output, ToolStartedAt: msg.StartedAt, ToolEndedAt: msg.EndedAt})
	if msg.IsCD && msg.Err == nil {
		m.appendSystemMessage("[Shell] cwd: "+m.currentCwd, "info")
	}
	m.refreshChatViewportIncremental(idx)
	return m, nil
}

func (m *CodeAgentModel) handleWorkingTick() (tea.Model, tea.Cmd) {
	if m.isWorking || m.isExecutingBash {
		m.workingFrame = (m.workingFrame + 1) % 4
		if m.isWorking {
			m.updateWorkingSystemMessage(m.chatWorkingStatusText())
		} else if m.isExecutingBash {
			m.updateWorkingSystemMessage(m.bashWorkingStatusText())
		}
		m.refreshChatViewport()
		return m, workingTickCmd()
	}
	return m, nil
}

func (m *CodeAgentModel) handleFileAdded(msg FileAddedMsg) (tea.Model, tea.Cmd) {
	path := strings.TrimSpace(msg.Path)
	if path == "" {
		return m, nil
	}

	// Close the file browser modal
	m.closeFileBrowserModal()

	// Record context action
	m.recordContextAction("File reference added: " + path)
	// Insert the file path into the textarea as an attachment token.
	reference := " `" + path + "`"

	// Get current value
	currentValue := m.ta.Value()

	// Append the reference to the end (simplest approach for now)
	newValue := currentValue + reference
	m.ta.SetValue(newValue)

	// Move cursor to end
	m.ta.CursorEnd()

	// Focus back on textarea
	m.ta.Focus()
	m.applyInputMode(inputModeChat)

	// Refresh the view
	m.recalculateLayout()
	m.refreshChatViewport()

	return m, nil
}

// readFileForContext reads a file and returns its content as a string.
// It uses the same truncation logic as the core Read tool.
func (m *CodeAgentModel) readFileForContext(path string) (string, error) {
	// Get the Read tool from the toolset
	if m.chatService == nil || m.chatService.Tools() == nil {
		return "", fmt.Errorf("toolset not available")
	}

	readTool := m.chatService.Tools().Read
	if readTool == nil {
		return "", fmt.Errorf("read tool not available")
	}

	// Execute the read tool
	ctx := m.lifecycleContext()
	result, err := readTool.Execute(ctx, tools.ReadInput{Path: path})
	if err != nil {
		return "", err
	}

	// Convert result to text
	var content strings.Builder
	for _, part := range result.Content {
		if part.Type == tools.ContentPartText {
			content.WriteString(part.Text)
		}
	}

	return content.String(), nil
}

func (m *CodeAgentModel) continueWithPendingMessage(pendingMsg string) tea.Cmd {
	// Add the pending message as a user message
	m.appendChatMessage(ChatMessage{Role: "user", Content: pendingMsg})
	m.conversationHistory = append(m.conversationHistory, llm.Message{Role: "user", Content: pendingMsg})
	m.markContextEntriesDirty()
	if m.sessionStore != nil {
		if err := m.sessionStore.AppendMessage(llm.Message{Role: "user", Content: pendingMsg}); err == nil {
			// Full content (including any inlined large-paste text) is now
			// durably persisted — safe to remove the paste temp files.
			for _, p := range m.pendingPastePaths {
				cleanupPasteTempFile(p)
			}
		}
		m.pendingPastePaths = nil
	}

	// Start a new stream with updated history
	history, err := m.chatHistoryAsLLM()
	if err != nil {
		m.appendSystemMessage("✗ Failed to build context: "+err.Error(), "error")
		return nil
	}

	m.activeAssistantIdx = -1
	m.isWorking = true
	m.chatAutoScroll = true
	m.activeToolIndices = map[string]int{}
	m.streamStartedAt = time.Now()
	m.firstChunkAt = time.Time{}
	m.streamChunkCount = 0
	m.streamCharCount = 0
	m.workingFrame = 0
	m.setWorkingSystemMessage(m.chatWorkingStatusText())
	m.refreshChatViewport()

	return tea.Batch(m.startChatStream(history), workingTickCmd())
}
