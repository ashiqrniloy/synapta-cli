package components

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/ashiqrniloy/synapta-cli/internal/core"
	"github.com/ashiqrniloy/synapta-cli/internal/core/tools"
	"github.com/ashiqrniloy/synapta-cli/internal/llm"
	"github.com/ashiqrniloy/synapta-cli/internal/oauth"
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
	if m.chatService == nil {
		return func() tea.Msg { return chatStreamErrMsg{Err: fmt.Errorf("chat service not available")} }
	}

	// Cancel any previously running stream.
	if m.cancelStream != nil {
		m.cancelStream()
		m.cancelStream = nil
	}

	providerID := m.selectedProvider
	modelID := m.selectedModelID
	streamCh := make(chan tea.Msg, 256)
	m.streamCh = streamCh

	// Create a cancellable context and store the cancel func on the model so
	// that Ctrl+C (or a steering interrupt) can cancel the in-flight request.
	ctx, cancel := m.withLifecycleTimeout(0)
	m.cancelStream = cancel

	go func() {
		defer close(streamCh)
		defer cancel() // ensure ctx is always released

		err := m.chatService.Stream(
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
				// Cancelled by user (Ctrl+C) or by a steering interrupt.
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
		if m.authStorage == nil {
			return providerBalanceMsg{ProviderID: providerID}
		}

		ctx, cancel := m.withLifecycleTimeout(defaultBalanceCheckTimeout)
		defer cancel()

		switch providerID {
		case "kilo":
			creds, err := m.authStorage.GetOAuthCredentials("kilo")
			if err != nil || creds == nil || strings.TrimSpace(creds.Access) == "" {
				return providerBalanceMsg{ProviderID: providerID}
			}
			gateway := llm.NewKiloGateway()
			balance, err := gateway.FetchBalance(ctx, creds.Access)
			if err != nil {
				return providerBalanceMsg{ProviderID: providerID, Err: err}
			}
			return providerBalanceMsg{ProviderID: providerID, Balance: llm.FormatBalance(balance)}
		case "github-copilot":
			creds, err := m.authStorage.GetOAuthCredentials("github-copilot")
			if err != nil || creds == nil || strings.TrimSpace(creds.Refresh) == "" {
				return providerBalanceMsg{ProviderID: providerID}
			}
			domain := "github.com"
			if len(creds.ExtraData) > 0 {
				var extra oauth.CopilotExtraData
				if err := json.Unmarshal(creds.ExtraData, &extra); err == nil && strings.TrimSpace(extra.EnterpriseDomain) != "" {
					domain = strings.TrimSpace(extra.EnterpriseDomain)
				}
			}
			usage, err := oauth.FetchCopilotPremiumUsage(ctx, creds.Refresh, domain)
			if err != nil || usage == nil || usage.Total <= 0 {
				return providerBalanceMsg{ProviderID: providerID}
			}
			return providerBalanceMsg{ProviderID: providerID, Balance: fmt.Sprintf("Premium %d/%d", usage.Used, usage.Total)}
		default:
			return providerBalanceMsg{ProviderID: providerID}
		}
	}
}

func (m *CodeAgentModel) executeBashCommand(command string, startedAt time.Time) tea.Cmd {
	cwd := m.currentCwd
	if cwd == "" {
		cwd, _ = os.Getwd()
	}

	return func() tea.Msg {
		if target, ok := parseCDCommand(command); ok {
			resolved, err := resolveCDTarget(cwd, target)
			ended := time.Now()
			if err != nil {
				return bashCommandDoneMsg{
					Command:   command,
					Output:    "cd: " + err.Error(),
					Err:       err,
					StartedAt: startedAt,
					EndedAt:   ended,
					IsCD:      true,
				}
			}
			return bashCommandDoneMsg{
				Command:   command,
				Output:    "Changed directory to " + resolved,
				StartedAt: startedAt,
				EndedAt:   ended,
				NewCwd:    resolved,
				IsCD:      true,
			}
		}

		bashTool := tools.NewBashTool(cwd)
		res, err := bashTool.Execute(m.lifecycleContext(), tools.BashInput{Command: command}, nil)
		output := toolResultPlainText(res)
		if strings.TrimSpace(output) == "" && err != nil {
			output = err.Error()
		}

		return bashCommandDoneMsg{
			Command:   command,
			Output:    output,
			Err:       err,
			StartedAt: startedAt,
			EndedAt:   time.Now(),
		}
	}
}

