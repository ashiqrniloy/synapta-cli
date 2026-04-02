package components

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"charm.land/bubbles/v2/textarea"

	"github.com/synapta/synapta-cli/internal/config"
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
	selectedModel string

	// Chat messages
	chatMessages []ChatMessage
	isWorking    bool
}

// NewCodeAgentModel creates the model using the loaded AppConfig.
func NewCodeAgentModel(cfg *config.AppConfig) *CodeAgentModel {
	t := cfg.ActiveTheme()
	styles := theme.NewStyles(t)

	// Initialize auth storage
	homeDir, _ := os.UserHomeDir()
	authDir := homeDir + "/.synapta"
	authStorage, _ := llm.NewAuthStorage(authDir)

	model := &CodeAgentModel{
		styles:      styles,
		ta:          buildTextarea(t, cfg),
		borderColor: t.Border,
		cfg:         cfg,
		picker:      NewCommandPicker(styles),
		authStorage: authStorage,
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
		m.ta.SetWidth(max(msg.Width-6, 40))
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
				// Model was selected
				m.selectedModel = msg.Path[1].Name
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
			return m, cmd
		}

		// ─── Normal Mode ────────────────────────────────────────

		// Check for quit key (default: ctrl+c)
		if keyStr == quitKey {
			m.quit = true
			return m, tea.Quit
		}

		// Check for ':' as first character to enter command mode
		if keyStr == ":" {
			value := m.ta.Value()
			if value == "" {
				// Enter command mode
				m.picker.Activate()
				m.ta.SetValue(":")
				return m, nil
			}
		}

		// Check for submit key (default: enter)
		submitKey := m.getSubmitKey()
		if keyStr == submitKey {
			// Only submit if we have text
			text := m.ta.Value()
			if text != "" && !strings.HasPrefix(text, ":") {
				// Add user message to chat
				m.chatMessages = append(m.chatMessages, ChatMessage{
					Role:    "user",
					Content: text,
				})
				m.isWorking = true
				m.ta.SetValue("")
			}
			return m, nil
		}

		// Handle Shift+Enter for newline
		if msg.Code == tea.KeyEnter && msg.Mod.Contains(tea.ModShift) {
			m.ta.InsertRune('\n')
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

	return m, cmd
}

func (m CodeAgentModel) View() tea.View {
	if m.quit {
		return tea.NewView("")
	}

	// ── Title ──
	title := m.styles.TitleStyle.Render("Synapta Code")

	// ── Status messages (recent ones) ──
	statusView := ""
	if len(m.msgs) > 0 {
		start := 0
		if len(m.msgs) > 2 {
			start = len(m.msgs) - 2
		}
		var msgLines []string
		for _, msg := range m.msgs[start:] {
			msgLines = append(msgLines, m.styles.MutedStyle.Render(msg))
		}
		statusView = strings.Join(msgLines, "\n")
	}

	// ── Chat messages (each separated by 1 blank row) ──
	var chatLines []string
	for _, msg := range m.chatMessages {
		if msg.Role == "user" {
			chatLines = append(chatLines, m.renderUserMessage(msg.Content))
		}
	}

	// ── Working indicator ──
	workingView := ""
	if m.isWorking {
		workingStyle := lipgloss.NewStyle().
			Foreground(m.styles.SuccessStyle.GetForeground()).
			Italic(true)
		workingView = workingStyle.Render("Working...")
	}

	// ── Input box with border ──
	taView := m.ta.View()
	borderStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(m.borderColor)).
		Padding(0, 1)
	inputBox := borderStyle.Render(taView)

	// ── Command picker (inline, above input) ──
	pickerView := ""
	if m.picker.IsActive() {
		pickerView = m.picker.View(m.width - 6)
	}

	// ── Instructions (centered, always last row) ──
	instructionsText := "↑/↓ navigate  │  Shift+Enter=newline  │  Enter=send  │  :=commands  │  Ctrl+C=quit"
	if m.selectedModel != "" {
		instructionsText = instructionsText + "  │  " + m.selectedModel
	}
	instructions := lipgloss.NewStyle().
		Foreground(m.styles.MutedStyle.GetForeground()).
		Width(m.width).
		Align(lipgloss.Center).
		Render(instructionsText)

	if m.height < 1 {
		v := tea.NewView("")
		v.AltScreen = true
		v.KeyboardEnhancements.ReportEventTypes = true
		return v
	}

	// Trim trailing newlines from lipgloss-rendered strings
	// (lipgloss adds them with Width/Align/Padding)
	title = strings.TrimRight(title, "\n")
	statusView = strings.TrimRight(statusView, "\n")
	for i, cl := range chatLines {
		chatLines[i] = strings.TrimRight(cl, "\n")
	}
	workingView = strings.TrimRight(workingView, "\n")
	inputBox = strings.TrimRight(inputBox, "\n")
	pickerView = strings.TrimRight(pickerView, "\n")

	// countLines returns the number of lines in a trimmed string
	countLines := func(s string) int {
		if s == "" {
			return 0
		}
		return strings.Count(s, "\n") + 1
	}

	// ── Compute section heights ──
	titleH := countLines(title)
	statusH := countLines(statusView)

	chatH := 0
	for _, cl := range chatLines {
		chatH += countLines(cl)
	}
	if len(chatLines) > 0 {
		chatH += len(chatLines) - 1 // 1 blank between messages
	}

	workingH := countLines(workingView)
	inputH := countLines(inputBox)
	pickerH := countLines(pickerView)

	// ── Bottom anchor zone ──
	// picker + 1 gap + input + 3 gaps + 1 instructions
	bottomZone := inputH
	if pickerH > 0 {
		bottomZone += 1 + pickerH
	}
	bottomZone += 3 + 1

	// ── Top zone: title + 2 blanks + status + (2 blanks) + chat + (1 blank) + working ──
	topZone := titleH + 2 // title + 2 blank rows after it
	if statusH > 0 {
		topZone += statusH
	}
	if chatH > 0 {
		if statusH > 0 {
			topZone += 2 // 2 blanks between status and first chat
		}
		topZone += chatH
		if workingH > 0 {
			topZone += 1 // 1 blank between last chat and working
		}
	}
	if workingH > 0 {
		topZone += workingH
	}

	spacerH := m.height - topZone - bottomZone
	if spacerH < 0 {
		spacerH = 0
	}

	// ── Build output ──
	var sb strings.Builder

	emitLines := func(s string) {
		if s == "" {
			return
		}
		for _, line := range strings.Split(s, "\n") {
			sb.WriteString(line)
			sb.WriteString("\n")
		}
	}

	// Title
	emitLines(title)
	sb.WriteString("\n") // 2nd blank row after title (total 2)

	// Status
	emitLines(statusView)

	// 2 blanks between status and first chat message
	if len(chatLines) > 0 && statusH > 0 {
		sb.WriteString("\n")
	}

	// Chat messages
	for i, cl := range chatLines {
		emitLines(cl)
		if i < len(chatLines)-1 {
			sb.WriteString("\n") // 1 blank between messages
		}
	}

	// 1 blank row between last chat message and working indicator
	if len(chatLines) > 0 && workingH > 0 {
		sb.WriteString("\n")
	}

	// Working indicator
	emitLines(workingView)

	// Spacer (pushes bottom elements toward screen bottom)
	for i := 0; i < spacerH; i++ {
		sb.WriteString("\n")
	}

	// Picker (command mode)
	if pickerH > 0 {
		emitLines(pickerView)
		sb.WriteString("\n") // blank row before input
	}

	// Input box
	emitLines(inputBox)

	// 3 blank rows between input and instructions
	sb.WriteString("\n\n\n")

	// Instructions — absolute last row, no trailing newline
	sb.WriteString(instructions)

	v := tea.NewView(sb.String())
	v.AltScreen = true
	v.KeyboardEnhancements.ReportEventTypes = true
	return v
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

// loadModels loads available models from authenticated providers.
func (m *CodeAgentModel) loadModels() tea.Cmd {
	return func() tea.Msg {
		var models []ModelInfo

		// Check if Kilo is authenticated
		if m.authStorage != nil && m.authStorage.HasAuth("kilo") {
			creds, err := m.authStorage.GetOAuthCredentials("kilo")
			if err == nil {
				gateway := llm.NewKiloGateway()
				kiloModels, err := gateway.FetchModels(creds.Access)
				if err == nil {
					for _, model := range kiloModels {
						models = append(models, ModelInfo{
							ID:   model.ID,
							Name: model.Name,
						})
					}
				}
			}
		}

		// If no authenticated providers, use defaults
		if len(models) == 0 {
			defaultModels := llm.KiloDefaultModels()
			for _, model := range defaultModels {
				models = append(models, ModelInfo{
					ID:   model.ID,
					Name: model.Name,
				})
			}
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
