package components

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/ashiqrniloy/synapta-cli/internal/application"
	"github.com/ashiqrniloy/synapta-cli/internal/core"
	"github.com/ashiqrniloy/synapta-cli/internal/llm"
)

func (m *CodeAgentModel) lifecycleContext() context.Context {
	if m != nil && m.lifecycleCtx != nil {
		return m.lifecycleCtx
	}
	return context.Background()
}

func (m *CodeAgentModel) withLifecycleTimeout(timeout time.Duration) (context.Context, context.CancelFunc) {
	base := m.lifecycleContext()
	if timeout > 0 {
		return context.WithTimeout(base, timeout)
	}
	return context.WithCancel(base)
}

func (m *CodeAgentModel) startChatStream(history []llm.Message) tea.Cmd {
	if m.chatController == nil {
		return func() tea.Msg { return chatStreamErrMsg{Err: fmt.Errorf("chat service not available")} }
	}

	if m.cancelStream != nil {
		m.cancelStream()
		m.cancelStream = nil
	}

	providerID := m.selectedProvider
	modelID := m.selectedModelID
	streamCh := make(chan tea.Msg, 256)
	m.streamCh = streamCh

	ctx, cancel := m.withLifecycleTimeout(0)
	m.cancelStream = cancel

	go func() {
		defer close(streamCh)
		defer cancel()

		err := m.chatController.Stream(
			ctx,
			providerID,
			modelID,
			history,
			func(text string) error {
				select {
				case <-ctx.Done():
					return ctx.Err()
				default:
					streamCh <- chatStreamChunkMsg{Text: text}
					return nil
				}
			},
			func(message llm.Message) error {
				select {
				case <-ctx.Done():
					return ctx.Err()
				default:
					copyMsg := message
					if len(message.ToolCalls) > 0 {
						copyMsg.ToolCalls = append([]llm.ToolCall(nil), message.ToolCalls...)
					}
					streamCh <- assistantToolCallsMsg{Message: copyMsg}
					return nil
				}
			},
			func(event core.ToolEvent) error {
				select {
				case <-ctx.Done():
					return ctx.Err()
				default:
					streamCh <- toolEventMsg{Event: event}
					return nil
				}
			},
		)
		if err != nil {
			if ctx.Err() == context.Canceled {
				streamCh <- chatStreamCancelledMsg{}
			} else {
				streamCh <- chatStreamErrMsg{Err: err}
			}
			return
		}
		streamCh <- chatStreamDoneMsg{}
	}()

	return waitForStreamMsg(streamCh)
}

func waitForStreamMsg(ch <-chan tea.Msg) tea.Cmd {
	if ch == nil {
		return nil
	}
	return func() tea.Msg {
		msg, ok := <-ch
		if !ok {
			return chatStreamDoneMsg{}
		}
		return msg
	}
}

func toolTickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(_ time.Time) tea.Msg {
		return toolTickMsg{}
	})
}

func workingTickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(_ time.Time) tea.Msg {
		return workingTickMsg{}
	})
}

func (m *CodeAgentModel) fetchProviderBalanceCmd(providerID string) tea.Cmd {
	return func() tea.Msg {
		if m.providerService == nil {
			return providerBalanceMsg{ProviderID: providerID}
		}

		ctx, cancel := m.withLifecycleTimeout(defaultBalanceCheckTimeout)
		defer cancel()

		balance, err := m.providerService.FetchBalance(ctx, providerID)
		if err != nil {
			return providerBalanceMsg{ProviderID: providerID, Err: err}
		}
		return providerBalanceMsg{ProviderID: providerID, Balance: balance}
	}
}

func (m *CodeAgentModel) executeBashCommand(command string, startedAt time.Time) tea.Cmd {
	cwd := m.currentCwd
	if m.workspaceService != nil {
		cwd = m.workspaceService.ResolveCurrentDir(cwd)
	}

	return func() tea.Msg {
		if m.workspaceService != nil {
			if target, ok := m.workspaceService.ParseCDCommand(command); ok {
				resolved, err := m.workspaceService.ResolveCDTarget(cwd, target)
				ended := time.Now()
				if err != nil {
					return bashCommandDoneMsg{Command: command, Output: "cd: " + err.Error(), Err: err, StartedAt: startedAt, EndedAt: ended, IsCD: true}
				}
				return bashCommandDoneMsg{Command: command, Output: "Changed directory to " + resolved, StartedAt: startedAt, EndedAt: ended, NewCwd: resolved, IsCD: true}
			}
			output, err := m.workspaceService.ExecuteBash(m.lifecycleContext(), cwd, command)
			return bashCommandDoneMsg{Command: command, Output: output, Err: err, StartedAt: startedAt, EndedAt: time.Now()}
		}
		return bashCommandDoneMsg{Command: command, Output: "workspace service not available", Err: fmt.Errorf("workspace service not available"), StartedAt: startedAt, EndedAt: time.Now()}
	}
}