func (m *CodeAgentModel) loadSessions() tea.Cmd {
	return func() tea.Msg {
		sessions, err := core.ListAllSessions(m.agentDir, core.AgentCode)
		if err != nil {
			return SessionsLoadErrMsg{Err: err}
		}
		return SessionsLoadedMsg{Sessions: sessions}
	}
}

func (m *CodeAgentModel) newSessionCmd() tea.Cmd {
	return func() tea.Msg {
		store := m.sessionStore
		if store == nil {
			var err error
			store, err = core.NewSessionStore(m.agentDir, core.AgentCode, m.currentCwd, core.DefaultCompactionSettings())
			if err != nil {
				return newSessionDoneMsg{Err: err}
			}
		}
		if err := store.StartNewSession(); err != nil {
			return newSessionDoneMsg{Err: err}
		}
		return newSessionDoneMsg{Store: store, SessionID: store.SessionID()}
	}
}

func (m *CodeAgentModel) resumeSessionCmd(sessionPath string) tea.Cmd {
	return func() tea.Msg {
		store, err := core.OpenSessionStore(m.agentDir, core.AgentCode, m.currentCwd, sessionPath, core.DefaultCompactionSettings())
		if err != nil {
			return resumeSessionDoneMsg{Err: err}
		}
		return resumeSessionDoneMsg{Store: store}
	}
}

func (m *CodeAgentModel) manualCompactCmd() tea.Cmd {
	return func() tea.Msg {
		if m.sessionStore == nil {
			return compactDoneMsg{Err: fmt.Errorf("session store not available")}
		}

		ctx, cancel := m.withLifecycleTimeout(defaultModelFetchTimeout)
		defer cancel()

		contextWindow := 128000
		if m.chatService != nil && m.selectedProvider != "" && m.selectedModelID != "" {
			if cw, err := m.chatService.ModelContextWindow(ctx, m.selectedProvider, m.selectedModelID); err == nil && cw > 0 {
				contextWindow = cw
			}
		}

		summarizer := func(ctx context.Context, toSummarize []llm.Message, previousSummary string) (string, error) {
			if m.chatService == nil || m.selectedProvider == "" || m.selectedModelID == "" {
				return "", nil
			}
			messagesForSummary := toSummarize
			if m.contextManager != nil {
				if built, err := m.contextManager.Build(toSummarize); err == nil && len(built) > 0 {
					messagesForSummary = built
				}
			}
			return m.chatService.SummarizeCompaction(ctx, m.selectedProvider, m.selectedModelID, messagesForSummary, previousSummary)
		}

		compacted, method, err := m.sessionStore.ManualCompact(ctx, contextWindow, summarizer)
		if err != nil {
			return compactDoneMsg{Err: err}
		}
		history := m.sessionStore.ContextMessages()
		return compactDoneMsg{Compacted: compacted, History: history, Method: method}
	}
}

func (m *CodeAgentModel) loadModels() tea.Cmd {
	return func() tea.Msg {
		if m.chatService == nil {
			return ModelsLoadedMsg{}
		}

		ctx, cancel := m.withLifecycleTimeout(defaultModelFetchTimeout)
		defer cancel()

		available, err := m.chatService.AvailableModels(ctx)
		if err != nil {
			return ModelsLoadErrMsg{Err: err}
		}

		models := make([]ModelInfo, 0, len(available))
		for _, model := range available {
			name := model.Name
			if tier := copilotModelTier(model.Provider, model.ID); tier != "" {
				name = fmt.Sprintf("%s [%s]", name, strings.ToUpper(tier))
			}
			models = append(models, ModelInfo{
				Provider: model.Provider,
				ID:       model.ID,
				Name:     name,
			})
		}

		sort.SliceStable(models, func(i, j int) bool {
			ri, rj := modelProviderRank(models[i].Provider), modelProviderRank(models[j].Provider)
			if ri != rj {
				return ri < rj
			}
			if models[i].Provider != models[j].Provider {
				return models[i].Provider < models[j].Provider
			}
			return strings.ToLower(models[i].Name) < strings.ToLower(models[j].Name)
		})

		return ModelsLoadedMsg{Models: models}
	}
}

