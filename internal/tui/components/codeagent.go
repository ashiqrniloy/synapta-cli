package components

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"

	"github.com/synapta/synapta-cli/internal/config"
	"github.com/synapta/synapta-cli/internal/core"
	"github.com/synapta/synapta-cli/internal/core/tools"
	"github.com/synapta/synapta-cli/internal/llm"
	"github.com/synapta/synapta-cli/internal/tui/theme"
)

// ─── Kilo Auth Messages ─────────────────────────────────────────────

// KiloAuthProgressMsg reports progress during Kilo authentication.
type KiloAuthProgressMsg string

// KiloAuthCompleteMsg is sent when Kilo authentication completes.
type KiloAuthCompleteMsg struct {
	Err        error
	Email      string
	ModelCount int
}

// ModelsLoadedMsg contains the loaded models for selection.
type ModelsLoadedMsg struct {
	Models []ModelInfo
}

type ModelsLoadErrMsg struct {
	Err error
}

// ModelSelectedMsg is sent when a model is selected.
type ModelSelectedMsg struct {
	ModelID   string
	ModelName string
}

// ChatMessage represents a transcript entry in the chat.
type ChatMessage struct {
	Role          string // "user", "assistant", "tool"
	Content       string
	ToolCallID    string
	ToolName      string
	ToolState     string // "running", "done", "error"
	IsPartial     bool
	ToolStartedAt time.Time
	ToolEndedAt   time.Time
}

type chatStreamChunkMsg struct {
	Text string
}

type toolEventMsg struct {
	Event core.ToolEvent
}

type toolTickMsg struct{}
type workingTickMsg struct{}

type chatStreamDoneMsg struct{}

type chatStreamErrMsg struct {
	Err error
}

type bashCommandDoneMsg struct {
	Command   string
	Output    string
	Err       error
	StartedAt time.Time
	EndedAt   time.Time
	NewCwd    string
	IsCD      bool
}

const (
	assistantPlaceholder = "Working..."
	inputModeChat        = "chat"
	inputModeBash        = "bash"
)

// CodeAgentModel is the main TUI model for the Synapta Code agent.
type CodeAgentModel struct {
	width       int
	height      int
	styles      *theme.Styles
	ta          textarea.Model
	quit        bool
	borderColor string
	msgs        []string
	cfg         *config.AppConfig
	authStorage *llm.AuthStorage

	// Inline command picker
	picker *CommandPicker

	// Selected model
	selectedModelName string
	selectedModelID   string
	selectedProvider  string

	// Chat messages
	chatMessages       []ChatMessage
	isWorking          bool
	activeAssistantIdx int
	activeToolIndices  map[string]int
	toolExpanded       map[string]bool
	streamCh           <-chan tea.Msg
	chatService        *core.ChatService
	chatViewport       viewport.Model
	chatAutoScroll     bool
	streamStartedAt    time.Time
	firstChunkAt       time.Time
	streamChunkCount   int
	streamCharCount    int

	inputMode       string
	currentCwd      string
	isExecutingBash bool
	workingFrame    int
}

// NewCodeAgentModel creates the model using the loaded AppConfig.
func NewCodeAgentModel(cfg *config.AppConfig) *CodeAgentModel {
	t := cfg.ActiveTheme()
	styles := theme.NewStyles(t)

	// Initialize auth storage
	homeDir, _ := os.UserHomeDir()
	authDir := homeDir + "/.synapta"
	authStorage, _ := llm.NewAuthStorage(authDir)

	vp := viewport.New(viewport.WithWidth(80), viewport.WithHeight(10))
	vp.SoftWrap = true
	vp.FillHeight = true

	cwd, _ := os.Getwd()
	toolset := core.NewToolSet(cwd)

	model := &CodeAgentModel{
		styles:             styles,
		ta:                 buildTextarea(t, cfg),
		borderColor:        t.Border,
		cfg:                cfg,
		picker:             NewCommandPicker(styles),
		authStorage:        authStorage,
		chatService:        core.NewChatService(authStorage, toolset),
		activeAssistantIdx: -1,
		activeToolIndices:  map[string]int{},
		toolExpanded:       map[string]bool{},
		chatViewport:       vp,
		chatAutoScroll:     true,
		inputMode:          inputModeChat,
		currentCwd:         cwd,
	}

	if cfg.Provider.Default != "" && cfg.Provider.Model != "" {
		model.selectedProvider = cfg.Provider.Default
		model.selectedModelID = cfg.Provider.Model
		model.selectedModelName = cfg.Provider.Model
	}

	// Check if already authenticated and show model count
	if authStorage != nil && authStorage.HasAuth("kilo") {
		model.msgs = append(model.msgs, "[Kilo] ✓ Authenticated")
	}

	model.applyInputMode(inputModeChat)
	return model
}

