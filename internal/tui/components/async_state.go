package components

import (
	"time"

	"github.com/ashiqrniloy/synapta-cli/internal/core"
	"github.com/ashiqrniloy/synapta-cli/internal/llm"
)

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

// FileAddedMsg is sent when a file is selected from the file browser.
type FileAddedMsg struct {
	Path string
}

type chatStreamDoneMsg struct{}

type chatStreamCancelledMsg struct{}

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
