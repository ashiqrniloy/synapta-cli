package components

import (
	"context"
	"os"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"

	"github.com/ashiqrniloy/synapta-cli/internal/application"
	"github.com/ashiqrniloy/synapta-cli/internal/config"
	"github.com/ashiqrniloy/synapta-cli/internal/core"
	"github.com/ashiqrniloy/synapta-cli/internal/core/tools"
	"github.com/ashiqrniloy/synapta-cli/internal/llm"
	"github.com/ashiqrniloy/synapta-cli/internal/tui/theme"
)

const (
	defaultModelFetchTimeout       = 12 * time.Second
	defaultBalanceCheckTimeout     = 8 * time.Second
	defaultManualCompactionTimeout = 300 * time.Second
)

type keybindingRow struct {
	Action      string
	Binding     string
	Description string
}

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

	picker        *CommandPicker
	skillPicker   *SkillPicker
	fileBrowser   *FileBrowser
	sessionSearch *SessionSearchPicker

	availableSkills   []core.Skill
	skillCatalogCache *core.SkillCatalogCache

	selectedModelName     string
	selectedModelID       string
	selectedProvider      string
	selectedThinkingLevel string
	selectedContextWindow int
	providerBalance       string

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
	chatController        *application.ChatController
	sessionService        *application.SessionService
	providerService       *application.ProviderService
	extensionService      *application.ExtensionService
	workspaceService      *application.WorkspaceService
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

	contextActions             []ContextAction
	contextModalOpen           bool
	contextModalEditMode       bool
	contextModalSelection      int
	contextModalEntries        []ContextEntry
	contextModalEditor         textarea.Model
	contextModalEditorHint     string
	contextModalPreviewOffset  int
	contextOverrideActive      bool
	keybindingsModalOpen       bool
	keybindingsSearch          string
	keybindingsSelection       int
	commandModalOpen           bool
	commandModalInput          textarea.Model
	fileBrowserModalOpen       bool
	fileBrowserModalCursor     int
	fileBrowserModalSearch     string
	fileBrowserModalEntries    []FileEntry
	fileBrowserModalOffset     int
	fileBrowserModalPath       string
	chatRenderedLines          []string
	chatMessageStartLines      []int
	sessionSearchHighlightLine int
	lastPromptHash             string
	lastPromptFingerprint      core.PromptFingerprint
	likelyPromptCacheHit       bool
	promptBuildCount           int
	stablePrefixChangeCount    int
	cancelStream               context.CancelFunc
	lifecycleCtx               context.Context
	cancelLifecycle           context.CancelFunc
	pendingUserMessage         string
	availableExtensions        []core.Extension

	cachedContextEntries []ContextEntry
	contextEntriesDirty  bool
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
	toolset := tools.NewToolSet(cwd)

	chatService := core.NewChatService(authStorage, toolset)
	chatController := application.NewChatController(chatService)
	contextManager := core.NewContextManager(core.AgentCode, agentDir, cwd, systemPromptStore)

	sessionStore, _ := core.NewSessionStore(agentDir, core.AgentCode, cwd, core.DefaultCompactionSettings())
	conversationHistory := make([]llm.Message, 0)
	if sessionStore != nil {
		conversationHistory = sessionStore.ContextMessages()
	}
	lifecycleCtx, cancelLifecycle := context.WithCancel(context.Background())

	skillCatalogCache := core.NewSkillCatalogCache()

	sessionService := application.NewSessionService(agentDir, core.AgentCode, chatController, contextManager, sessionStore)
	providerService := application.NewProviderService(authStorage, chatController)
	extensionService := application.NewExtensionService()
	workspaceService := application.NewWorkspaceService()
	model := &CodeAgentModel{
		styles:                styles,
		ta:                    buildTextarea(t, cfg),
		borderColor:           t.Border,
		cfg:                   cfg,
		picker:                NewCommandPicker(styles),
		skillPicker:           NewSkillPicker(styles),
		fileBrowser:           NewFileBrowser(styles),
		sessionSearch:         NewSessionSearchPicker(styles),
		skillCatalogCache:     skillCatalogCache,
		authStorage:           authStorage,
		chatService:           chatService,
		systemPromptStore:     systemPromptStore,
		contextManager:        contextManager,
		chatController:        chatController,
		sessionService:        sessionService,
		providerService:       providerService,
		extensionService:      extensionService,
		workspaceService:      workspaceService,
		sessionStore:          sessionStore,
		agentDir:              agentDir,
		conversationHistory:   conversationHistory,
		activeAssistantIdx:    -1,
		activeSystemStatusIdx: -1,
		lifecycleCtx:          lifecycleCtx,
		cancelLifecycle:       cancelLifecycle,
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
	model.contextEntriesDirty = true

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
	model.commandModalInput = buildTextarea(t, cfg)
	model.commandModalInput.Placeholder = "Command mode… type to filter"
	model.commandModalInput.SetValue(":")
	model.commandModalInput.Blur()

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