func buildTextarea(t config.Theme, cfg *config.AppConfig) textarea.Model {
	ta := textarea.New()
	ta.Placeholder = "Type your message... (Enter=send, Shift+Enter=newline)"
	ta.ShowLineNumbers = false

	// Enable dynamic height - textarea will grow/shrink based on content
	ta.DynamicHeight = true
	ta.MinHeight = 1
	ta.MaxHeight = 15 // Leave room for title and padding

	// Clean, minimal styling using lipgloss v2
	noBg := lipgloss.NewStyle().Foreground(lipgloss.Color(t.Foreground))
	placeholder := lipgloss.NewStyle().Foreground(lipgloss.Color(t.Muted))
	empty := lipgloss.NewStyle()

	// v2 uses ta.Styles() method instead of ta.FocusedStyle
	styles := ta.Styles()
	styles.Focused.Base = noBg
	styles.Focused.Text = noBg
	styles.Focused.CursorLine = empty
	styles.Focused.CursorLineNumber = empty
	styles.Focused.EndOfBuffer = empty
	styles.Focused.Placeholder = placeholder
	styles.Focused.Prompt = empty
	styles.Focused.LineNumber = empty

	styles.Blurred.Base = noBg
	styles.Blurred.Text = noBg
	styles.Blurred.CursorLine = empty
	styles.Blurred.CursorLineNumber = empty
	styles.Blurred.EndOfBuffer = empty
	styles.Blurred.Placeholder = placeholder
	styles.Blurred.Prompt = empty
	styles.Blurred.LineNumber = empty
	ta.SetStyles(styles)

	ta.SetWidth(80)
	ta.Focus()

	// In v2, Enter is the default for newline
	// We want Shift+Enter for newline and Enter for submit
	// So we disable the textarea's newline handling on Enter
	// and handle it ourselves
	ta.KeyMap.InsertNewline.SetKeys() // Remove all keys for insert newline
	// Shift+Enter will be handled in Update

	return ta
}

// normalizeKeyName converts config key names to bubbletea v2 format
func normalizeKeyName(key string) string {
	lower := strings.ToLower(key)
	replacer := strings.NewReplacer(
		"escape", "esc",
	)
	return replacer.Replace(lower)
}

// getSubmitKey returns the configured submit key (default: enter)
func (m *CodeAgentModel) getSubmitKey() string {
	if m.cfg != nil && m.cfg.Keybindings.Submit != "" {
		return normalizeKeyName(m.cfg.Keybindings.Submit)
	}
	return "enter"
}

// getQuitKey returns the configured quit key
func (m *CodeAgentModel) getQuitKey() string {
	if m.cfg != nil && m.cfg.Keybindings.Quit != "" {
		return normalizeKeyName(m.cfg.Keybindings.Quit)
	}
	return "ctrl+c"
}

// getFilterText returns the text used for filtering (after the ':' prefix).
func (m *CodeAgentModel) getFilterText() string {
	value := m.ta.Value()
	if strings.HasPrefix(value, ":") {
		return value[1:] // everything after ':'
	}
	return ""
}

// clearCommandMode clears the input and exits command mode.
func (m *CodeAgentModel) clearCommandMode() {
	m.ta.SetValue("")
	m.picker.Deactivate()
	m.recalculateLayout()
}

func (m *CodeAgentModel) applyInputMode(mode string) {
	m.inputMode = mode
	switch mode {
	case inputModeBash:
		m.ta.Placeholder = "bash> Enter command (Enter=run, Esc=exit bash mode)"
	default:
		m.inputMode = inputModeChat
		m.ta.Placeholder = "Type your message... (Enter=send, Shift+Enter=newline)"
	}
}

// ─── tea.Model ───────────────────────────────────────────────────────

func (m CodeAgentModel) Init() tea.Cmd {
	return textarea.Blink
}

