package core

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/ashiqrniloy/synapta-cli/internal/llm"
)

const (
	currentSessionVersion = 1

	compactionSummaryPrefix = "The conversation history before this point was compacted into the following summary:\n\n<summary>\n"
	compactionSummarySuffix = "\n</summary>"
)

type CompactionMethod string

const (
	CompactionMethodModel         CompactionMethod = "model"
	CompactionMethodDeterministic CompactionMethod = "fallback"
)

func (m CompactionMethod) IsValid() bool {
	switch m {
	case CompactionMethodModel, CompactionMethodDeterministic:
		return true
	default:
		return false
	}
}

type SessionEntryType string

const (
	SessionEntryTypeSession    SessionEntryType = "session"
	SessionEntryTypeMessage    SessionEntryType = "message"
	SessionEntryTypeCompaction SessionEntryType = "compaction"
	SessionEntryTypeContextOp  SessionEntryType = "context_op"
)

func (t SessionEntryType) IsValid() bool {
	switch t {
	case SessionEntryTypeSession, SessionEntryTypeMessage, SessionEntryTypeCompaction, SessionEntryTypeContextOp:
		return true
	default:
		return false
	}
}

type SessionOperationAction string

const (
	SessionOperationActionEdit   SessionOperationAction = "edit"
	SessionOperationActionRemove SessionOperationAction = "remove"
)

func (a SessionOperationAction) IsValid() bool {
	switch a {
	case SessionOperationActionEdit, SessionOperationActionRemove:
		return true
	default:
		return false
	}
}

type CompactionSettings struct {
	Enabled          bool
	ReserveTokens    int
	KeepRecentTokens int
}

func DefaultCompactionSettings() CompactionSettings {
	return CompactionSettings{
		Enabled:          true,
		ReserveTokens:    16384,
		KeepRecentTokens: 20000,
	}
}

type CompactionSummarizer func(ctx context.Context, toSummarize []llm.Message, previousSummary string) (string, error)

type SessionInfo struct {
	Path         string
	ID           string
	CWD          string
	Created      time.Time
	Modified     time.Time
	MessageCount int
	FirstMessage string
}

type sessionEntry struct {
	Type                  SessionEntryType
	Version               int
	ID                    string
	Timestamp             time.Time
	CWD                   string
	Message               *llm.Message
	Summary               string
	FirstKeptMessageIndex int
	TokensBefore          int
	CompactionMethod      CompactionMethod
	Operation             *ContextOperation
}

type ContextOperation struct {
	Action       SessionOperationAction `json:"action"`
	ContextIndex int                    `json:"contextIndex"`
	Role         llm.MessageRole        `json:"role,omitempty"`
	Category     string                 `json:"category,omitempty"`
	BeforeHash   string                 `json:"beforeHash,omitempty"`
	AfterHash    string                 `json:"afterHash,omitempty"`
}

func (op ContextOperation) Validate() error {
	if !op.Action.IsValid() {
		return fmt.Errorf("invalid context operation action: %q", op.Action)
	}
	if op.Role != "" && !op.Role.IsValid() {
		return fmt.Errorf("invalid context operation role: %q", op.Role)
	}
	return nil
}

type contextMessageRef struct {
	Message      llm.Message
	EntryIndex   int
	MessageIndex int
	IsCompaction bool
}

// SessionStore persists and compacts conversation history in a pi-style append-only JSONL file.
//
// Sessions are grouped by agent and CWD and stored as separate JSONL files:
//
//	~/.synapta/sessions/<agent>/<encoded-cwd>/<timestamp>_<session-id>.jsonl
//
// The active session can be switched to support "new session" and "resume session" flows.
type SessionStore struct {
	mu         sync.Mutex
	baseDir    string
	agentID    string
	cwd        string
	sessionDir string
	filePath   string
	sessionID  string
	entries    []sessionEntry
	settings   CompactionSettings
	appendFile *os.File
}