func (m *CodeAgentModel) startKiloAuth() tea.Cmd {
	return func() tea.Msg {
		gateway := llm.NewKiloGateway()
		ctx, cancel := m.withLifecycleTimeout(0)
		defer cancel()

		var verificationURL string

		creds, err := gateway.Login(ctx, llm.DeviceAuthCallbacks{
			OnAuth: func(url, code string) {
				verificationURL = url
				// Open browser automatically
				if err := openBrowser(url); err != nil {
					fmt.Printf("Failed to open browser: %v\n", err)
					fmt.Printf("Please open this URL manually: %s\n", url)
				}
			},
			OnProgress: func(msg string) {
				// Can't send progress messages from here in bubbletea v2
				// Progress will be shown after completion
			},
			Signal: ctx,
		})

		if err != nil {
			// If we have a verification URL, include it in the error message
			if verificationURL != "" {
				return KiloAuthCompleteMsg{
					Err: fmt.Errorf("%w\nOpen this URL: %s", err, verificationURL),
				}
			}
			return KiloAuthCompleteMsg{Err: err}
		}

		// Store credentials
		if m.authStorage != nil {
			if err := m.authStorage.SetOAuthCredentials("kilo", creds); err != nil {
				return KiloAuthCompleteMsg{
					Err: fmt.Errorf("authenticated but failed to store credentials: %w", err),
				}
			}
		}

		// Fetch all models with the token
		models, err := gateway.FetchModels(creds.Access)
		if err != nil {
			return KiloAuthCompleteMsg{
				Err: fmt.Errorf("authenticated but failed to fetch models: %w", err),
			}
		}

		return KiloAuthCompleteMsg{
			Email:      "Authenticated",
			ModelCount: len(models),
		}
	}
}

func (m *CodeAgentModel) startCopilotAuth() tea.Cmd {
	authCh := make(chan tea.Msg, 32)
	m.authCh = authCh

	go func() {
		defer close(authCh)

		provider := oauth.NewGitHubCopilotOAuth("")
		ctx, cancel := m.withLifecycleTimeout(0)
		defer cancel()

		authCh <- CopilotAuthProgressMsg("Initiating device authorization...")

		creds, err := provider.Login(llm.OAuthLoginCallbacks{
			OnAuth: func(url string, instructions string) {
				code := strings.TrimSpace(strings.TrimPrefix(instructions, "Enter code:"))
				authCh <- CopilotAuthPromptMsg{VerificationURL: url, UserCode: code}
				if err := openBrowser(url); err != nil {
					authCh <- CopilotAuthProgressMsg("Could not open browser automatically. Open the URL manually.")
				}
			},
			OnProgress: func(message string) {
				authCh <- CopilotAuthProgressMsg(message)
			},
			OnPrompt: func(message string, placeholder string, allowEmpty bool) (string, error) {
				// For now we default to github.com in TUI flow.
				return "", nil
			},
			Signal: ctx,
		})
		if err != nil {
			authCh <- CopilotAuthCompleteMsg{Err: err}
			authCh <- authFlowDoneMsg{}
			return
		}

		if m.authStorage != nil {
			if err := m.authStorage.SetOAuthCredentials("github-copilot", creds); err != nil {
				authCh <- CopilotAuthCompleteMsg{Err: fmt.Errorf("authenticated but failed to store credentials: %w", err)}
				authCh <- authFlowDoneMsg{}
				return
			}
		}

		authCh <- CopilotAuthCompleteMsg{ModelCount: len(llm.GitHubCopilotDefaultModels())}
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
	default: // linux, freebsd, etc.
		cmd = exec.Command("xdg-open", url)
	}

	return cmd.Start()
}
