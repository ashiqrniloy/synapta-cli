package components

import (
	"os"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"

	"github.com/ashiqrniloy/synapta-cli/internal/config"
	"github.com/ashiqrniloy/synapta-cli/internal/core"
	"github.com/ashiqrniloy/synapta-cli/internal/llm"
	"github.com/ashiqrniloy/synapta-cli/internal/tui/theme"
)

// ─── Kilo Auth Messages ─────────────────────────────────────────────

// KiloAuthProgressMsg reports progress during Kilo authentication.
type KiloAuthProgressMsg string

// CopilotAuthProgressMsg reports progress during GitHub Copilot authentication.
type CopilotAuthProgressMsg string

// CopilotAuthPromptMsg carries the device auth URL and code.
type CopilotAuthPromptMsg struct {
	VerificationURL string
	UserCode        string
}

type authFlowDoneMsg struct{}

// KiloAuthCompleteMsg is sent when Kilo authentication completes.
type KiloAuthCompleteMsg struct {
	Err        error
	Email      string
	ModelCount int
}

// CopilotAuthCompleteMsg is sent when GitHub Copilot authentication completes.
type CopilotAuthCompleteMsg struct {
	Err        error
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
	Timestamp       time.Time
	EstimatedTokens int
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

type assistantToolCallsMsg struct {
	Message llm.Message
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
	Method    string
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

	layoutModeSplit   = "split"
	layoutModeStacked = "stacked"
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
	selectedContextWindow int
	providerBalance       string

	// Chat messages
	chatMessages          []ChatMessage
	isWorking             bool
	activeAssistantIdx    int
	activeSystemStatusIdx int
	activeToolIndices     map[string]int
	toolExpanded          map[string]bool
	selectedToolCallID    string
	streamCh              <-chan tea.Msg
	authCh                <-chan tea.Msg
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

	inputMode         string
	currentCwd        string
	currentGitBranch  string
	isExecutingBash   bool
	workingFrame      int
	chatPaneWidth     int
	contextPaneWidth  int
	contextPaneHeight int
	layoutMode        string

	contextActions            []ContextAction
	contextModalOpen          bool
	contextModalEditMode      bool
	contextModalSelection     int
	contextModalEntries       []ContextEntry
	contextModalEditor        textarea.Model
	contextModalEditorHint    string
	contextModalPreviewOffset int
	contextOverrideActive     bool
	keybindingsModalOpen      bool
	keybindingsSearch         string
	keybindingsSelection      int
	lastPromptHash            string
	lastPromptFingerprint     core.PromptFingerprint
	likelyPromptCacheHit      bool
	promptBuildCount          int
	stablePrefixChangeCount   int
	availableExtensions       []core.Extension
}

// NewCodeAgentModel creates the model using the loaded AppConfig.
func NewCodeAgentModel(cfg *config.AppConfig) *CodeAgentModel {
	t := cfg.ActiveTheme()
	styles := theme.NewStyles(t)

	agentDir := llm.GetAgentDir()
	authStorage, _ := llm.NewAuthStorage(agentDir)
	systemPromptStore := core.NewSystemPromptStore(agentDir)
	_ = systemPromptStore.EnsureDefaultIfAgentDirMissing(core.AgentCode, "")
	_ = core.LoadCompactionPrompt(agentDir)

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
		currentGitBranch:      detectGitBranch(cwd),
		layoutMode:            layoutModeStacked,
	}

	if cfg.Provider.Default != "" && cfg.Provider.Model != "" {
		model.selectedProvider = cfg.Provider.Default
		model.selectedModelID = cfg.Provider.Model
		model.selectedModelName = cfg.Provider.Model
		model.selectedThinkingLevel = inferThinkingLevel(cfg.Provider.Model, cfg.Provider.Model)
		model.selectedContextWindow = resolveModelContextWindow(cfg.Provider.Default, cfg.Provider.Model)
	}

	model.rebuildTranscriptFromHistory()
	model.reloadAvailableSkills()
	model.reloadAvailableExtensions()
	model.recordContextAction("System prompt loaded")

	if authStorage != nil {
		if authStorage.HasAuth("kilo") {
			model.appendSystemMessage("[Kilo] ✓ Authenticated", "done")
		}
		if authStorage.HasAuth("github-copilot") {
			model.appendSystemMessage("[GitHub Copilot] ✓ Authenticated", "done")
		}
	}
	model.contextModalEditor = buildTextarea(t, cfg)
	model.contextModalEditor.Placeholder = "Edit selected context (Ctrl+S to save, Esc to cancel)"

	model.applyInputMode(inputModeChat)
	return model
}

func buildTextarea(t config.Theme, cfg *config.AppConfig) textarea.Model {
	_ = cfg
	ta := textarea.New()
	ta.Placeholder = "Type your message... (Enter=send, Shift+Enter/Ctrl+N=newline)"
	ta.ShowLineNumbers = false
	ta.DynamicHeight = true
	ta.MinHeight = 1
	ta.MaxHeight = 15

	noBg := lipgloss.NewStyle().Foreground(lipgloss.Color(t.Foreground))
	placeholder := lipgloss.NewStyle().Foreground(lipgloss.Color(t.Muted))
	empty := lipgloss.NewStyle()

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
	ta.KeyMap.InsertNewline.SetKeys()

	return ta
}

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

	v := tea.NewView(base)
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	v.KeyboardEnhancements.ReportEventTypes = true
	return v
}
