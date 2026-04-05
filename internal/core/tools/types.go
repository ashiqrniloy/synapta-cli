package tools

import "context"

const (
	DefaultMaxLines = 2000
	DefaultMaxBytes = 50 * 1024 // 50KB
)

type ContentPartType string

const (
	ContentPartText  ContentPartType = "text"
	ContentPartImage ContentPartType = "image"
)

// ContentPart is a multimodal payload returned by tools.
type ContentPart struct {
	Type     ContentPartType `json:"type"`
	Text     string          `json:"text,omitempty"`
	Data     string          `json:"data,omitempty"`     // base64 for images
	MimeType string          `json:"mimeType,omitempty"` // image mime type
}

type TruncationBy string

const (
	TruncationByLines TruncationBy = "lines"
	TruncationByBytes TruncationBy = "bytes"
)

// Truncation describes how output was trimmed.
type Truncation struct {
	Truncated             bool         `json:"truncated"`
	TruncatedBy           TruncationBy `json:"truncatedBy,omitempty"`
	TotalLines            int          `json:"totalLines"`
	TotalBytes            int          `json:"totalBytes"`
	OutputLines           int          `json:"outputLines"`
	OutputBytes           int          `json:"outputBytes"`
	LastLinePartial       bool         `json:"lastLinePartial"`
	FirstLineExceedsLimit bool         `json:"firstLineExceedsLimit"`
	MaxLines              int          `json:"maxLines"`
	MaxBytes              int          `json:"maxBytes"`
}

// Result is the generic return payload for all tools.
type Result struct {
	Content []ContentPart `json:"content"`
	Details any           `json:"details,omitempty"`
}

// StreamUpdate can be used by long-running tools (bash) to emit incremental updates.
type StreamUpdate func(Result)

// Tool is the shared core interface for all built-in tools.
type Tool interface {
	Name() string
	Description() string
}

// ReadInput is the input for the read tool.
type ReadInput struct {
	Path   string `json:"path"`
	Offset *int   `json:"offset,omitempty"` // 1-indexed
	Limit  *int   `json:"limit,omitempty"`
}

// WriteInput is the input for the write tool.
type WriteInput struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// BashInput is the input for the bash tool.
type BashInput struct {
	Command string `json:"command"`
	Timeout *int   `json:"timeout,omitempty"` // seconds
}

// ToolSet is the shared core bundle consumed by agents.
type ToolSet struct {
	Read  *ReadTool
	Write *WriteTool
	Bash  *BashTool
}

func NewToolSet(cwd string) *ToolSet {
	return &ToolSet{
		Read:  NewReadTool(cwd),
		Write: NewWriteTool(cwd),
		Bash:  NewBashTool(cwd),
	}
}

// Optional compile-time guard for function signatures used by the agent runtime.
type ReadExecutor func(context.Context, ReadInput) (Result, error)
type WriteExecutor func(context.Context, WriteInput) (Result, error)
type BashExecutor func(context.Context, BashInput, StreamUpdate) (Result, error)
