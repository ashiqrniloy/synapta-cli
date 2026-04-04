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

type SessionsLoadedMsg struct {
	Sessions []core.SessionInfo
}

type SessionsLoadErrMsg struct {
	Err error
}

// ModelSelectedMsg is sent when a model is selected.
type ModelSelectedMsg struct {
	ModelID   string
	ModelName string
}

// ChatMessage represents a transcript entry in the chat.
type ChatMessage struct {
	Role          string // "user", "assistant", "tool", "system"
	Content       string
	SystemKind    string // "info", "working", "done", "error"
	ToolCallID    string
	ToolName      string
	ToolPath      string
	ToolCommand   string
	ToolState     string // "running", "done", "error"
	IsPartial     bool
	ToolStartedAt time.Time
	ToolEndedAt   time.Time
}

type ContextAction struct {
	At      time.Time
	Message string
}

type ContextEntry struct {
	Order           int
	ContextIndex    int
	Category        string
	Label           string
	Role            string
	Content         string
	HistoryIndex    int // index in SessionStore.ContextMessages()/filtered history
	RawHistoryIndex int // index in m.conversationHistory
	Editable        bool
	Removable       bool
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

type compactDoneMsg struct {
	Compacted bool
	History   []llm.Message
	Err       error
}

type newSessionDoneMsg struct {
	Store     *core.SessionStore
	SessionID string
	Err       error
}

type resumeSessionDoneMsg struct {
	Store *core.SessionStore
	Err   error
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

type providerBalanceMsg struct {
	ProviderID string
	Balance    string
	Err        error
}

type keybindingRow struct {
	Action      string
	Binding     string
	Description string
}

const (
	inputModeChat = "chat"
	inputModeBash = "bash"
)

// CodeAgentModel is the main TUI model for the Synapta Code agent.
type CodeAgentModel struct {
	width       int
	height      int
	styles      *theme.Styles
	ta          textarea.Model
	quit        bool
	borderColor string
	cfg         *config.AppConfig
	authStorage *llm.AuthStorage

	// Inline command / skill pickers
	picker      *CommandPicker
	skillPicker *SkillPicker

	availableSkills   []core.Skill
	skillCatalogCache *core.SkillCatalogCache

	// Selected model
	selectedModelName     string
	selectedModelID       string
	selectedProvider      string
	selectedThinkingLevel string
	providerBalance       string

	// Chat messages
	chatMessages          []ChatMessage
	isWorking             bool
	activeAssistantIdx    int
	activeSystemStatusIdx int
	activeToolIndices     map[string]int
	toolExpanded          map[string]bool
	streamCh              <-chan tea.Msg
	chatService           *core.ChatService
	systemPromptStore     *core.SystemPromptStore
	contextManager        *core.ContextManager
	sessionStore          *core.SessionStore
	agentDir              string
	conversationHistory   []llm.Message
	currentAssistantText  strings.Builder
	chatViewport          viewport.Model
	chatAutoScroll        bool
	streamStartedAt       time.Time
	firstChunkAt          time.Time
	streamChunkCount      int
	streamCharCount       int

	inputMode        string
	currentCwd       string
	isExecutingBash  bool
	workingFrame     int
	chatPaneWidth    int
	contextPaneWidth int

	contextActions          []ContextAction
	contextModalOpen        bool
	contextModalEditMode    bool
	contextModalSelection   int
	contextModalEntries     []ContextEntry
	contextModalEditor      textarea.Model
	contextModalEditorHint  string
	contextOverrideActive   bool
	keybindingsModalOpen    bool
	keybindingsSearch       string
	keybindingsSelection    int
	lastPromptHash          string
	lastPromptFingerprint   core.PromptFingerprint
	likelyPromptCacheHit    bool
	promptBuildCount        int
	stablePrefixChangeCount int
}

// NewCodeAgentModel creates the model using the loaded AppConfig.
func NewCodeAgentModel(cfg *config.AppConfig) *CodeAgentModel {
	t := cfg.ActiveTheme()
	styles := theme.NewStyles(t)

	// Initialize auth and system prompt storage
	homeDir, _ := os.UserHomeDir()
	agentDir := homeDir + "/.synapta"
	authStorage, _ := llm.NewAuthStorage(agentDir)
	systemPromptStore := core.NewSystemPromptStore(agentDir)
	_ = systemPromptStore.EnsureDefaultIfAgentDirMissing(core.AgentCode, core.DefaultCodeSystemPrompt)

	vp := viewport.New(viewport.WithWidth(80), viewport.WithHeight(10))
	vp.SoftWrap = true
	vp.FillHeight = true

	cwd, _ := os.Getwd()
	toolset := core.NewToolSet(cwd)

	sessionStore, _ := core.NewSessionStore(agentDir, core.AgentCode, cwd, core.DefaultCompactionSettings())
	conversationHistory := make([]llm.Message, 0)
	if sessionStore != nil {
		conversationHistory = sessionStore.ContextMessages()
	}

	skillCatalogCache := core.NewSkillCatalogCache()

	model := &CodeAgentModel{
		styles:                styles,
		ta:                    buildTextarea(t, cfg),
		borderColor:           t.Border,
		cfg:                   cfg,
		picker:                NewCommandPicker(styles),
		skillPicker:           NewSkillPicker(styles),
		skillCatalogCache:     skillCatalogCache,
		authStorage:           authStorage,
		chatService:           core.NewChatService(authStorage, toolset),
		systemPromptStore:     systemPromptStore,
		contextManager:        core.NewContextManager(core.AgentCode, agentDir, cwd, systemPromptStore),
		sessionStore:          sessionStore,
		agentDir:              agentDir,
		conversationHistory:   conversationHistory,
		activeAssistantIdx:    -1,
		activeSystemStatusIdx: -1,
		activeToolIndices:     map[string]int{},
		toolExpanded:          map[string]bool{},
		chatViewport:          vp,
		chatAutoScroll:        true,
		inputMode:             inputModeChat,
		currentCwd:            cwd,
	}

	if cfg.Provider.Default != "" && cfg.Provider.Model != "" {
		model.selectedProvider = cfg.Provider.Default
		model.selectedModelID = cfg.Provider.Model
		model.selectedModelName = cfg.Provider.Model
		model.selectedThinkingLevel = inferThinkingLevel(cfg.Provider.Model, cfg.Provider.Model)
	}

	model.rebuildTranscriptFromHistory()
	model.reloadAvailableSkills()
	model.recordContextAction("System prompt loaded")

	// Check if already authenticated and show model count
	if authStorage != nil && authStorage.HasAuth("kilo") {
		model.appendSystemMessage("[Kilo] ✓ Authenticated", "done")
	}
	model.contextModalEditor = buildTextarea(t, cfg)
	model.contextModalEditor.Placeholder = "Edit selected context (Ctrl+S to save, Esc to cancel)"

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

func (m *CodeAgentModel) getContextKey() string {
	if m.cfg != nil && m.cfg.Keybindings.Context != "" {
		return normalizeKeyName(m.cfg.Keybindings.Context)
	}
	return "ctrl+k"
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
	m.ta.Placeholder = "Command mode… type to filter"
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
		m.ta.Placeholder = "Type your message... (Enter=send, Shift+Enter=newline)"
	}
}

func (m *CodeAgentModel) reloadAvailableSkills() {
	opts := core.LoadSkillsOptions{
		CWD:             m.currentCwd,
		AgentDir:        m.agentDir,
		IncludeDefaults: true,
	}
	if m.skillCatalogCache != nil {
		result := m.skillCatalogCache.Load(opts)
		m.availableSkills = result.Skills
	} else {
		result := core.LoadSkills(opts)
		m.availableSkills = result.Skills
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
	prefix := text[:idx]
	return prefix + "@" + skillName + " "
}

func (m *CodeAgentModel) recordContextAction(message string) {
	m.contextActions = append(m.contextActions, ContextAction{At: time.Now(), Message: message})
	if len(m.contextActions) > 200 {
		m.contextActions = m.contextActions[len(m.contextActions)-200:]
	}
}

func (m *CodeAgentModel) openContextModal() {
	m.contextModalOpen = true
	m.contextModalEditMode = false
	m.contextModalSelection = 0
	m.contextModalEntries = m.buildContextEntries()
}

func (m *CodeAgentModel) closeContextModal() {
	m.contextModalOpen = false
	m.contextModalEditMode = false
	m.contextModalEntries = nil
	m.contextModalEditorHint = ""
}

func (m *CodeAgentModel) buildContextEntries() []ContextEntry {
	if m.contextManager == nil {
		return nil
	}
	msgs, err := m.contextManager.Build(m.conversationHistory)
	if err != nil {
		return nil
	}
	historyIdx := make([]int, 0)
	for i, msg := range m.conversationHistory {
		if (msg.Role == "user" || msg.Role == "assistant" || msg.Role == "tool") && strings.TrimSpace(msg.Content) != "" {
			historyIdx = append(historyIdx, i)
		}
	}
	entries := make([]ContextEntry, 0, len(msgs))
	hPos := 0
	for i, msg := range msgs {
		category := categorizeContextMessage(msg)
		entry := ContextEntry{
			Order:           i + 1,
			ContextIndex:    i,
			Role:            msg.Role,
			Content:         strings.TrimSpace(msg.Content),
			HistoryIndex:    -1,
			RawHistoryIndex: -1,
			Category:        category,
			Label:           contextEntryLabel(msg, category),
		}
		if msg.Role != "system" && hPos < len(historyIdx) {
			entry.HistoryIndex = hPos
			entry.RawHistoryIndex = historyIdx[hPos]
			entry.Editable = true
			entry.Removable = true
			hPos++
		}
		if entry.Category == "compacted-output" {
			entry.Editable = false
			entry.Removable = false
		}
		entries = append(entries, entry)
	}
	return entries
}

func categorizeContextMessage(msg llm.Message) string {
	content := strings.TrimSpace(msg.Content)
	if msg.Role == "system" {
		return "system-prompt"
	}
	if strings.HasPrefix(content, "The conversation history before this point was compacted") {
		return "compacted-output"
	}
	if strings.Contains(content, "<skill name=") {
		return "skills"
	}
	if msg.Role == "tool" {
		switch strings.ToLower(strings.TrimSpace(msg.Name)) {
		case "read":
			return "files-read"
		case "write":
			return "files-written"
		case "bash":
			return "tool-bash"
		default:
			return "tool-output"
		}
	}
	if msg.Role == "assistant" {
		return "llm-output"
	}
	if msg.Role == "user" {
		return "user-input"
	}
	return "context"
}

func contextEntryLabel(msg llm.Message, category string) string {
	content := strings.TrimSpace(msg.Content)
	switch category {
	case "user-input":
		return "User Input"
	case "llm-output":
		return "LLM Output"
	case "compacted-output":
		return "Compacted Summary"
	case "skills":
		if name := extractBetween(content, "<skill name=\"", "\""); name != "" {
			return "Skill: " + name
		}
		return "Skill"
	case "files-read":
		if p := extractAfterPrefixLine(content, "Path: "); p != "" {
			return "Tool Output (File Read): " + p
		}
		return "Tool Output (File Read)"
	case "files-written":
		if p := extractAfterPrefixLine(content, "Path: "); p != "" {
			return "Tool Output (File Write): " + p
		}
		return "Tool Output (File Write)"
	case "tool-bash":
		if cmd := extractAfterPrefixLine(content, "Command: "); cmd != "" {
			return "Tool Output (Bash): " + cmd
		}
		return "Tool Output (Bash)"
	case "tool-output":
		return "Tool Output"
	case "system-prompt":
		return "System Prompt"
	default:
		return "Context"
	}
}

func extractAfterPrefixLine(text, prefix string) string {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(line, prefix))
		}
	}
	return ""
}

