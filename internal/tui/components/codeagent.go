package components

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"

	"github.com/synapta/synapta-cli/internal/config"
	"github.com/synapta/synapta-cli/internal/core"
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

// ChatMessage represents a message in the chat.
type ChatMessage struct {
	Role    string // "user" or "assistant"
	Content string
}

type chatStreamChunkMsg struct {
	Text string
}

type chatStreamDoneMsg struct{}

type chatStreamErrMsg struct {
	Err error
}

const assistantPlaceholder = "Working..."

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
	streamCh           <-chan tea.Msg
	chatService        *core.ChatService
	chatViewport       viewport.Model
	chatAutoScroll     bool
	streamStartedAt    time.Time
	firstChunkAt       time.Time
	streamChunkCount   int
	streamCharCount    int
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

	model := &CodeAgentModel{
		styles:             styles,
		ta:                 buildTextarea(t, cfg),
		borderColor:        t.Border,
		cfg:                cfg,
		picker:             NewCommandPicker(styles),
		authStorage:        authStorage,
		chatService:        core.NewChatService(authStorage),
		activeAssistantIdx: -1,
		chatViewport:       vp,
		chatAutoScroll:     true,
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
			if !m.streamStartedAt.IsZero() {
				m.msgs = append(m.msgs, fmt.Sprintf("[Chat dbg] first chunk in %s", m.firstChunkAt.Sub(m.streamStartedAt).Round(time.Millisecond)))
			}
		}
		m.streamChunkCount++
		m.streamCharCount += len(msg.Text)
		if m.activeAssistantIdx >= 0 && m.activeAssistantIdx < len(m.chatMessages) {
			if m.chatMessages[m.activeAssistantIdx].Content == assistantPlaceholder {
				m.chatMessages[m.activeAssistantIdx].Content = msg.Text
			} else {
				m.chatMessages[m.activeAssistantIdx].Content += msg.Text
			}
		}
		m.refreshChatViewport()
		return m, waitForStreamMsg(m.streamCh)

	case chatStreamDoneMsg:
		if m.activeAssistantIdx >= 0 && m.activeAssistantIdx < len(m.chatMessages) {
			if m.chatMessages[m.activeAssistantIdx].Content == assistantPlaceholder {
				m.chatMessages[m.activeAssistantIdx].Content = ""
			}
		}
		if !m.streamStartedAt.IsZero() {
			m.msgs = append(m.msgs, fmt.Sprintf("[Chat dbg] done in %s (%d chunks, %d chars)", time.Since(m.streamStartedAt).Round(time.Millisecond), m.streamChunkCount, m.streamCharCount))
		}
		m.isWorking = false
		m.activeAssistantIdx = -1
		m.streamCh = nil
		m.streamStartedAt = time.Time{}
		m.firstChunkAt = time.Time{}
		m.streamChunkCount = 0
		m.streamCharCount = 0
		m.refreshChatViewport()
		return m, nil

	case chatStreamErrMsg:
		if !m.streamStartedAt.IsZero() {
			m.msgs = append(m.msgs, fmt.Sprintf("[Chat dbg] error after %s (%d chunks, %d chars)", time.Since(m.streamStartedAt).Round(time.Millisecond), m.streamChunkCount, m.streamCharCount))
		}
		m.isWorking = false
		m.activeAssistantIdx = -1
		m.streamCh = nil
		m.streamStartedAt = time.Time{}
		m.firstChunkAt = time.Time{}
		m.streamChunkCount = 0
		m.streamCharCount = 0
		m.msgs = append(m.msgs, "[Chat] ✗ "+msg.Err.Error())
		m.refreshChatViewport()
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
			if m.isWorking {
				return m, nil
			}

			text := strings.TrimSpace(m.ta.Value())
			if text == "" || strings.HasPrefix(text, ":") {
				return m, nil
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
			m.streamStartedAt = time.Now()
			m.firstChunkAt = time.Time{}
			m.streamChunkCount = 0
			m.streamCharCount = 0
			m.msgs = append(m.msgs, fmt.Sprintf("[Chat dbg] start %s/%s", m.selectedProvider, m.selectedModelID))
			m.ta.SetValue("")
			m.recalculateLayout()
			m.refreshChatViewport()

			cmd := m.startChatStream(history)
			return m, cmd
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
	if len(m.msgs) == 0 {
		return ""
	}
	start := 0
	if len(m.msgs) > 6 {
		start = len(m.msgs) - 6
	}
	var lines []string
	for _, msg := range m.msgs[start:] {
		lines = append(lines, m.styles.MutedStyle.Render(msg))
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
	instructionsText := "PgUp/PgDn scroll  │  Shift+Enter newline  │  Enter send  │  :=commands  │  Ctrl+C quit"
	if m.selectedModelName != "" {
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

func (m *CodeAgentModel) chatHistoryAsLLM() []llm.Message {
	messages := make([]llm.Message, 0, len(m.chatMessages))
	for _, msg := range m.chatMessages {
		if strings.TrimSpace(msg.Content) == "" {
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
		err := m.chatService.Stream(context.Background(), providerID, modelID, history, func(text string) error {
			streamCh <- chatStreamChunkMsg{Text: text}
			return nil
		})
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

func parseModelSelectionKey(key string) (providerID string, modelID string, ok bool) {
	parts := strings.SplitN(key, "::", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
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