func (m CodeAgentModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.recalculateLayout()
		m.refreshChatViewport()
		return m, nil

	case CommandActionMsg:
		// Handle completed command
		if len(msg.Path) == 0 {
			m.clearCommandMode()
			return m, nil
		}

		commandID := msg.Path[0].ID
		switch commandID {
		case "bash":
			m.clearCommandMode()
			m.applyInputMode(inputModeBash)
			m.msgs = append(m.msgs, "[Bash] Mode enabled. Enter a command and press Enter.")
			return m, nil
		case "add-provider":
			m.clearCommandMode()
			if len(msg.Path) > 1 {
				providerID := msg.Path[1].ID
				if providerID == "kilo" {
					// Start Kilo Gateway OAuth flow
					m.msgs = append(m.msgs, "[Kilo] Starting authentication...")
					return m, m.startKiloAuth()
				}
				m.msgs = append(m.msgs, "[Add Provider] Selected: "+msg.Path[1].Name)
			}
		case "set-model":
			if len(msg.Path) > 1 {
				providerID, modelID, ok := parseModelSelectionKey(msg.Path[1].ID)
				if !ok {
					m.msgs = append(m.msgs, "[Model] Invalid selection")
					m.clearCommandMode()
					return m, nil
				}

				m.selectedProvider = providerID
				m.selectedModelID = modelID
				m.selectedModelName = msg.Path[1].Name
				if m.cfg != nil {
					if err := m.cfg.SetProvider(providerID, modelID); err != nil {
						m.msgs = append(m.msgs, "[Model] Warning: failed to save config: "+err.Error())
					}
				}
				m.clearCommandMode()
				m.msgs = append(m.msgs, "[Model] Selected: "+msg.Path[1].Name)
			} else {
				// Need to load models first
				return m, m.loadModels()
			}
		}
		return m, nil

	case ModelsLoadedMsg:
		if len(msg.Models) == 0 {
			m.msgs = append(m.msgs, "[Model] No models available. Add a provider first.")
			m.clearCommandMode()
			return m, nil
		}
		// Load models into picker (preserves breadcrumb path)
		m.picker.LoadModels(msg.Models)
		// Clear filter for new items
		m.ta.SetValue(":")
		m.recalculateLayout()
		return m, nil

	case ModelsLoadErrMsg:
		m.msgs = append(m.msgs, "[Model] ✗ "+msg.Err.Error())
		m.clearCommandMode()
		return m, nil

	case KiloAuthProgressMsg:
		m.msgs = append(m.msgs, "[Kilo] "+string(msg))
		return m, nil

	case KiloAuthCompleteMsg:
		if msg.Err != nil {
			// Split error message into multiple lines for readability
			errLines := strings.Split(msg.Err.Error(), "\n")
			m.msgs = append(m.msgs, "[Kilo] ✗ Authentication failed:")
			for _, line := range errLines {
				m.msgs = append(m.msgs, "[Kilo]   "+line)
			}
		} else {
			m.msgs = append(m.msgs, "[Kilo] ✓ Authentication successful! "+msg.Email)
			m.msgs = append(m.msgs, "[Kilo] ✓ "+fmt.Sprintf("%d models available", msg.ModelCount))
		}
		return m, nil

	case chatStreamChunkMsg:
		if m.firstChunkAt.IsZero() {
			m.firstChunkAt = time.Now()
		}
		m.streamChunkCount++
		m.streamCharCount += len(msg.Text)
		if m.activeAssistantIdx < 0 || m.activeAssistantIdx >= len(m.chatMessages) {
			m.chatMessages = append(m.chatMessages, ChatMessage{Role: "assistant", Content: ""})
			m.activeAssistantIdx = len(m.chatMessages) - 1
		}
		if m.chatMessages[m.activeAssistantIdx].Content == assistantPlaceholder {
			m.chatMessages[m.activeAssistantIdx].Content = msg.Text
		} else {
			m.chatMessages[m.activeAssistantIdx].Content += msg.Text
		}
		m.refreshChatViewport()
		return m, waitForStreamMsg(m.streamCh)

	case toolEventMsg:
		e := msg.Event
		switch e.Type {
		case core.ToolEventStart:
			if m.activeAssistantIdx >= 0 && m.activeAssistantIdx < len(m.chatMessages) && m.chatMessages[m.activeAssistantIdx].Content == assistantPlaceholder {
				m.chatMessages = append(m.chatMessages[:m.activeAssistantIdx], m.chatMessages[m.activeAssistantIdx+1:]...)
			}
			m.activeAssistantIdx = -1
			m.chatMessages = append(m.chatMessages, ChatMessage{
				Role:          "tool",
				ToolCallID:    e.CallID,
				ToolName:      e.ToolName,
				ToolState:     "running",
				Content:       "",
				IsPartial:     true,
				ToolStartedAt: time.Now(),
			})
			m.activeToolIndices[e.CallID] = len(m.chatMessages) - 1
			m.toolExpanded[e.CallID] = false
			m.refreshChatViewport()
			return m, tea.Batch(waitForStreamMsg(m.streamCh), toolTickCmd())
		case core.ToolEventUpdate:
			if idx, ok := m.activeToolIndices[e.CallID]; ok && idx >= 0 && idx < len(m.chatMessages) {
				m.chatMessages[idx].Content = e.Output
				m.chatMessages[idx].IsPartial = true
				m.chatMessages[idx].ToolState = "running"
			}
		case core.ToolEventEnd:
			if idx, ok := m.activeToolIndices[e.CallID]; ok && idx >= 0 && idx < len(m.chatMessages) {
				if strings.TrimSpace(e.Output) != "" {
					m.chatMessages[idx].Content = e.Output
				}
				m.chatMessages[idx].IsPartial = false
				m.chatMessages[idx].ToolEndedAt = time.Now()
				if e.IsError {
					m.chatMessages[idx].ToolState = "error"
					m.toolExpanded[e.CallID] = true
				} else {
					m.chatMessages[idx].ToolState = "done"
				}
				delete(m.activeToolIndices, e.CallID)
			}
		}
		m.refreshChatViewport()
		return m, waitForStreamMsg(m.streamCh)

	case toolTickMsg:
		if m.hasRunningTool() {
			m.refreshChatViewport()
			return m, toolTickCmd()
		}
		return m, nil

	case chatStreamDoneMsg:
		if m.activeAssistantIdx >= 0 && m.activeAssistantIdx < len(m.chatMessages) {
			if m.chatMessages[m.activeAssistantIdx].Content == assistantPlaceholder {
				m.chatMessages[m.activeAssistantIdx].Content = ""
			}
		}
		elapsed := time.Duration(0)
		if !m.streamStartedAt.IsZero() {
			elapsed = time.Since(m.streamStartedAt)
		}
		m.isWorking = false
		m.workingFrame = 0
		m.activeAssistantIdx = -1
		m.streamCh = nil
		m.streamStartedAt = time.Time{}
		m.firstChunkAt = time.Time{}
		m.streamChunkCount = 0
		m.streamCharCount = 0
		m.activeToolIndices = map[string]int{}
		if elapsed > 0 {
			m.msgs = append(m.msgs, fmt.Sprintf("[Chat] ✓ Done in %s", elapsed.Round(time.Millisecond)))
		} else {
			m.msgs = append(m.msgs, "[Chat] ✓ Done")
		}
		m.refreshChatViewport()
		return m, nil

	case chatStreamErrMsg:
		m.isWorking = false
		m.workingFrame = 0
		m.activeAssistantIdx = -1
		m.streamCh = nil
		m.streamStartedAt = time.Time{}
		m.firstChunkAt = time.Time{}
		m.streamChunkCount = 0
		m.streamCharCount = 0
		m.activeToolIndices = map[string]int{}
		m.msgs = append(m.msgs, "[Chat] ✗ "+msg.Err.Error())
		m.refreshChatViewport()
		return m, nil

	case bashCommandDoneMsg:
		m.isExecutingBash = false
		m.workingFrame = 0
		if msg.IsCD && msg.Err == nil {
			if err := os.Chdir(msg.NewCwd); err != nil {
				msg.Err = err
				msg.Output = "Failed to change directory: " + err.Error()
			} else {
				m.currentCwd = msg.NewCwd
				m.chatService = core.NewChatService(m.authStorage, core.NewToolSet(m.currentCwd))
			}
		}

		state := "done"
		if msg.Err != nil {
			state = "error"
		}
		m.chatMessages = append(m.chatMessages, ChatMessage{
			Role:          "tool",
			ToolName:      "bash",
			ToolState:     state,
			Content:       msg.Output,
			ToolStartedAt: msg.StartedAt,
			ToolEndedAt:   msg.EndedAt,
		})
		if msg.IsCD && msg.Err == nil {
			m.msgs = append(m.msgs, "[Bash] cwd: "+m.currentCwd)
		}
		m.refreshChatViewport()
		return m, nil

	case workingTickMsg:
		if m.isWorking || m.isExecutingBash {
			m.workingFrame = (m.workingFrame + 1) % 4
			m.refreshChatViewport()
			return m, workingTickCmd()
		}
		return m, nil

	case tea.MouseWheelMsg:
		if m.picker.IsActive() {
			return m, nil
		}
		var cmd tea.Cmd
		m.chatViewport, cmd = m.chatViewport.Update(msg)
		m.chatAutoScroll = m.chatViewport.AtBottom()
		return m, cmd

	case tea.KeyPressMsg:
		keyStr := msg.String()
		quitKey := m.getQuitKey()

		// ─── Command Mode Active ────────────────────────────────
		if m.picker.IsActive() {
			// Ctrl+Q exits command mode
			if keyStr == quitKey || keyStr == "ctrl+q" {
				m.clearCommandMode()
				return m, nil
			}

			// ESC goes back one level (or exits if at root)
			if keyStr == "esc" {
				if !m.picker.HandleBack() {
					// At root, exit command mode
					m.clearCommandMode()
				} else {
					m.recalculateLayout()
				}
				return m, nil
			}

			// Up/Down navigation
			if keyStr == "up" {
				m.picker.MoveUp()
				return m, nil
			}
			if keyStr == "down" {
				m.picker.MoveDown()
				return m, nil
			}

			// Enter selects the highlighted item
			if keyStr == "enter" {
				selected := m.picker.Selected()
				completed := m.picker.HandleSelect()
				if completed {
					// Command fully selected
					path := m.picker.Path()
					m.picker.Deactivate()
					return m, func() tea.Msg {
						return CommandActionMsg{Path: path}
					}
				}
				// Navigated deeper
				// Check if we need to load models for "set-model"
				if selected != nil && selected.ID == "set-model" {
					return m, m.loadModels()
				}
				// Clear filter for new level
				m.ta.SetValue(":")
				return m, nil
			}

			// All other keys go to the textarea for filtering
			// But first, check if user deleted the ':'
			if keyStr == "backspace" {
				value := m.ta.Value()
				if len(value) <= 1 {
					// Deleting the ':', exit command mode
					m.clearCommandMode()
					return m, nil
				}
			}

			// Pass to textarea
			var cmd tea.Cmd
			m.ta, cmd = m.ta.Update(msg)

			// Update filter based on new text
			m.picker.Filter(m.getFilterText())
			m.recalculateLayout()
			return m, cmd
		}

		// ─── Normal Mode ────────────────────────────────────────

		// Check for quit key (default: ctrl+c)
		if keyStr == quitKey {
			m.quit = true
			return m, tea.Quit
		}

		if keyStr == "esc" && m.inputMode == inputModeBash && strings.TrimSpace(m.ta.Value()) == "" {
			m.applyInputMode(inputModeChat)
			m.msgs = append(m.msgs, "[Bash] Mode disabled")
			return m, nil
		}

		// Toggle tool output expansion
		if keyStr == "ctrl+o" {
			if id, ok := m.lastToolCallID(); ok {
				m.toolExpanded[id] = !m.toolExpanded[id]
				m.refreshChatViewport()
			}
			return m, nil
		}

		// Chat scrolling
		if keyStr == "pgup" || keyStr == "pageup" || keyStr == "ctrl+up" {
			var cmd tea.Cmd
			m.chatViewport, cmd = m.chatViewport.Update(msg)
			m.chatAutoScroll = m.chatViewport.AtBottom()
			return m, cmd
		}
		if keyStr == "pgdown" || keyStr == "pagedown" || keyStr == "ctrl+down" {
			var cmd tea.Cmd
			m.chatViewport, cmd = m.chatViewport.Update(msg)
			m.chatAutoScroll = m.chatViewport.AtBottom()
			return m, cmd
		}

		// Check for ':' as first character to enter command mode
		if keyStr == ":" {
			value := m.ta.Value()
			if value == "" {
				// Enter command mode
				m.picker.Activate()
				m.ta.SetValue(":")
				m.recalculateLayout()
				return m, nil
			}
		}

		// Check for submit key (default: enter)
		submitKey := m.getSubmitKey()
		if keyStr == submitKey {
			if m.isWorking || m.isExecutingBash {
				return m, nil
			}

			text := strings.TrimSpace(m.ta.Value())
			if text == "" || strings.HasPrefix(text, ":") {
				return m, nil
			}

			if m.inputMode == inputModeBash {
				started := time.Now()
				m.chatMessages = append(m.chatMessages, ChatMessage{Role: "user", Content: "$ " + text})
				m.ta.SetValue("")
				m.isExecutingBash = true
				m.workingFrame = 0
				m.refreshChatViewport()
				return m, tea.Batch(m.executeBashCommand(text, started), workingTickCmd())
			}

			if m.selectedProvider == "" || m.selectedModelID == "" {
				m.msgs = append(m.msgs, "[Chat] Select a model first via :set-model")
				return m, nil
			}

			m.chatMessages = append(m.chatMessages, ChatMessage{Role: "user", Content: text})
			history := m.chatHistoryAsLLM()

			m.chatMessages = append(m.chatMessages, ChatMessage{Role: "assistant", Content: assistantPlaceholder})
			m.activeAssistantIdx = len(m.chatMessages) - 1
			m.isWorking = true
			m.chatAutoScroll = true
			m.activeToolIndices = map[string]int{}
			m.streamStartedAt = time.Now()
			m.firstChunkAt = time.Time{}
			m.streamChunkCount = 0
			m.streamCharCount = 0
			m.msgs = append(m.msgs, fmt.Sprintf("[Chat] Working with %s/%s...", m.selectedProvider, m.selectedModelID))
			m.ta.SetValue("")
			m.workingFrame = 0
			m.recalculateLayout()
			m.refreshChatViewport()

			cmd := m.startChatStream(history)
			return m, tea.Batch(cmd, workingTickCmd())
		}

		// Handle Shift+Enter for newline
		if msg.Code == tea.KeyEnter && msg.Mod.Contains(tea.ModShift) {
			m.ta.InsertRune('\n')
			m.recalculateLayout()
			return m, nil
		}

		// Handle Ctrl+M (which some terminals send for Enter with modifiers)
		if keyStr == "ctrl+m" || (msg.Code == 'm' && msg.Mod.Contains(tea.ModCtrl)) {
			return m, nil
		}
	}

	// Let the textarea process other keys
	var cmd tea.Cmd
	m.ta, cmd = m.ta.Update(msg)
	m.recalculateLayout()

	return m, cmd
}