func extractBetween(text, start, end string) string {
	i := strings.Index(text, start)
	if i < 0 {
		return ""
	}
	s := text[i+len(start):]
	j := strings.Index(s, end)
	if j < 0 {
		return ""
	}
	return strings.TrimSpace(s[:j])
}

func (m *CodeAgentModel) applyContextEntryEdit(entry ContextEntry, content string) {
	if entry.HistoryIndex < 0 {
		return
	}
	if m.sessionStore != nil && !m.contextOverrideActive {
		if err := m.sessionStore.UpdateContextMessageAt(entry.HistoryIndex, content); err == nil {
			m.conversationHistory = m.sessionStore.ContextMessages()
			m.rebuildTranscriptFromHistory()
			m.recordContextAction(fmt.Sprintf("Context edited: #%d %s", entry.Order, entry.Category))
			return
		}
	}
	if entry.RawHistoryIndex < 0 || entry.RawHistoryIndex >= len(m.conversationHistory) {
		return
	}
	m.conversationHistory[entry.RawHistoryIndex].Content = content
	m.contextOverrideActive = true
	m.rebuildTranscriptFromHistory()
	m.recordContextAction(fmt.Sprintf("Context edited (session-local): #%d %s", entry.Order, entry.Category))
}

func (m *CodeAgentModel) removeContextEntry(entry ContextEntry) {
	if entry.HistoryIndex < 0 {
		return
	}
	if m.sessionStore != nil && !m.contextOverrideActive {
		if err := m.sessionStore.RemoveContextMessageAt(entry.HistoryIndex); err == nil {
			m.conversationHistory = m.sessionStore.ContextMessages()
			m.rebuildTranscriptFromHistory()
			m.recordContextAction(fmt.Sprintf("Context removed: #%d %s", entry.Order, entry.Category))
			return
		}
	}
	if entry.RawHistoryIndex < 0 || entry.RawHistoryIndex >= len(m.conversationHistory) {
		return
	}
	m.conversationHistory = append(m.conversationHistory[:entry.RawHistoryIndex], m.conversationHistory[entry.RawHistoryIndex+1:]...)
	m.contextOverrideActive = true
	m.rebuildTranscriptFromHistory()
	m.recordContextAction(fmt.Sprintf("Context removed (session-local): #%d %s", entry.Order, entry.Category))
}

// ─── tea.Model ───────────────────────────────────────────────────────

