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

	// Annotate every output line with its 1-indexed line number.
	// Eliminates the need for `nl` or similar bash commands.
	IncludeLineNumbers *bool `json:"include_line_numbers,omitempty"`

	// Locate mode: search for a literal string or RE2 regex inside the file.
	// Returns matching line numbers and optional surrounding context.
	// When set, offset/limit/include_line_numbers are ignored.
	Pattern     string `json:"pattern,omitempty"`      // literal string or regex (when pattern_is_regex=true)
	PatternIsRegex *bool `json:"pattern_is_regex,omitempty"` // default false → literal search
	ContextLines *int   `json:"context_lines,omitempty"`    // lines of context before/after each match (default 0)
}

// WriteMode selects the edit strategy for write.
type WriteMode string

const (
	WriteModeOverwrite    WriteMode = "overwrite"     // default; write full content
	WriteModeReplace      WriteMode = "replace"       // literal find/replace in existing content
	WriteModeReplaceRegex WriteMode = "replace_regex" // regex find/replace in existing content
	WriteModeLineEdit     WriteMode = "line_edit"     // replace [start_line,end_line] inclusive
	WriteModePatch        WriteMode = "patch"         // apply unified diff
)

// WriteInput is the input for the write tool.
type WriteInput struct {
	Path    string    `json:"path"`
	Content string    `json:"content,omitempty"`
	Mode    WriteMode `json:"mode,omitempty"`

	// replace and replace_regex modes
	Find            string `json:"find,omitempty"`
	Replace         string `json:"replace,omitempty"`
	ExpectedMatches *int   `json:"expected_matches,omitempty"`
	MaxReplacements *int   `json:"max_replacements,omitempty"`

	// line_edit mode (1-indexed, inclusive)
	StartLine *int `json:"start_line,omitempty"`
	EndLine   *int `json:"end_line,omitempty"`

	// patch mode
	UnifiedDiff string `json:"unified_diff,omitempty"`

	// behavior flags
	DryRun                  *bool `json:"dry_run,omitempty"`
	PreserveTrailingNewline *bool `json:"preserve_trailing_newline,omitempty"`

	// When false (default) the response omits the full file preview, keeping
	// the result compact. Set to true to include a head-truncated preview.
	IncludePreview *bool `json:"include_preview,omitempty"`
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