func (m CodeAgentModel) View() tea.View {
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

	title := strings.TrimRight(m.styles.TitleStyle.Render("Synapta Code"), "\n")
	statusView := strings.TrimRight(m.renderStatusView(), "\n")
	pickerView := ""
	if m.picker.IsActive() {
		pickerView = strings.TrimRight(m.picker.View(max(m.width-6, 20)), "\n")
	}
	inputBox := strings.TrimRight(m.renderInputBox(), "\n")
	instructions := strings.TrimRight(m.renderInstructions(), "\n")

	var sections []string
	sections = append(sections, title)
	if statusView != "" {
		sections = append(sections, statusView)
	}
	sections = append(sections, "", m.chatViewport.View(), "")
	if pickerView != "" {
		sections = append(sections, pickerView)
	}
	sections = append(sections, inputBox, instructions)

	v := tea.NewView(strings.Join(sections, "\n"))
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	v.KeyboardEnhancements.ReportEventTypes = true
	return v
}

func countLines(s string) int {
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return 0
	}
	return strings.Count(s, "\n") + 1
}

func (m *CodeAgentModel) renderStatusView() string {
	start := 0
	if len(m.msgs) > 6 {
		start = len(m.msgs) - 6
	}
	lines := make([]string, 0, 8)
	for _, msg := range m.msgs[start:] {
		lines = append(lines, m.styles.MutedStyle.Render(msg))
	}

	if m.isWorking {
		spinner := []string{"⠋", "⠙", "⠹", "⠸"}[m.workingFrame%4]
		elapsed := time.Since(m.streamStartedAt).Round(time.Second)
		if m.streamStartedAt.IsZero() {
			elapsed = 0
		}
		status := fmt.Sprintf("[Chat] %s Working... %s", spinner, elapsed)
		lines = append(lines, m.styles.MutedStyle.Render(status))
	} else if m.isExecutingBash {
		spinner := []string{"⠋", "⠙", "⠹", "⠸"}[m.workingFrame%4]
		status := fmt.Sprintf("[Bash] %s Running command...", spinner)
		lines = append(lines, m.styles.MutedStyle.Render(status))
	}

	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n")
}