func (m *CodeAgentModel) Init() tea.Cmd {
	if m.selectedProvider == "kilo" {
		return tea.Batch(textarea.Blink, m.fetchProviderBalanceCmd("kilo"))
	}
	return textarea.Blink
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
			m.appendSystemMessage("[Bash] Mode enabled. Enter a command and press Enter.", "info")
			return m, nil
		case "add-provider":
			m.clearCommandMode()
			if len(msg.Path) > 1 {
				providerID := msg.Path[1].ID
				if providerID == "kilo" {
					// Start Kilo Gateway OAuth flow
					m.appendSystemMessage("[Kilo] Starting authentication...", "working")
					return m, m.startKiloAuth()
				}
				m.appendSystemMessage("[Add Provider] Selected: "+msg.Path[1].Name, "info")
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
				if m.cfg != nil {
					if err := m.cfg.SetProvider(providerID, modelID); err != nil {
						m.appendSystemMessage("[Model] Warning: failed to save config: "+err.Error(), "error")
					}
				}
				m.clearCommandMode()
				m.appendSystemMessage("[Model] Selected: "+msg.Path[1].Name, "done")
				m.providerBalance = ""
				if providerID == "kilo" {
					return m, m.fetchProviderBalanceCmd("kilo")
				}
			} else {
				// Need to load models first
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

	case ModelsLoadedMsg:
		if len(msg.Models) == 0 {
			m.appendSystemMessage("[Model] No models available. Add a provider first.", "info")
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
		m.appendSystemMessage("[Model] ✗ "+msg.Err.Error(), "error")
		m.clearCommandMode()
		return m, nil

	case SessionsLoadedMsg:
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
		m.ta.SetValue(":")
		m.recalculateLayout()
		return m, nil

	case SessionsLoadErrMsg:
		m.appendSystemMessage("[Session] ✗ "+msg.Err.Error(), "error")
		m.clearCommandMode()
		return m, nil

	case KiloAuthProgressMsg:
		m.appendSystemMessage("[Kilo] "+string(msg), "info")
		return m, nil

	case providerBalanceMsg:
		if msg.ProviderID == m.selectedProvider {
			if msg.Err != nil {
				m.providerBalance = ""
			} else {
				m.providerBalance = msg.Balance
			}
		}
		return m, nil

	case KiloAuthCompleteMsg:
		if msg.Err != nil {
			// Split error message into multiple lines for readability
			errLines := strings.Split(msg.Err.Error(), "\n")
			m.appendSystemMessage("[Kilo] ✗ Authentication failed:", "error")
			for _, line := range errLines {
				m.appendSystemMessage("[Kilo]   "+line, "error")
			}
			return m, nil
		}
		m.appendSystemMessage("[Kilo] ✓ Authentication successful! "+msg.Email, "done")
		m.appendSystemMessage("[Kilo] ✓ "+fmt.Sprintf("%d models available", msg.ModelCount), "done")
		return m, m.fetchProviderBalanceCmd("kilo")

	case chatStreamChunkMsg:
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
		m.refreshChatViewport()
		return m, waitForStreamMsg(m.streamCh)

	case toolEventMsg:
		e := msg.Event
		switch e.Type {
		case core.ToolEventStart:
			m.activeAssistantIdx = -1
			idx := m.appendChatMessage(ChatMessage{
				Role:          "tool",
				ToolCallID:    e.CallID,
				ToolName:      e.ToolName,
				ToolPath:      e.Path,
				ToolCommand:   e.Command,
				ToolState:     "running",
				Content:       "",
				IsPartial:     true,
				ToolStartedAt: time.Now(),
			})
			m.activeToolIndices[e.CallID] = idx
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
				if strings.TrimSpace(e.Path) != "" {
					m.chatMessages[idx].ToolPath = e.Path
				}
				if strings.TrimSpace(e.Command) != "" {
					m.chatMessages[idx].ToolCommand = e.Command
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
			toolCtx := llm.Message{Role: "tool", Name: e.ToolName, ToolCallID: e.CallID, Content: formatToolContextContent(e)}
			m.conversationHistory = append(m.conversationHistory, toolCtx)
			if m.sessionStore != nil {
				_ = m.sessionStore.AppendMessage(toolCtx)
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
		elapsed := time.Duration(0)
		if !m.streamStartedAt.IsZero() {
			elapsed = time.Since(m.streamStartedAt)
		}
		m.isWorking = false
		m.workingFrame = 0
		assistantText := strings.TrimSpace(m.currentAssistantText.String())
		if assistantText != "" {
			assistantMsg := llm.Message{Role: "assistant", Content: assistantText}
			m.conversationHistory = append(m.conversationHistory, assistantMsg)
			if m.sessionStore != nil {
				_ = m.sessionStore.AppendMessage(assistantMsg)
			}
		}
		m.currentAssistantText.Reset()
		m.activeAssistantIdx = -1
		m.streamCh = nil
		m.streamStartedAt = time.Time{}
		m.firstChunkAt = time.Time{}
		m.streamChunkCount = 0
		m.streamCharCount = 0
		m.activeToolIndices = map[string]int{}
		if elapsed > 0 {
			m.finalizeWorkingSystemMessage(fmt.Sprintf("[Chat] ✓ Done in %s", elapsed.Round(time.Millisecond)), "done")
		} else {
			m.finalizeWorkingSystemMessage("[Chat] ✓ Done", "done")
		}
		m.refreshChatViewport()
		return m, nil

	case chatStreamErrMsg:
		m.isWorking = false
		m.workingFrame = 0
		m.currentAssistantText.Reset()
		m.activeAssistantIdx = -1
		m.streamCh = nil
		m.streamStartedAt = time.Time{}
		m.firstChunkAt = time.Time{}
		m.streamChunkCount = 0
		m.streamCharCount = 0
		m.activeToolIndices = map[string]int{}
		m.finalizeWorkingSystemMessage("[Chat] ✗ "+msg.Err.Error(), "error")
		m.refreshChatViewport()
		return m, nil

	case compactDoneMsg:
		if msg.Err != nil {
			m.appendSystemMessage("[Compact] ✗ "+msg.Err.Error(), "error")
			return m, nil
		}
		if msg.History != nil {
			m.conversationHistory = append([]llm.Message(nil), msg.History...)
		}
		if msg.Compacted {
			m.appendSystemMessage("[Compact] ✓ Session compacted", "done")
			m.recordContextAction("Compaction applied")
		} else {
			m.appendSystemMessage("[Compact] No compaction needed", "info")
		}
		m.refreshChatViewport()
		return m, nil

	case newSessionDoneMsg:
		if msg.Err != nil {
			m.appendSystemMessage("[Session] ✗ "+msg.Err.Error(), "error")
			return m, nil
		}
		m.sessionStore = msg.Store
		m.conversationHistory = m.sessionStore.ContextMessages()
		m.currentAssistantText.Reset()
		m.rebuildTranscriptFromHistory()
		m.appendSystemMessage("[Session] ✓ New session started ("+msg.SessionID+")", "done")
		m.recordContextAction("New session started")
		return m, nil

	case resumeSessionDoneMsg:
		if msg.Err != nil {
			m.appendSystemMessage("[Session] ✗ "+msg.Err.Error(), "error")
			return m, nil
		}
		m.sessionStore = msg.Store
		sessionCWD := m.sessionStore.CWD()
		if strings.TrimSpace(sessionCWD) != "" {
			m.currentCwd = sessionCWD
			m.chatService = core.NewChatService(m.authStorage, core.NewToolSet(m.currentCwd))
		}
		if m.contextManager != nil {
			m.contextManager.SetCWD(m.currentCwd)
		}
		m.reloadAvailableSkills()
		m.conversationHistory = m.sessionStore.ContextMessages()
		m.currentAssistantText.Reset()
		m.rebuildTranscriptFromHistory()
		m.appendSystemMessage("[Session] ✓ Session resumed ("+m.sessionStore.SessionID()+")", "done")
		m.recordContextAction("Session resumed")
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
				if m.contextManager != nil {
					m.contextManager.SetCWD(m.currentCwd)
				}
				m.reloadAvailableSkills()
			}
		}

		state := "done"
		if msg.Err != nil {
			state = "error"
		}
		if msg.Err != nil {
			m.finalizeWorkingSystemMessage("[Bash] ✗ Command failed", "error")
		} else {
			m.finalizeWorkingSystemMessage("[Bash] ✓ Command finished", "done")
		}
		m.appendChatMessage(ChatMessage{
			Role:          "tool",
			ToolName:      "bash",
			ToolCommand:   msg.Command,
			ToolState:     state,
			Content:       msg.Output,
			ToolStartedAt: msg.StartedAt,
			ToolEndedAt:   msg.EndedAt,
		})
		// User-invoked bash mode output is intentionally not added to LLM context history.
		if msg.IsCD && msg.Err == nil {
			m.appendSystemMessage("[Bash] cwd: "+m.currentCwd, "info")
		}
		m.refreshChatViewport()
		return m, nil

	case workingTickMsg:
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
		contextKey := m.getContextKey()
		helpKey := m.getHelpKey()

		if m.keybindingsModalOpen {
			if keyStr == "esc" || keyStr == helpKey {
				m.keybindingsModalOpen = false
				return m, nil
			}
			if keyStr == "up" && m.keybindingsSelection > 0 {
				m.keybindingsSelection--
				return m, nil
			}
			if keyStr == "down" {
				rows := m.filteredKeybindingRows()
				if m.keybindingsSelection < len(rows)-1 {
					m.keybindingsSelection++
				}
				return m, nil
			}
			if keyStr == "pgup" || keyStr == "pageup" {
				m.keybindingsSelection -= 8
				if m.keybindingsSelection < 0 {
					m.keybindingsSelection = 0
				}
				return m, nil
			}
			if keyStr == "pgdown" || keyStr == "pagedown" {
				rows := m.filteredKeybindingRows()
				m.keybindingsSelection += 8
				if m.keybindingsSelection >= len(rows) {
					m.keybindingsSelection = max(len(rows)-1, 0)
				}
				return m, nil
			}
			if keyStr == "backspace" {
				r := []rune(m.keybindingsSearch)
				if len(r) > 0 {
					m.keybindingsSearch = string(r[:len(r)-1])
					m.keybindingsSelection = 0
				}
				return m, nil
			}
			if len([]rune(keyStr)) == 1 && !msg.Mod.Contains(tea.ModCtrl) && !msg.Mod.Contains(tea.ModAlt) {
				m.keybindingsSearch += keyStr
				m.keybindingsSelection = 0
				return m, nil
			}
			return m, nil
		}

		if m.contextModalOpen {
			if keyStr == "esc" {
				if m.contextModalEditMode {
					m.contextModalEditMode = false
					m.contextModalEditorHint = ""
				} else {
					m.closeContextModal()
				}
				return m, nil
			}
			if m.contextModalEditMode {
				if keyStr == "ctrl+s" {
					if m.contextModalSelection >= 0 && m.contextModalSelection < len(m.contextModalEntries) {
						entry := m.contextModalEntries[m.contextModalSelection]
						m.applyContextEntryEdit(entry, strings.TrimSpace(m.contextModalEditor.Value()))
						m.contextModalEntries = m.buildContextEntries()
						m.contextModalEditMode = false
						m.contextModalEditorHint = "Saved"
					}
					return m, nil
				}
				var cmd tea.Cmd
				m.contextModalEditor, cmd = m.contextModalEditor.Update(msg)
				return m, cmd
			}

			if keyStr == "up" && m.contextModalSelection > 0 {
				m.contextModalSelection--
				return m, nil
			}
			if keyStr == "down" && m.contextModalSelection < len(m.contextModalEntries)-1 {
				m.contextModalSelection++
				return m, nil
			}
			if keyStr == "d" || keyStr == "backspace" {
				if m.contextModalSelection >= 0 && m.contextModalSelection < len(m.contextModalEntries) {
					entry := m.contextModalEntries[m.contextModalSelection]
					if entry.Removable {
						m.removeContextEntry(entry)
						m.contextModalEntries = m.buildContextEntries()
						if m.contextModalSelection >= len(m.contextModalEntries) && m.contextModalSelection > 0 {
							m.contextModalSelection--
						}
					}
				}
				return m, nil
			}
			if keyStr == "e" || keyStr == "enter" {
				if m.contextModalSelection >= 0 && m.contextModalSelection < len(m.contextModalEntries) {
					entry := m.contextModalEntries[m.contextModalSelection]
					if entry.Editable {
						m.contextModalEditMode = true
						m.contextModalEditor.SetValue(entry.Content)
						m.contextModalEditor.CursorEnd()
						m.contextModalEditorHint = ""
					}
				}
				return m, nil
			}
			if keyStr == "c" {
				m.recordContextAction("Manual compaction triggered from context manager")
				m.closeContextModal()
				m.appendSystemMessage("[Compact] Running manual compaction...", "working")
				return m, m.manualCompactCmd()
			}
			return m, nil
		}

		if keyStr == helpKey {
			m.keybindingsModalOpen = true
			m.keybindingsSearch = ""
			m.keybindingsSelection = 0
			return m, nil
		}

		if keyStr == contextKey {
			m.openContextModal()
			return m, nil
		}

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
				// Check if we need to load dynamic items.
				if selected != nil && selected.ID == "set-model" {
					return m, m.loadModels()
				}
				if selected != nil && selected.ID == "resume-session" {
					return m, m.loadSessions()
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

		// ─── Skill Picker Active ───────────────────────────────
		if m.skillPicker != nil && m.skillPicker.IsActive() {
			if keyStr == "esc" {
				m.skillPicker.Deactivate()
				m.recalculateLayout()
				return m, nil
			}
			if keyStr == "up" {
				m.skillPicker.MoveUp()
				return m, nil
			}
			if keyStr == "down" {
				m.skillPicker.MoveDown()
				return m, nil
			}
			if keyStr == "enter" {
				selected := m.skillPicker.Selected()
				if selected != nil {
					m.ta.SetValue(replaceActiveSkillMention(m.ta.Value(), selected.Name))
				}
				m.skillPicker.Deactivate()
				m.recalculateLayout()
				return m, nil
			}

			var cmd tea.Cmd
			m.ta, cmd = m.ta.Update(msg)
			m.updateSkillPickerFromInput()
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
			m.appendSystemMessage("[Bash] Mode disabled", "info")
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

		// Enter command mode via configured key or ':' prefix.
		if keyStr == m.getCommandKey() {
			m.enterCommandMode()
			return m, nil
		}
		if keyStr == ":" {
			value := m.ta.Value()
			if value == "" {
				m.enterCommandMode()
				return m, nil
			}
		}

		if keyStr == "@" && m.inputMode == inputModeChat && m.skillPicker != nil && len(m.availableSkills) > 0 {
			var cmd tea.Cmd
			m.ta, cmd = m.ta.Update(msg)
			m.activateSkillPicker()
			m.updateSkillPickerFromInput()
			m.recalculateLayout()
			return m, cmd
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

			expandedText := text
			usedSkills := []core.Skill{}
			if m.inputMode == inputModeChat {
				var err error
				expandedText, usedSkills, err = core.ExpandSkillReferencesWithCache(text, m.availableSkills, m.skillCatalogCache)
				if err != nil {
					m.appendSystemMessage("[Skills] ✗ "+err.Error(), "error")
					return m, nil
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
				return m, tea.Batch(m.executeBashCommand(text, started), workingTickCmd())
			}

			if m.selectedProvider == "" || m.selectedModelID == "" {
				m.appendSystemMessage("[Chat] Select a model first via :set-model", "error")
				return m, nil
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
				m.appendSystemMessage("[Chat] ✗ Failed to build context: "+err.Error(), "error")
				return m, nil
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

	chatView := lipgloss.NewStyle().Width(m.chatPaneWidth).Render(m.chatViewport.View())
	contextView := m.renderContextPane(m.chatViewport.Height())
	middle := lipgloss.JoinHorizontal(lipgloss.Top, chatView, " ", contextView)

	divider := lipgloss.NewStyle().Foreground(m.styles.MutedStyle.GetForeground()).Render(strings.Repeat("─", max(m.width, 1)))
	var sections []string
	sections = append(sections, header, divider, middle)
	if !m.isCompactDensity() {
		sections = append(sections, "")
	}
	sections = append(sections, inputBox)
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

	v := tea.NewView(base)
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

func (m *CodeAgentModel) renderHeaderBar() string {
	title := m.styles.TitleStyle.Render("SYNAPTA CODE")
	titleW := lipgloss.Width(title)
	if titleW >= m.width {
		return lipgloss.NewStyle().Width(m.width).Align(lipgloss.Center).Render(truncateLine(title, m.width))
	}

	provider := m.providerDisplayLabel()
	if m.providerBalance != "" && m.selectedProvider == "kilo" {
		provider += " · " + m.providerBalance
	}
	cwd := "cwd: " + m.currentCwd

	remaining := m.width - titleW
	leftW := remaining / 2
	rightW := remaining - leftW

	left := lipgloss.NewStyle().
		Width(leftW).
		Align(lipgloss.Left).
		Foreground(m.styles.MutedStyle.GetForeground()).
		Render(truncateLine(provider, leftW))
	center := lipgloss.NewStyle().
		Width(titleW).
		Align(lipgloss.Center).
		Render(title)
	right := lipgloss.NewStyle().
		Width(rightW).
		Align(lipgloss.Right).
		Foreground(m.styles.MutedStyle.GetForeground()).
		Render(truncateLine(cwd, rightW))

	return lipgloss.JoinHorizontal(lipgloss.Top, left, center, right)
}

func (m *CodeAgentModel) renderInputBox() string {
	taView := strings.TrimRight(m.ta.View(), "\n")
	innerWidth := max(m.width-4, 20)
	if m.picker.IsActive() {
		taView = lipgloss.NewStyle().Foreground(m.styles.MutedStyle.GetForeground()).Render(taView)
		pickerView := strings.TrimRight(m.picker.View(innerWidth), "\n")
		if pickerView != "" {
			divider := lipgloss.NewStyle().Foreground(m.styles.MutedStyle.GetForeground()).Render(strings.Repeat("─", max(innerWidth, 1)))
			taView += "\n" + divider + "\n" + pickerView
		}
	} else if m.skillPicker != nil && m.skillPicker.IsActive() {
		taView = lipgloss.NewStyle().Foreground(m.styles.MutedStyle.GetForeground()).Render(taView)
		pickerView := strings.TrimRight(m.skillPicker.View(innerWidth), "\n")
		if pickerView != "" {
			divider := lipgloss.NewStyle().Foreground(m.styles.MutedStyle.GetForeground()).Render(strings.Repeat("─", max(innerWidth, 1)))
			taView += "\n" + divider + "\n" + pickerView
		}
	}
	borderStyle := lipgloss.NewStyle().
		Width(m.width).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(m.borderColor)).
		Padding(0, 1)
	return borderStyle.Render(taView)
}

func (m *CodeAgentModel) renderModelFooter() string {
	if m.selectedModelName == "" {
		return ""
	}
	thinking := m.selectedThinkingLevel
	if thinking == "" {
		thinking = inferThinkingLevel(m.selectedModelID, m.selectedModelName)
	}
	text := fmt.Sprintf("%s  •  thinking: %s", m.selectedModelName, thinking)
	return lipgloss.NewStyle().
		Foreground(m.styles.MutedStyle.GetForeground()).
		Width(m.width).
		Align(lipgloss.Right).
		Render(text)
}

func (m *CodeAgentModel) renderContextPane(height int) string {
	width := max(m.contextPaneWidth, 20)
	innerWidth := max(width-4, 10)
	innerHeight := max(height-2, 8)
	actionsRatio := 24
	skillsRatio := 16
	if m.isCompactDensity() {
		actionsRatio = 20
		skillsRatio = 12
	}
	actionsH := max((innerHeight*actionsRatio)/100, 3)
	skillsH := max((innerHeight*skillsRatio)/100, 3)
	contextH := max(innerHeight-actionsH-skillsH-6, 4)

	actionLines := make([]string, 0, actionsH)
	for i := len(m.contextActions) - 1; i >= 0 && len(actionLines) < actionsH; i-- {
		a := m.contextActions[i]
		line := fmt.Sprintf("%s  %s", a.At.Format("15:04:05"), a.Message)
		actionLines = append([]string{truncateLine(line, innerWidth)}, actionLines...)
	}
	for len(actionLines) < actionsH {
		actionLines = append(actionLines, "")
	}

	skillLines := make([]string, 0, skillsH)
	for _, s := range m.availableSkills {
		skillLines = append(skillLines, truncateLine("@"+s.Name+" — "+s.Description, innerWidth))
		if len(skillLines) >= skillsH {
			break
		}
	}
	for len(skillLines) < skillsH {
		skillLines = append(skillLines, "")
	}

	entries := m.buildContextEntries()
	contextLines := make([]string, 0, contextH)
	for _, e := range entries {
		contextLines = append(contextLines, m.renderContextPreviewLine(e, innerWidth))
		if len(contextLines) >= contextH {
			break
		}
	}
	for len(contextLines) < contextH {
		contextLines = append(contextLines, "")
	}

	muted := lipgloss.NewStyle().Foreground(m.styles.MutedStyle.GetForeground())
	sep := muted.Render(strings.Repeat("─", max(innerWidth, 1)))
	lines := []string{lipgloss.NewStyle().Bold(true).Render("Context")}
	lines = append(lines, muted.Render("Actions"))
	lines = append(lines, actionLines...)
	lines = append(lines, sep)
	lines = append(lines, muted.Render("Known skills"))
	lines = append(lines, skillLines...)
	lines = append(lines, sep)
	lines = append(lines, muted.Render("Current context"))
	lines = append(lines, contextLines...)

	content := strings.Join(lines, "\n")
	return lipgloss.NewStyle().
		Width(width).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(m.borderColor)).
		Padding(0, 1).
		Render(content)
}

func (m *CodeAgentModel) renderContextPreviewLine(e ContextEntry, width int) string {
	badge := renderContextBadge(e.Category)
	_, badgeLabel := contextBadgeLabel(e.Category)
	prefix := fmt.Sprintf("%2d ", e.Order)
	headPlain := fmt.Sprintf("%2d %s %s — ", e.Order, badgeLabel, e.Label)
	remaining := width - lipgloss.Width(headPlain)
	if remaining < 0 {
		remaining = 0
	}
	preview := strings.ReplaceAll(strings.TrimSpace(e.Content), "\n", " ")
	preview = truncateLine(preview, remaining)
	line := prefix + badge + " " + e.Label + " — " + preview
	return truncateLine(line, width)
}

func (m *CodeAgentModel) renderContextDiagnostics(width int, height int) []string {
	fp := m.lastPromptFingerprint
	lines := []string{}
	if fp.PromptHash == "" {
		lines = append(lines, "Prompt: (not built yet)")
	} else {
		cacheState := "miss"
		if m.promptBuildCount <= 1 {
			cacheState = "cold-start"
		} else if m.likelyPromptCacheHit {
			cacheState = "hit-candidate"
		}
		lines = append(lines, truncateLine("Prompt hash: "+shortHash(fp.PromptHash), width))
		lines = append(lines, truncateLine("Stable: "+shortHash(fp.StablePrefixHash), width))
		lines = append(lines, truncateLine("History: "+shortHash(fp.HistoryHash), width))
		lines = append(lines, truncateLine(fmt.Sprintf("Cache: %s  builds:%d  stableΔ:%d", cacheState, m.promptBuildCount, m.stablePrefixChangeCount), width))
		lines = append(lines, truncateLine(fmt.Sprintf("Messages: %d", fp.MessageCount), width))
	}
	for len(lines) < height {
		lines = append(lines, "")
	}
	if len(lines) > height {
		lines = lines[:height]
	}
	return lines
}

func shortHash(s string) string {
	if len(s) <= 12 {
		return s
	}
	return s[:12]
}

func (m *CodeAgentModel) renderContextModal() string {
	width := m.width
	height := m.height

	if m.contextModalEntries == nil {
		m.contextModalEntries = m.buildContextEntries()
	}

	var body string
	if m.contextModalEditMode {
		m.contextModalEditor.SetWidth(max(width-8, 40))
		m.contextModalEditor.SetHeight(max(height-8, 8))
		body = m.contextModalEditor.View()
	} else {
		if m.contextModalSelection < 0 {
			m.contextModalSelection = 0
		}
		if m.contextModalSelection >= len(m.contextModalEntries) && len(m.contextModalEntries) > 0 {
			m.contextModalSelection = len(m.contextModalEntries) - 1
		}

		leftW := max((width-10)*45/100, 30)
		rightW := max((width-10)-leftW, 30)
		innerH := max(height-8, 8)

		listLines := []string{lipgloss.NewStyle().Bold(true).Render("Context Entries")}
		for i, e := range m.contextModalEntries {
			prefix := "  "
			if i == m.contextModalSelection {
				prefix = "▸ "
			}
			line := m.renderContextPreviewLine(e, leftW-4)
			listLines = append(listLines, truncateLine(prefix+line, leftW-2))
		}
		listContent := strings.Join(limitLines(listLines, innerH), "\n")
		leftPane := lipgloss.NewStyle().
			Width(leftW).
			Height(innerH).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color(m.borderColor)).
			Padding(0, 1).
			Render(listContent)

		diag := m.renderContextDiagnostics(rightW-4, 5)
		previewLines := []string{lipgloss.NewStyle().Bold(true).Render("Diagnostics")}
		previewLines = append(previewLines, diag...)
		previewLines = append(previewLines, "", lipgloss.NewStyle().Bold(true).Render("Selected Entry"))
		if len(m.contextModalEntries) > 0 && m.contextModalSelection >= 0 && m.contextModalSelection < len(m.contextModalEntries) {
			e := m.contextModalEntries[m.contextModalSelection]
			previewLines = append(previewLines,
				fmt.Sprintf("#%d  %s", e.Order, renderContextBadge(e.Category)),
				lipgloss.NewStyle().Foreground(m.styles.MutedStyle.GetForeground()).Render("Role: "+e.Role),
				"",
			)
			wrapped := wordWrap(strings.TrimSpace(e.Content), max(rightW-4, 20))
			previewLines = append(previewLines, wrapped...)
		} else {
			previewLines = append(previewLines, lipgloss.NewStyle().Foreground(m.styles.MutedStyle.GetForeground()).Render("No context entries"))
		}
		previewContent := strings.Join(limitLines(previewLines, innerH), "\n")
		rightPane := lipgloss.NewStyle().
			Width(rightW).
			Height(innerH).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(m.styles.CommandHighlightStyle.GetForeground()).
			Padding(0, 1).
			Render(previewContent)

		head := "Context Manager"
		foot := "↑↓ select  Enter/E edit  D remove  C compact  Esc close"
		body = head + "\n\n" + lipgloss.JoinHorizontal(lipgloss.Top, leftPane, " ", rightPane) + "\n\n" + foot
	}
	if m.contextModalEditorHint != "" {
		body += "\n\n" + m.contextModalEditorHint
	}
	modal := lipgloss.NewStyle().
		Width(width).
		Height(height).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(m.styles.CommandHighlightStyle.GetForeground()).
		Padding(1, 2).
		Render(body)
	return modal
}

func contextBadgeLabel(category string) (string, string) {
	bg := "240"
	label := category
	switch category {
	case "system-prompt":
		bg, label = "61", "System"
	case "skills":
		bg, label = "99", "Skill"
	case "compacted-output":
		bg, label = "172", "Compacted"
	case "files-read":
		bg, label = "31", "Tool:Read"
	case "files-written":
		bg, label = "29", "Tool:Write"
	case "tool-bash":
		bg, label = "130", "Tool:Bash"
	case "llm-output":
		bg, label = "64", "LLM"
	case "user-input":
		bg, label = "95", "User"
	case "tool-output":
		bg, label = "67", "Tool"
	}
	return bg, label
}

func (m *CodeAgentModel) providerDisplayLabel() string {
	switch m.selectedProvider {
	case "kilo":
		return "Kilo Gateway"
	case "github-copilot":
		return "GitHub Copilot"
	case "":
		return "No provider"
	default:
		return m.selectedProvider
	}
}

func inferThinkingLevel(modelID, modelName string) string {
	id := strings.ToLower(modelID + " " + modelName)
	if strings.Contains(id, "thinking") || strings.Contains(id, "reason") || strings.Contains(id, "r1") || strings.Contains(id, "o1") || strings.Contains(id, "o3") {
		return "reasoning"
	}
	return "standard"
}

func (m *CodeAgentModel) densityMode() string {
	if m.cfg != nil {
		d := strings.ToLower(strings.TrimSpace(m.cfg.UI.Density))
		if d == "compact" || d == "comfortable" {
			return d
		}
	}
	return "comfortable"
}

func (m *CodeAgentModel) isCompactDensity() bool {
	return m.densityMode() == "compact"
}

func (m *CodeAgentModel) keybindingRows() []keybindingRow {
	newline := "shift+enter"
	if m.cfg != nil && m.cfg.Keybindings.Newline != "" {
		newline = normalizeKeyName(m.cfg.Keybindings.Newline)
	}
	return []keybindingRow{
		{Action: "Submit", Binding: m.getSubmitKey(), Description: "Send message / run bash"},
		{Action: "Newline", Binding: newline, Description: "Insert newline in input"},
		{Action: "Command palette", Binding: m.getCommandKey(), Description: "Open command picker"},
		{Action: "Context manager", Binding: m.getContextKey(), Description: "Open context modal"},
		{Action: "Keybindings help", Binding: m.getHelpKey(), Description: "Open keybindings modal"},
		{Action: "Toggle tool expansion", Binding: "ctrl+o", Description: "Expand/collapse latest tool block"},
		{Action: "Skill picker", Binding: "@", Description: "Open skills suggestions"},
		{Action: "Quit", Binding: m.getQuitKey(), Description: "Exit Synapta Code"},
	}
}

func (m *CodeAgentModel) filteredKeybindingRows() []keybindingRow {
	rows := m.keybindingRows()
	q := strings.ToLower(strings.TrimSpace(m.keybindingsSearch))
	if q == "" {
		return rows
	}
	filtered := make([]keybindingRow, 0, len(rows))
	for _, r := range rows {
		h := strings.ToLower(r.Action + " " + r.Binding + " " + r.Description)
		if strings.Contains(h, q) {
			filtered = append(filtered, r)
		}
	}
	return filtered
}

func (m *CodeAgentModel) renderKeybindingsModal() string {
	rows := m.filteredKeybindingRows()
	if m.keybindingsSelection >= len(rows) {
		m.keybindingsSelection = max(len(rows)-1, 0)
	}

	width := max(m.width-8, 60)
	height := max(m.height-6, 16)
	listH := max(height-8, 8)

	lines := []string{lipgloss.NewStyle().Bold(true).Render("Keybindings")}
	search := m.keybindingsSearch
	if search == "" {
		search = "type to search..."
	}
	lines = append(lines, m.styles.MutedStyle.Render("Search: "+search), "")

	start := 0
	if m.keybindingsSelection >= listH {
		start = m.keybindingsSelection - listH + 1
	}
	end := min(start+listH, len(rows))
	if len(rows) == 0 {
		lines = append(lines, m.styles.MutedStyle.Render("No keybindings match search."))
	} else {
		for i := start; i < end; i++ {
			r := rows[i]
			line := fmt.Sprintf("%-18s  %-10s  %s", r.Action, r.Binding, r.Description)
			if i == m.keybindingsSelection {
				lines = append(lines, m.styles.CommandHighlightStyle.Render(truncateLine("▸ "+line, width-6)))
			} else {
				lines = append(lines, truncateLine("  "+line, width-6))
			}
		}
	}
	lines = append(lines, "", m.styles.MutedStyle.Render("↑↓ navigate  •  PgUp/PgDn scroll  •  type to filter  •  Backspace delete  •  Esc close"))

	body := strings.Join(lines, "\n")
	return lipgloss.NewStyle().
		Width(m.width).
		Height(m.height).
		AlignHorizontal(lipgloss.Center).
		AlignVertical(lipgloss.Center).
		Render(lipgloss.NewStyle().
			Width(width).
			Height(height).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(m.styles.CommandHighlightStyle.GetForeground()).
			Padding(1, 2).
			Render(body))
}

func renderContextBadge(category string) string {
	bg, label := contextBadgeLabel(category)
	return lipgloss.NewStyle().Foreground(lipgloss.Color("255")).Background(lipgloss.Color(bg)).Bold(true).Padding(0, 1).Render(label)
}

func limitLines(lines []string, maxLines int) []string {
	if maxLines <= 0 {
		return []string{}
	}
	if len(lines) <= maxLines {
		return lines
	}
	if maxLines == 1 {
		return []string{truncateLine(lines[0], 1)}
	}
	out := append([]string{}, lines[:maxLines-1]...)
	out = append(out, lipgloss.NewStyle().Foreground(lipgloss.Color("244")).Render("…"))
	return out
}

func truncateLine(s string, maxLen int) string {
	if maxLen <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= maxLen {
		return s
	}
	return lipgloss.NewStyle().MaxWidth(maxLen).Render(s)
}

func (m *CodeAgentModel) recalculateLayout() {
	if m.width < 1 || m.height < 1 {
		return
	}

	m.contextPaneWidth = max((m.width*40)/100, 32)
	if m.contextPaneWidth > m.width-30 {
		m.contextPaneWidth = max(m.width-30, 20)
	}
	m.chatPaneWidth = max(m.width-m.contextPaneWidth, 20)

	m.ta.SetWidth(max(m.width-4, 20))

	inputH := countLines(m.renderInputBox())
	footerH := countLines(m.renderModelFooter())

	spacers := 2
	if m.isCompactDensity() {
		spacers = 1
	}
	reserved := 2 + inputH + footerH + spacers // header + spacers

	chatH := m.height - reserved
	if chatH < 3 {
		chatH = 3
	}

	m.chatViewport.SetWidth(max(m.chatPaneWidth, 20))
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
		case "system":
			lines = append(lines, strings.TrimRight(m.renderSystemMessage(msg), "\n"))
		}
	}
	sep := "\n\n"
	if m.isCompactDensity() {
		sep = "\n"
	}
	return strings.Join(lines, sep)
}

func (m *CodeAgentModel) refreshChatViewport() {
	m.recalculateLayout()
	m.chatViewport.SetContent(m.renderChatTranscript())
	if m.chatAutoScroll {
		m.chatViewport.GotoBottom()
	}
}

func (m *CodeAgentModel) appendChatMessage(msg ChatMessage) int {
	insertAt := len(m.chatMessages)
	if m.activeSystemStatusIdx >= 0 && m.activeSystemStatusIdx < len(m.chatMessages) {
		insertAt = m.activeSystemStatusIdx
		m.chatMessages = append(m.chatMessages, ChatMessage{})
		copy(m.chatMessages[insertAt+1:], m.chatMessages[insertAt:])
		m.chatMessages[insertAt] = msg
		m.activeSystemStatusIdx++
		if m.activeAssistantIdx >= insertAt {
			m.activeAssistantIdx++
		}
		for id, idx := range m.activeToolIndices {
			if idx >= insertAt {
				m.activeToolIndices[id] = idx + 1
			}
		}
	} else {
		m.chatMessages = append(m.chatMessages, msg)
	}
	m.chatAutoScroll = true
	return insertAt
}

func (m *CodeAgentModel) appendSystemMessage(content, kind string) {
	m.appendChatMessage(ChatMessage{Role: "system", Content: content, SystemKind: kind})
	m.refreshChatViewport()
}

func (m *CodeAgentModel) setWorkingSystemMessage(content string) {
	if m.activeSystemStatusIdx >= 0 && m.activeSystemStatusIdx < len(m.chatMessages) {
		m.chatMessages[m.activeSystemStatusIdx].Content = content
		m.chatMessages[m.activeSystemStatusIdx].SystemKind = "working"
		return
	}
	m.activeSystemStatusIdx = len(m.chatMessages)
	m.chatMessages = append(m.chatMessages, ChatMessage{Role: "system", Content: content, SystemKind: "working"})
	m.chatAutoScroll = true
}

func (m *CodeAgentModel) updateWorkingSystemMessage(content string) {
	if m.activeSystemStatusIdx >= 0 && m.activeSystemStatusIdx < len(m.chatMessages) {
		m.chatMessages[m.activeSystemStatusIdx].Content = content
		m.chatMessages[m.activeSystemStatusIdx].SystemKind = "working"
	}
}

func (m *CodeAgentModel) finalizeWorkingSystemMessage(content, kind string) {
	if m.activeSystemStatusIdx >= 0 && m.activeSystemStatusIdx < len(m.chatMessages) {
		m.chatMessages[m.activeSystemStatusIdx].Content = content
		m.chatMessages[m.activeSystemStatusIdx].SystemKind = kind
		m.activeSystemStatusIdx = -1
		m.chatAutoScroll = true
		return
	}
	m.appendSystemMessage(content, kind)
}

func (m *CodeAgentModel) chatWorkingStatusText() string {
	spinner := []string{"⠋", "⠙", "⠹", "⠸"}[m.workingFrame%4]
	elapsed := time.Since(m.streamStartedAt).Round(time.Second)
	if m.streamStartedAt.IsZero() {
		elapsed = 0
	}
	return fmt.Sprintf("[Chat] %s Working with %s/%s... %s", spinner, m.selectedProvider, m.selectedModelID, elapsed)
}

func (m *CodeAgentModel) bashWorkingStatusText() string {
	spinner := []string{"⠋", "⠙", "⠹", "⠸"}[m.workingFrame%4]
	return fmt.Sprintf("[Bash] %s Running command...", spinner)
}

// renderUserMessage renders a user message with interaction highlight.
func (m *CodeAgentModel) renderUserMessage(content string) string {
	maxWidth := max(m.chatViewport.Width(), 20)
	label := lipgloss.NewStyle().Foreground(m.styles.MutedStyle.GetForeground()).Bold(true).Render("You")
	lines := wordWrap(content, maxWidth-2)
	if len(lines) == 0 {
		lines = []string{""}
	}
	padY := 0
	if !m.isCompactDensity() {
		padY = 1
	}
	return m.styles.InteractionHighlightStyle.
		Width(maxWidth).
		Padding(padY, 1).
		Render(label + "\n" + strings.Join(lines, "\n"))
}

func (m *CodeAgentModel) renderAssistantMessage(content string) string {
	maxWidth := max(m.chatViewport.Width(), 20)
	label := lipgloss.NewStyle().Foreground(m.styles.MutedStyle.GetForeground()).Bold(true).Render("Assistant")
	lines := wordWrap(content, maxWidth)
	if len(lines) == 0 {
		lines = []string{""}
	}
	body := lipgloss.NewStyle().
		Foreground(m.styles.CommandHighlightStyle.GetForeground()).
		Width(maxWidth).
		Render(strings.Join(lines, "\n"))
	return label + "\n" + body
}

func (m *CodeAgentModel) renderSystemMessage(msg ChatMessage) string {
	maxWidth := max(m.chatViewport.Width(), 20)
	lines := wordWrap(strings.TrimSpace(msg.Content), maxWidth-4)
	if len(lines) == 0 {
		lines = []string{""}
	}

	t := m.cfg.ActiveTheme()
	fg := lipgloss.Color(t.Foreground)
	bg := lipgloss.Color(t.SystemMessageColor)
	prefix := "ℹ"
	switch msg.SystemKind {
	case "error":
		prefix = "✗"
		bg = lipgloss.Color(t.Error)
	case "done":
		prefix = "✓"
		bg = lipgloss.Color(t.Success)
	case "working":
		prefix = "…"
		bg = lipgloss.Color(t.Primary)
	}

	label := lipgloss.NewStyle().Bold(true).Foreground(fg).Render("System " + prefix)
	content := label + "\n" + strings.Join(lines, "\n")
	return lipgloss.NewStyle().
		Width(maxWidth).
		Foreground(fg).
		Background(bg).
		Padding(0, 1).
		Render(content)
}

func (m *CodeAgentModel) renderToolMessage(msg ChatMessage) string {
	maxWidth := max(m.chatViewport.Width(), 20)
	state := msg.ToolState
	if state == "" {
		state = "running"
	}

	stateColor := m.styles.MutedStyle.GetForeground()
	stateIcon := "●"
	switch state {
	case "error":
		stateColor = lipgloss.Color("9")
		stateIcon = "✗"
	case "done":
		stateColor = m.styles.SuccessStyle.GetForeground()
		stateIcon = "✓"
	case "running":
		stateColor = m.styles.CommandHighlightStyle.GetForeground()
		stateIcon = "…"
	}
	header := lipgloss.NewStyle().Foreground(stateColor).Bold(true).Render(fmt.Sprintf("%s %s", stateIcon, strings.ToUpper(state)))
	meta := []string{m.styles.MutedStyle.Render("tool: " + msg.ToolName)}
	if strings.TrimSpace(msg.ToolPath) != "" {
		meta = append(meta, m.styles.MutedStyle.Render("path: "+msg.ToolPath))
	}
	if msg.ToolName == "bash" && strings.TrimSpace(msg.ToolCommand) != "" {
		meta = append(meta, m.styles.MutedStyle.Render("command: "+msg.ToolCommand))
	}

	body := strings.TrimSpace(msg.Content)
	if body == "" {
		body = "(no output yet)"
	}
	body = m.styleToolBody(msg.ToolName, state, body)
	wrapped := wordWrap(body, maxWidth-2)
	if len(wrapped) == 0 {
		wrapped = []string{""}
	}

	if msg.ToolName == "read" {
		if len(wrapped) > 10 {
			hidden := len(wrapped) - 10
			wrapped = append(wrapped[:10], m.styles.MutedStyle.Render(fmt.Sprintf("... (%d more lines)", hidden)))
		}
	} else {
		expanded := msg.ToolCallID != "" && m.toolExpanded[msg.ToolCallID]
		if !expanded {
			previewLines := m.toolPreviewLines()
			if len(wrapped) > previewLines {
				hidden := len(wrapped) - previewLines
				if msg.ToolName == "write" {
					wrapped = wrapped[:previewLines]
					wrapped = append(wrapped, m.styles.MutedStyle.Render(fmt.Sprintf("... (%d more lines, Ctrl+O to expand)", hidden)))
				} else {
					wrapped = wrapped[len(wrapped)-previewLines:]
					wrapped = append([]string{m.styles.MutedStyle.Render(fmt.Sprintf("... (%d earlier lines, Ctrl+O to expand)", hidden))}, wrapped...)
				}
			}
		}
	}

	blockLines := []string{header}
	blockLines = append(blockLines, meta...)
	blockLines = append(blockLines, strings.Join(wrapped, "\n"))

	if !msg.ToolStartedAt.IsZero() {
		end := msg.ToolEndedAt
		label := "Took"
		if end.IsZero() {
			end = time.Now()
			label = "Elapsed"
		}
		blockLines = append(blockLines, m.styles.MutedStyle.Render(fmt.Sprintf("%s %s", label, end.Sub(msg.ToolStartedAt).Round(time.Second))))
	}

	color := stateColor

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(color).
		Padding(0, 1).
		Width(maxWidth)

	return box.Render(strings.Join(blockLines, "\n"))
}

func formatToolContextContent(e core.ToolEvent) string {
	var b strings.Builder
	b.WriteString("Tool: ")
	b.WriteString(strings.TrimSpace(e.ToolName))
	b.WriteString("\n")
	if strings.TrimSpace(e.Path) != "" {
		b.WriteString("Path: ")
		b.WriteString(strings.TrimSpace(e.Path))
		b.WriteString("\n")
	}
	if strings.TrimSpace(e.Command) != "" {
		b.WriteString("Command: ")
		b.WriteString(strings.TrimSpace(e.Command))
		b.WriteString("\n")
	}
	b.WriteString("State: ")
	if e.IsError {
		b.WriteString("error\n")
	} else {
		b.WriteString("done\n")
	}
	b.WriteString("\nOutput:\n")
	if strings.TrimSpace(e.Output) == "" {
		b.WriteString("(no output)")
	} else {
		b.WriteString(strings.TrimSpace(e.Output))
	}
	return b.String()
}

func (m *CodeAgentModel) chatHistoryAsLLM() ([]llm.Message, error) {
	baseHistory := m.conversationHistory
	if m.sessionStore != nil && !m.contextOverrideActive {
		contextWindow := 128000
		if m.chatService != nil && m.selectedProvider != "" && m.selectedModelID != "" {
			if cw, err := m.chatService.ModelContextWindow(context.Background(), m.selectedProvider, m.selectedModelID); err == nil && cw > 0 {
				contextWindow = cw
			}
		}
		summarizer := func(ctx context.Context, toSummarize []llm.Message, previousSummary string) (string, error) {
			if m.chatService == nil || m.selectedProvider == "" || m.selectedModelID == "" {
				return "", nil
			}
			return m.chatService.SummarizeCompaction(ctx, m.selectedProvider, m.selectedModelID, toSummarize, previousSummary)
		}
		compacted, err := m.sessionStore.CompactIfNeeded(context.Background(), contextWindow, summarizer)
		if err != nil {
			return nil, err
		}
		if compacted {
			m.recordContextAction("Auto compaction applied")
		}
		baseHistory = m.sessionStore.ContextMessages()
		m.conversationHistory = append([]llm.Message(nil), baseHistory...)
	}
	if m.contextManager == nil {
		return append([]llm.Message(nil), baseHistory...), nil
	}
	msgs, err := m.contextManager.Build(baseHistory)
	if err != nil {
		return nil, err
	}
	fp := m.contextManager.LastPromptFingerprint()
	if fp.PromptHash != "" {
		m.promptBuildCount++
		m.likelyPromptCacheHit = m.lastPromptFingerprint.StablePrefixHash != "" && fp.StablePrefixHash == m.lastPromptFingerprint.StablePrefixHash
		if m.lastPromptFingerprint.StablePrefixHash != "" && fp.StablePrefixHash != m.lastPromptFingerprint.StablePrefixHash {
			m.stablePrefixChangeCount++
		}
		if fp.PromptHash != m.lastPromptHash {
			m.lastPromptHash = fp.PromptHash
			m.recordContextAction(fmt.Sprintf("Prompt fingerprint updated: %s", fp.PromptHash[:12]))
		}
		m.lastPromptFingerprint = fp
	}
	return msgs, nil
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

func (m *CodeAgentModel) fetchProviderBalanceCmd(providerID string) tea.Cmd {
	return func() tea.Msg {
		if providerID != "kilo" || m.authStorage == nil {
			return providerBalanceMsg{ProviderID: providerID}
		}
		creds, err := m.authStorage.GetOAuthCredentials("kilo")
		if err != nil || creds == nil || strings.TrimSpace(creds.Access) == "" {
			return providerBalanceMsg{ProviderID: providerID}
		}
		gateway := llm.NewKiloGateway()
		balance, err := gateway.FetchBalance(creds.Access)
		if err != nil {
			return providerBalanceMsg{ProviderID: providerID, Err: err}
		}
		return providerBalanceMsg{ProviderID: providerID, Balance: llm.FormatBalance(balance)}
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

// ─── Model Loading / Session Commands ───────────────────────────────

func (m *CodeAgentModel) rebuildTranscriptFromHistory() {
	messages := make([]ChatMessage, 0, len(m.conversationHistory))
	for _, msg := range m.conversationHistory {
		if msg.Role != "user" && msg.Role != "assistant" {
			continue
		}
		content := strings.TrimSpace(msg.Content)
		if content == "" {
			continue
		}
		messages = append(messages, ChatMessage{Role: msg.Role, Content: content})
	}
	m.chatMessages = messages
	m.activeAssistantIdx = -1
	m.activeToolIndices = map[string]int{}
	m.refreshChatViewport()
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

		contextWindow := 128000
		if m.chatService != nil && m.selectedProvider != "" && m.selectedModelID != "" {
			if cw, err := m.chatService.ModelContextWindow(context.Background(), m.selectedProvider, m.selectedModelID); err == nil && cw > 0 {
				contextWindow = cw
			}
		}

		summarizer := func(ctx context.Context, toSummarize []llm.Message, previousSummary string) (string, error) {
			if m.chatService == nil || m.selectedProvider == "" || m.selectedModelID == "" {
				return "", nil
			}
			return m.chatService.SummarizeCompaction(ctx, m.selectedProvider, m.selectedModelID, toSummarize, previousSummary)
		}

		compacted, err := m.sessionStore.ManualCompact(context.Background(), contextWindow, summarizer)
		if err != nil {
			return compactDoneMsg{Err: err}
		}
		history := m.sessionStore.ContextMessages()
		return compactDoneMsg{Compacted: compacted, History: history}
	}
}

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
