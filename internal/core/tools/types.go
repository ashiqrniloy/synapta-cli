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

// ToolMetadata captures lightweight invocation metadata used by runtime and UI.
type ToolMetadata struct {
	Path    string `json:"path,omitempty"`
	Command string `json:"command,omitempty"`
}

// RuntimeTool extends Tool with schema/decoding/metadata/execution hooks so a
// tool can be registered as a single source of truth.
type RuntimeTool interface {
	Tool
	JSONSchema() map[string]any
	Decode(raw string) (any, error)
	Metadata(any) ToolMetadata
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
	Pattern        string `json:"pattern,omitempty"`          // literal string or regex (when pattern_is_regex=true)
	PatternIsRegex *bool  `json:"pattern_is_regex,omitempty"` // default false → literal search
	ContextLines   *int   `json:"context_lines,omitempty"`    // lines of context before/after each match (default 0)
}

// WriteMode selects the edit strategy for write.
type WriteMode string

const (
	WriteModeOverwrite       WriteMode = "overwrite"         // default; write full content
	WriteModeAppend          WriteMode = "append"            // append content to end of file (or create)
	WriteModeReplace         WriteMode = "replace"           // literal find/replace in existing content
	WriteModeReplaceRegex    WriteMode = "replace_regex"     // regex find/replace in existing content
	WriteModeLineEdit        WriteMode = "line_edit"         // replace [start_line,end_line] inclusive
	WriteModeInsertAfterLine WriteMode = "insert_after_line" // insert lines after a given line number
	WriteModePatch           WriteMode = "patch"             // apply unified diff
)

// WriteInput is the input for the write tool.
type WriteInput struct {
	// Path to the file to create or edit. Required.
	Path string `json:"path"`

	// Content is the canonical field for file content or text to write:
	//   - overwrite mode: the full new file content (must not be empty).
	//   - append mode: the text to append to the end of the file.
	//   - replace/replace_regex/line_edit/insert_after_line modes: the replacement/inserted text.
	Content string `json:"content,omitempty"`

	// Mode selects the edit strategy. Defaults to "overwrite" when omitted.
	Mode WriteMode `json:"mode,omitempty"`

	// ── stale-write protection ────────────────────────────────────────────────

	// SHA256Before is the hex SHA-256 of the file content the agent last read.
	// When set, the tool checks the current file hash before writing and fails
	// with a clear error if it has changed (i.e. another write happened in
	// between). This prevents silent overwrite of concurrent edits.
	// Obtain the value from read tool's details.sha256 field.
	// Not applicable to overwrite or append modes that create new files.
	SHA256Before string `json:"sha256_before,omitempty"`

	// ── replace / replace_regex ──────────────────────────────────────────────

	// Find is the literal text (replace) or RE2 pattern (replace_regex) to match.
	Find string `json:"find,omitempty"`

	// Replace is an alias for Content in replace/replace_regex modes.
	// Provided for backward compatibility; Content takes precedence when both are set.
	Replace string `json:"replace,omitempty"`

	// ExpectedMatches makes the operation fail if the actual match count differs.
	// Strongly recommended when targeting a specific number of occurrences (e.g. 1).
	ExpectedMatches *int `json:"expected_matches,omitempty"`

	// MaxReplacements caps how many matches are replaced. Defaults to all.
	MaxReplacements *int `json:"max_replacements,omitempty"`

	// ── line_edit ────────────────────────────────────────────────────────────

	// StartLine is the 1-indexed first line to replace (inclusive).
	StartLine *int `json:"start_line,omitempty"`

	// EndLine is the 1-indexed last line to replace (inclusive). Must be >= StartLine.
	EndLine *int `json:"end_line,omitempty"`

	// ── insert_after_line ────────────────────────────────────────────────────

	// AfterLine is the 1-indexed line after which content is inserted.
	// Use 0 to insert at the very beginning of the file (before line 1).
	AfterLine *int `json:"after_line,omitempty"`

	// ── patch ────────────────────────────────────────────────────────────────

	// UnifiedDiff is a standard unified diff with hunk headers (@@ -old,+new @@).
	// Required for patch mode. Must NOT use *** Begin Patch wrappers.
	UnifiedDiff string `json:"unified_diff,omitempty"`

	// ── behavior flags ───────────────────────────────────────────────────────

	// DryRun computes and returns the diff without writing the file.
	DryRun *bool `json:"dry_run,omitempty"`

	// PreserveTrailingNewline controls whether the original file's trailing
	// newline is kept after a line_edit or insert_after_line. Default true.
	PreserveTrailingNewline *bool `json:"preserve_trailing_newline,omitempty"`

	// IncludePreview appends a head-truncated file preview to the response.
	// Default false — keeps the response compact.
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