func (m *CodeAgentModel) renderInputBox() string {
	taView := m.ta.View()
	borderStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(m.borderColor)).
		Padding(0, 1)
	return borderStyle.Render(taView)
}

func (m *CodeAgentModel) renderInstructions() string {
	enterAction := "Enter send"
	if m.inputMode == inputModeBash {
		enterAction = "Enter run bash"
	}
	instructionsText := "PgUp/PgDn scroll  │  Ctrl+O toggle latest tool  │  Shift+Enter newline  │  " + enterAction + "  │  :=commands  │  Ctrl+C quit"
	if m.currentCwd != "" {
		instructionsText += "  │  cwd: " + m.currentCwd
	}
	if m.inputMode == inputModeChat && m.selectedModelName != "" {
		instructionsText += "  │  " + m.selectedModelName
	}
	return lipgloss.NewStyle().
		Foreground(m.styles.MutedStyle.GetForeground()).
		Width(m.width).
		Align(lipgloss.Center).
		Render(instructionsText)
}

func (m *CodeAgentModel) recalculateLayout() {
	if m.width < 1 || m.height < 1 {
		return
	}

	m.ta.SetWidth(max(m.width-6, 40))

	titleH := countLines(m.styles.TitleStyle.Render("Synapta Code"))
	statusH := countLines(m.renderStatusView())
	pickerH := 0
	if m.picker.IsActive() {
		pickerH = countLines(m.picker.View(max(m.width-6, 20)))
	}
	inputH := countLines(m.renderInputBox())
	instructionsH := countLines(m.renderInstructions())

	reserved := titleH + statusH + inputH + instructionsH + 2 // two spacer lines around chat
	if statusH > 0 {
		reserved++
	}
	if pickerH > 0 {
		reserved += pickerH
	}

	chatH := m.height - reserved
	if chatH < 3 {
		chatH = 3
	}

	m.chatViewport.SetWidth(max(m.width-4, 20))
	m.chatViewport.SetHeight(chatH)
}