func (m *CodeAgentModel) loadSessions() tea.Cmd {
	return func() tea.Msg {
		if m.sessionService == nil {
			return SessionsLoadErrMsg{Err: fmt.Errorf("session service not available")}
		}
		sessions, err := m.sessionService.ListAll()
		if err != nil {
			return SessionsLoadErrMsg{Err: err}
		}
		return SessionsLoadedMsg{Sessions: sessions}
	}
}

func (m *CodeAgentModel) newSessionCmd() tea.Cmd {
	return func() tea.Msg {
		if m.sessionService == nil {
			return newSessionDoneMsg{Err: fmt.Errorf("session service not available")}
		}
		store, sessionID, err := m.sessionService.StartNew(m.currentCwd)
		if err != nil {
			return newSessionDoneMsg{Err: err}
		}
		return newSessionDoneMsg{Store: store, SessionID: sessionID}
	}
}

func (m *CodeAgentModel) resumeSessionCmd(sessionPath string) tea.Cmd {
	return func() tea.Msg {
		if m.sessionService == nil {
			return resumeSessionDoneMsg{Err: fmt.Errorf("session service not available")}
		}
		store, err := m.sessionService.Resume(m.currentCwd, sessionPath)
		if err != nil {
			return resumeSessionDoneMsg{Err: err}
		}
		return resumeSessionDoneMsg{Store: store}
	}
}

func (m *CodeAgentModel) manualCompactCmd() tea.Cmd {
	return func() tea.Msg {
		if m.sessionService == nil {
			return compactDoneMsg{Err: fmt.Errorf("session service not available")}
		}
		ctx, cancel := m.withLifecycleTimeout(defaultManualCompactionTimeout)
		defer cancel()
		compacted, history, method, err := m.sessionService.ManualCompact(ctx, m.selectedProvider, m.selectedModelID)
		if err != nil {
			return compactDoneMsg{Err: err}
		}
		return compactDoneMsg{Compacted: compacted, History: history, Method: method}
	}
}

func (m *CodeAgentModel) loadModels() tea.Cmd {
	return func() tea.Msg {
		if m.providerService == nil {
			return ModelsLoadedMsg{}
		}

		ctx, cancel := m.withLifecycleTimeout(defaultModelFetchTimeout)
		defer cancel()

		available, err := m.providerService.AvailableModels(ctx, modelProviderRank, copilotModelTier)
		if err != nil {
			return ModelsLoadErrMsg{Err: err}
		}

		models := make([]ModelInfo, 0, len(available))
		for _, model := range available {
			models = append(models, ModelInfo{Provider: model.Provider, ID: model.ID, Name: model.Name})
		}
		return ModelsLoadedMsg{Models: models}
	}
}

func (m *CodeAgentModel) startKiloAuth() tea.Cmd {
	return func() tea.Msg {
		if m.providerService == nil {
			return KiloAuthCompleteMsg{Err: fmt.Errorf("provider service not available")}
		}
		ctx, cancel := m.withLifecycleTimeout(0)
		defer cancel()
		modelCount, err := m.providerService.AuthenticateKilo(ctx, func(url string) {
			if err := openBrowser(url); err != nil {
				fmt.Printf("Failed to open browser: %v\n", err)
				fmt.Printf("Please open this URL manually: %s\n", url)
			}
		})
		if err != nil {
			return KiloAuthCompleteMsg{Err: err}
		}
		return KiloAuthCompleteMsg{Email: "Authenticated", ModelCount: modelCount}
	}
}

func (m *CodeAgentModel) startCopilotAuth() tea.Cmd {
	authCh := make(chan tea.Msg, 32)
	m.authCh = authCh

	go func() {
		defer close(authCh)
		if m.providerService == nil {
			authCh <- CopilotAuthCompleteMsg{Err: fmt.Errorf("provider service not available")}
			authCh <- authFlowDoneMsg{}
			return
		}

		ctx, cancel := m.withLifecycleTimeout(0)
		defer cancel()

		m.providerService.AuthenticateCopilot(ctx,
			func(event application.CopilotAuthEvent) {
				if strings.TrimSpace(event.Progress) != "" {
					authCh <- CopilotAuthProgressMsg(event.Progress)
				}
				if event.Prompt != nil {
					authCh <- CopilotAuthPromptMsg{VerificationURL: event.Prompt.VerificationURL, UserCode: event.Prompt.UserCode}
				}
				if event.Complete != nil {
					authCh <- CopilotAuthCompleteMsg{Err: event.Complete.Err, ModelCount: event.Complete.ModelCount}
				}
			},
			func(url string) {
				if err := openBrowser(url); err != nil {
					authCh <- CopilotAuthProgressMsg("Could not open browser automatically. Open the URL manually.")
				}
			},
		)
		authCh <- authFlowDoneMsg{}
	}()

	return waitForAuthMsg(authCh)
}

func waitForAuthMsg(ch <-chan tea.Msg) tea.Cmd {
	if ch == nil {
		return nil
	}
	return func() tea.Msg {
		msg, ok := <-ch
		if !ok {
			return authFlowDoneMsg{}
		}
		return msg
	}
}

func openBrowser(url string) error {
	var cmd *exec.Cmd

	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}

	return cmd.Start()
}
