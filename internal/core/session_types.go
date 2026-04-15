package core

import (
	"context"
	"os"
	"sync"
	"time"

	"github.com/ashiqrniloy/synapta-cli/internal/llm"
)

const (
	currentSessionVersion = 1

	compactionSummaryPrefix       = "The conversation history before this point was compacted into the following summary:\n\n<summary>\n"
	compactionSummarySuffix       = "\n</summary>"
	compactionMethodModel         = "model"
	compactionMethodDeterministic = "fallback"
)

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
	Type                  string            `json:"type"`
	Version               int               `json:"version,omitempty"`
	ID                    string            `json:"id,omitempty"`
	Timestamp             string            `json:"timestamp,omitempty"`
	CWD                   string            `json:"cwd,omitempty"`
	Message               *llm.Message      `json:"message,omitempty"`
	Summary               string            `json:"summary,omitempty"`
	FirstKeptMessageIndex int               `json:"firstKeptMessageIndex,omitempty"`
	TokensBefore          int               `json:"tokensBefore,omitempty"`
	CompactionMethod      string            `json:"compactionMethod,omitempty"`
	Operation             *ContextOperation `json:"operation,omitempty"`
}

type ContextOperation struct {
	Action       string `json:"action"`
	ContextIndex int    `json:"contextIndex"`
	Role         string `json:"role,omitempty"`
	Category     string `json:"category,omitempty"`
	BeforeHash   string `json:"beforeHash,omitempty"`
	AfterHash    string `json:"afterHash,omitempty"`
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