func (m *CodeAgentModel) renderChatTranscript() string {
	if len(m.chatMessages) == 0 {
		return ""
	}
	lines := make([]string, 0, len(m.chatMessages))
	for _, msg := range m.chatMessages {
		switch msg.Role {
		case "user":
			lines = append(lines, strings.TrimRight(m.renderUserMessage(msg.Content), "\n"))
		case "assistant":
			lines = append(lines, strings.TrimRight(m.renderAssistantMessage(msg.Content), "\n"))
		case "tool":
			lines = append(lines, strings.TrimRight(m.renderToolMessage(msg), "\n"))
		}
	}
	return strings.Join(lines, "\n\n")
}

func (m *CodeAgentModel) refreshChatViewport() {
	m.recalculateLayout()
	m.chatViewport.SetContent(m.renderChatTranscript())
	if m.chatAutoScroll {
		m.chatViewport.GotoBottom()
	}
}

// renderUserMessage renders a user message with interaction highlight.
func (m *CodeAgentModel) renderUserMessage(content string) string {
	// Calculate available width for the message
	maxWidth := m.width - 8
	if maxWidth < 20 {
		maxWidth = 20
	}

	// Word wrap the content
	lines := wordWrap(content, maxWidth-4) // 4 for padding
	if len(lines) == 0 {
		lines = []string{""}
	}

	// Render each line with the interaction highlight style
	var rendered []string
	for _, line := range lines {
		rendered = append(rendered, line)
	}

	// Join lines and apply style
	joined := strings.Join(rendered, "\n")
	return m.styles.InteractionHighlightStyle.
		Width(maxWidth).
		Padding(0, 1).
		Render(joined)
}

