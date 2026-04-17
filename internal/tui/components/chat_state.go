package components

import (
	"time"
)

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
	HistoryIndex    int
	RawHistoryIndex int
	Editable        bool
	Removable       bool
}