func (m *CodeAgentModel) renderAssistantMessage(content string) string {
	maxWidth := m.width - 8
	if maxWidth < 20 {
		maxWidth = 20
	}

	lines := wordWrap(content, maxWidth-2)
	if len(lines) == 0 {
		lines = []string{""}
	}

	joined := strings.Join(lines, "\n")
	return lipgloss.NewStyle().
		Foreground(m.styles.CommandHighlightStyle.GetForeground()).
		Width(maxWidth).
		Render(joined)
}

func (m *CodeAgentModel) renderToolMessage(msg ChatMessage) string {
	maxWidth := m.width - 8
	if maxWidth < 20 {
		maxWidth = 20
	}

	state := msg.ToolState
	if state == "" {
		state = "running"
	}

	header := fmt.Sprintf("[%s] %s", strings.ToUpper(state), msg.ToolName)
	body := strings.TrimSpace(msg.Content)
	if body == "" {
		body = "(no output yet)"
	}

	body = m.styleToolBody(msg.ToolName, state, body)
	wrapped := wordWrap(body, maxWidth-4)
	if len(wrapped) == 0 {
		wrapped = []string{""}
	}

	expanded := msg.ToolCallID != "" && m.toolExpanded[msg.ToolCallID]
	if !expanded {
		previewLines := m.toolPreviewLines()
		if len(wrapped) > previewLines {
			hidden := len(wrapped) - previewLines
			if msg.ToolName == "write" || msg.ToolName == "read" {
				wrapped = wrapped[:previewLines]
				hint := m.styles.MutedStyle.Render(fmt.Sprintf("... (%d more lines, Ctrl+O to expand)", hidden))
				wrapped = append(wrapped, hint)
			} else {
				wrapped = wrapped[len(wrapped)-previewLines:]
				hint := m.styles.MutedStyle.Render(fmt.Sprintf("... (%d earlier lines, Ctrl+O to expand)", hidden))
				wrapped = append([]string{hint}, wrapped...)
			}
		}
	}

	body = strings.Join(wrapped, "\n")

	var footer string
	if !msg.ToolStartedAt.IsZero() {
		end := msg.ToolEndedAt
		label := "Took"
		if end.IsZero() {
			end = time.Now()
			label = "Elapsed"
		}
		footer = "\n" + m.styles.MutedStyle.Render(fmt.Sprintf("%s %s", label, end.Sub(msg.ToolStartedAt).Round(time.Second)))
	}

	color := m.styles.MutedStyle.GetForeground()
	if state == "error" {
		color = lipgloss.Color("9")
	}
	if state == "done" {
		color = m.styles.CommandHighlightStyle.GetForeground()
	}

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(color).
		Foreground(color).
		Padding(0, 1).
		Width(maxWidth)

	return box.Render(header + "\n" + body + footer)
}

func (m *CodeAgentModel) chatHistoryAsLLM() []llm.Message {
	messages := make([]llm.Message, 0, len(m.chatMessages))
	for _, msg := range m.chatMessages {
		if msg.Role != "user" && msg.Role != "assistant" {
			continue
		}
		if strings.TrimSpace(msg.Content) == "" || msg.Content == assistantPlaceholder {
			continue
		}
		messages = append(messages, llm.Message{Role: msg.Role, Content: msg.Content})
	}
	return messages
}

func (m *CodeAgentModel) startChatStream(history []llm.Message) tea.Cmd {
	if m.chatService == nil {
		return func() tea.Msg { return chatStreamErrMsg{Err: fmt.Errorf("chat service not available")} }
	}

	providerID := m.selectedProvider
	modelID := m.selectedModelID
	streamCh := make(chan tea.Msg, 256)
	m.streamCh = streamCh

	go func() {
		defer close(streamCh)
		err := m.chatService.Stream(
			context.Background(),
			providerID,
			modelID,
			history,
			func(text string) error {
				streamCh <- chatStreamChunkMsg{Text: text}
				return nil
			},
			func(event core.ToolEvent) error {
				streamCh <- toolEventMsg{Event: event}
				return nil
			},
		)
		if err != nil {
			streamCh <- chatStreamErrMsg{Err: err}
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

func (m *CodeAgentModel) hasRunningTool() bool {
	for _, msg := range m.chatMessages {
		if msg.Role == "tool" && msg.ToolState == "running" {
			return true
		}
	}
	return false
}

func (m *CodeAgentModel) lastToolCallID() (string, bool) {
	for i := len(m.chatMessages) - 1; i >= 0; i-- {
		msg := m.chatMessages[i]
		if msg.Role == "tool" && msg.ToolCallID != "" {
			return msg.ToolCallID, true
		}
	}
	return "", false
}

func (m *CodeAgentModel) toolPreviewLines() int {
	h := m.chatViewport.Height()
	if h <= 0 {
		return 10
	}
	lines := h / 3
	if lines < 5 {
		lines = 5
	}
	if lines > 20 {
		lines = 20
	}
	return lines
}

func (m *CodeAgentModel) styleToolBody(toolName, state, body string) string {
	if state == "error" {
		return lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Render(body)
	}
	switch toolName {
	case "write":
		if strings.Contains(strings.ToLower(body), "successfully wrote") {
			return lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Render(body)
		}
	case "read":
		lines := strings.Split(body, "\n")
		for i, line := range lines {
			trim := strings.TrimSpace(line)
			if strings.HasPrefix(trim, "[") && strings.HasSuffix(trim, "]") {
				lines[i] = lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Render(line)
			}
		}
		return strings.Join(lines, "\n")
	case "bash":
		if strings.Contains(body, "Full output:") {
			return lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Render(body)
		}
	}
	return body
}

func parseModelSelectionKey(key string) (providerID string, modelID string, ok bool) {
	parts := strings.SplitN(key, "::", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
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
		res, err := bashTool.Execute(context.Background(), tools.BashInput{Command: command}, nil)
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

func parseCDCommand(command string) (target string, ok bool) {
	trimmed := strings.TrimSpace(command)
	if trimmed == "cd" {
		return "~", true
	}
	if !strings.HasPrefix(trimmed, "cd ") {
		return "", false
	}
	target = strings.TrimSpace(strings.TrimPrefix(trimmed, "cd"))
	if target == "" {
		return "~", true
	}
	if strings.Contains(target, "&&") || strings.Contains(target, "||") || strings.ContainsAny(target, ";|><`") {
		return "", false
	}
	if (strings.HasPrefix(target, "\"") && strings.HasSuffix(target, "\"")) || (strings.HasPrefix(target, "'") && strings.HasSuffix(target, "'")) {
		target = target[1 : len(target)-1]
	}
	return target, true
}

func resolveCDTarget(baseCwd, target string) (string, error) {
	if strings.HasPrefix(target, "~") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		if target == "~" {
			target = home
		} else {
			target = filepath.Join(home, strings.TrimPrefix(target, "~/"))
		}
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(baseCwd, target)
	}
	resolved, err := filepath.Abs(filepath.Clean(target))
	if err != nil {
		return "", err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%s is not a directory", resolved)
	}
	return resolved, nil
}

func toolResultPlainText(result tools.Result) string {
	var b strings.Builder
	for _, c := range result.Content {
		if c.Type == tools.ContentPartText && c.Text != "" {
			if b.Len() > 0 {
				b.WriteString("\n")
			}
			b.WriteString(c.Text)
		}
	}
	return strings.TrimSpace(b.String())
}

// wordWrap wraps text to fit within the given width.
func wordWrap(text string, width int) []string {
	if width <= 0 || len(text) == 0 {
		return []string{text}
	}

	var lines []string
	for len(text) > width {
		// Find last space before width
		breakAt := width
		for i := width; i > 0; i-- {
			if text[i-1] == ' ' {
				breakAt = i
				break
			}
		}
		lines = append(lines, text[:breakAt])
		text = strings.TrimLeft(text[breakAt:], " ")
	}
	if len(text) > 0 {
		lines = append(lines, text)
	}
	return lines
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// ─── Model Loading ──────────────────────────────────────────────────

// loadModels loads available models from all connected providers.
func (m *CodeAgentModel) loadModels() tea.Cmd {
	return func() tea.Msg {
		if m.chatService == nil {
			return ModelsLoadedMsg{}
		}

		available, err := m.chatService.AvailableModels(context.Background())
		if err != nil {
			return ModelsLoadErrMsg{Err: err}
		}

		models := make([]ModelInfo, 0, len(available))
		for _, model := range available {
			models = append(models, ModelInfo{
				Provider: model.Provider,
				ID:       model.ID,
				Name:     model.Name,
			})
		}

		return ModelsLoadedMsg{Models: models}
	}
}

// ─── Kilo Gateway Authentication ────────────────────────────────────

// startKiloAuth initiates the Kilo Gateway OAuth flow.
func (m *CodeAgentModel) startKiloAuth() tea.Cmd {
	return func() tea.Msg {
		gateway := llm.NewKiloGateway()
		ctx, cancel := context.WithCancel(context.Background())
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

// openBrowser opens the specified URL in the default browser.
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
