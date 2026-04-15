package tools

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	udiff "github.com/aymanbagabas/go-udiff"
)

type WriteTool struct {
	cwd string
}

func NewWriteTool(cwd string) *WriteTool {
	return &WriteTool{cwd: cwd}
}

func (t *WriteTool) Name() string { return "write" }

func (t *WriteTool) Description() string {
	return `Edit files using one of these modes (mode field is required for edits):

MODES:
- overwrite (default): Replace the entire file with new content. Creates the file if it does not exist.
  Requires: path, content (must not be empty).
  Example: {"path":"internal/core/chat.go","mode":"overwrite","content":"package core\n"}

- append: Append content to the end of an existing file, or create the file if it does not exist.
  Requires: path, content.
  Appended text is added exactly as given — include a leading newline if needed.
  Example: {"path":"log.txt","mode":"append","content":"\n// added import\nimport \"fmt\"\n"}

- replace: Find and replace an exact literal string in an existing file.
  Requires: path, find, content (replacement text).
  Optional: expected_matches, max_replacements.
  Example: {"path":"internal/core/chat.go","mode":"replace","find":"old line","content":"new line","expected_matches":1}

- replace_regex: Find and replace using a RE2 regex in an existing file.
  Requires: path, find (regex pattern), content (replacement text).
  Optional: expected_matches, max_replacements.
  Example: {"path":"internal/core/chat.go","mode":"replace_regex","find":"func\\s+old","content":"func new","max_replacements":1}

- line_edit: Replace a line range in an existing file (1-indexed, inclusive).
  Requires: path, start_line, end_line, content.
  Example: {"path":"internal/core/chat.go","mode":"line_edit","start_line":42,"end_line":48,"content":"new line 42\nnew line 43\n"}

- insert_after_line: Insert new lines after a specific line in an existing file without replacing anything.
  Requires: path, after_line (1-indexed; use 0 to insert before line 1), content.
  Example: {"path":"internal/core/chat.go","mode":"insert_after_line","after_line":10,"content":"// new comment\nfunc helper() {}\n"}

- patch: Apply a standard unified diff to an existing file.
  Requires: path, unified_diff.
  unified_diff is mandatory for patch mode.
  The patch must be a standard unified diff and include hunk headers like @@ -old,+new @@.
  Example: {"path":"internal/core/chat.go","mode":"patch","unified_diff":"--- a/internal/core/chat.go\n+++ b/internal/core/chat.go\n@@ -42,7 +42,8 @@\n old line\n+new line\n"}

IMPORTANT:
- Never use bash with sed/awk/python to edit files. Always use this write tool for file modifications.
- For patch mode, custom wrappers like "*** Begin Patch" / "*** End Patch" are not accepted.
- For replace/replace_regex: always set expected_matches=1 when you intend to change exactly one occurrence. This prevents silent partial edits.
- overwrite and append with empty content will be rejected.
- Use sha256_before to guard against writing over a file that changed since you last read it.

PARAMETERS:
- path: File to edit (relative or absolute). Required.
- mode: Edit strategy (default: overwrite). See MODES above.
- content: New file content (overwrite), text to append (append), or replacement/inserted text (replace/replace_regex/line_edit/insert_after_line).
- sha256_before: Optional. Hex SHA-256 of the file as last read. If the file has changed since, the write is rejected. Obtain from read tool's details.sha256 field.
- find: Exact text to match (replace) or RE2 regex pattern (replace_regex). Required for those modes.
- expected_matches: Expected number of matches (replace/replace_regex); tool errors if actual count differs. Recommended for safety.
- max_replacements: Limit how many replacements to apply (replace/replace_regex).
- start_line: 1-indexed start line, inclusive (line_edit).
- end_line: 1-indexed end line, inclusive (line_edit). Must be >= start_line.
- after_line: 1-indexed line after which to insert content (insert_after_line). Use 0 to insert before line 1.
- unified_diff: Standard unified diff with hunk headers (patch mode). Required for patch.
- preserve_trailing_newline: Keep the original file's trailing newline in line_edit/insert_after_line (default true).
- dry_run: Plan and diff without writing changes. Returns what would change.
- include_preview: When true, append a head-truncated preview of the result. Default false.`
}

// ── write plan ───────────────────────────────────────────────────────────────

type writePlan struct {
	Mode               WriteMode
	OldExists          bool
	OldContent         string
	NewContent         string
	AppliedMatches     int
	ExpectedMatches    *int
	MaxReplacements    *int
	StartLine          *int
	EndLine            *int
	AfterLine          *int
	DryRun             bool
	PreserveTrailingNL bool
}

// ── WriteDetails — structured metadata returned to the agent ─────────────────

type WriteDetails struct {
	// Edit facts
	Mode           WriteMode `json:"mode"`
	DryRun         bool      `json:"dry_run"`
	Changed        bool      `json:"changed"`
	Insertions     int       `json:"insertions"`
	Deletions      int       `json:"deletions"`
	ChangedRanges  []string  `json:"changed_ranges,omitempty"` // e.g. ["12-18","24"]
	AppliedMatches int       `json:"applied_matches,omitempty"`

	// File size facts
	LineCountBefore int `json:"line_count_before"`
	LineCountAfter  int `json:"line_count_after"`
	BytesBefore     int `json:"bytes_before"`
	BytesAfter      int `json:"bytes_after"`

	// Integrity hashes — sha256_after can be fed back as sha256_before on the
	// next write to ensure no concurrent modification occurred between calls.
	SHA256After string `json:"sha256_after,omitempty"`

	// Optional mode-specific fields (only emitted when relevant)
	ExpectedMatches *int `json:"expected_matches,omitempty"`
	MaxReplacements *int `json:"max_replacements,omitempty"`
	StartLine       *int `json:"start_line,omitempty"`
	EndLine         *int `json:"end_line,omitempty"`
	AfterLine       *int `json:"after_line,omitempty"`
	// PreserveTrailingNewline is only emitted for line_edit / insert_after_line.
	PreserveTrailingNewline *bool `json:"preserve_trailing_newline,omitempty"`
}

// ── Execute ──────────────────────────────────────────────────────────────────

func (t *WriteTool) Execute(ctx context.Context, in WriteInput) (Result, error) {
	if strings.TrimSpace(in.Path) == "" {
		return Result{}, fmt.Errorf("path is required. Provide `path` (relative or absolute). Example: {\"path\":\"internal/core/chat.go\",\"mode\":\"overwrite\",\"content\":\"...\"}")
	}

	absPath := resolveToCwd(in.Path, t.cwd)

	// Guard: reject path traversal outside CWD.
	if t.cwd != "" {
		cleanCWD := filepath.Clean(t.cwd)
		cleanAbs := filepath.Clean(absPath)
		if !strings.HasPrefix(cleanAbs, cleanCWD+string(filepath.Separator)) && cleanAbs != cleanCWD {
			return Result{}, fmt.Errorf("path %q resolves outside the working directory %q. Use a path within the project root.", in.Path, t.cwd)
		}
	}

	dir := filepath.Dir(absPath)

	oldContentBytes, readErr := os.ReadFile(absPath)
	oldExists := readErr == nil
	if readErr != nil && !os.IsNotExist(readErr) {
		return Result{}, fmt.Errorf("reading existing file: %w", readErr)
	}
	oldContent := string(oldContentBytes)

	// ── Stale-write guard ────────────────────────────────────────────────────
	if in.SHA256Before != "" && oldExists {
		actual := fmt.Sprintf("%x", sha256.Sum256(oldContentBytes))
		if actual != in.SHA256Before {
			return Result{}, fmt.Errorf(
				"stale write rejected: file %q has changed since it was last read "+
					"(expected sha256 %s, got %s). "+
					"Read the file again to get the latest content, then retry.",
				in.Path, in.SHA256Before, actual)
		}
	}

	plan, err := buildWritePlan(in, oldContent, oldExists)
	if err != nil {
		return Result{}, err
	}

	if !plan.DryRun {
		err = withFileMutationQueue(absPath, func() error {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return fmt.Errorf("creating parent dirs: %w", err)
			}
			return writeFileDirect(absPath, []byte(plan.NewContent))
		})
		if err != nil {
			return Result{}, err
		}
	}

	changed := plan.OldContent != plan.NewContent

	// ── Diff (via go-udiff) ──────────────────────────────────────────────────
	insertions, deletions, changedRangeStrs, diffText := computeDisplayDiff(plan.OldContent, plan.NewContent)

	// ── Hash ────────────────────────────────────────────────────────────────
	hashAfter := fmt.Sprintf("%x", sha256.Sum256([]byte(plan.NewContent)))

	// ── Compact text summary ─────────────────────────────────────────────────
	verb := "wrote"
	if plan.DryRun {
		verb = "planned write"
	}
	var sb strings.Builder
	if changed {
		sb.WriteString(fmt.Sprintf("Successfully %s %s (mode=%s, +%d -%d lines)", verb, in.Path, string(plan.Mode), insertions, deletions))
	} else {
		sb.WriteString(fmt.Sprintf("No changes for %s (mode=%s)", in.Path, string(plan.Mode)))
	}
	if plan.Mode == WriteModeReplace || plan.Mode == WriteModeReplaceRegex {
		sb.WriteString(fmt.Sprintf(", matches=%d", plan.AppliedMatches))
	}
	if plan.Mode == WriteModeLineEdit && plan.StartLine != nil && plan.EndLine != nil {
		sb.WriteString(fmt.Sprintf(", lines=%d-%d", *plan.StartLine, *plan.EndLine))
	}
	if plan.Mode == WriteModeInsertAfterLine && plan.AfterLine != nil {
		sb.WriteString(fmt.Sprintf(", after_line=%d", *plan.AfterLine))
	}
	if changed && len(changedRangeStrs) > 0 {
		sb.WriteString(fmt.Sprintf("\nChanged ranges (new): %s", strings.Join(changedRangeStrs, ", ")))
	}

	if diffText != "" {
		sb.WriteString("\n\n")
		sb.WriteString(diffText)
	}

	// ── Optional file preview ─────────────────────────────────────────────────
	wantPreview := in.IncludePreview != nil && *in.IncludePreview
	if wantPreview {
		preview, trunc := truncateHead(plan.NewContent, 60, 8*1024)
		if strings.TrimSpace(preview) != "" {
			sb.WriteString("\n\n--- file preview ---\n")
			sb.WriteString(preview)
			if trunc.Truncated {
				sb.WriteString(fmt.Sprintf("\n\n[Preview truncated to %d lines / %s]", trunc.OutputLines, formatSize(trunc.OutputBytes)))
			}
		}
	}

	// ── Build details — only emit mode-relevant optional fields ──────────────
	oldLineCount := countLines(plan.OldContent)
	newLineCount := countLines(plan.NewContent)

	details := WriteDetails{
		Mode:            plan.Mode,
		DryRun:          plan.DryRun,
		Changed:         changed,
		Insertions:      insertions,
		Deletions:       deletions,
		ChangedRanges:   changedRangeStrs,
		AppliedMatches:  plan.AppliedMatches,
		LineCountBefore: oldLineCount,
		LineCountAfter:  newLineCount,
		BytesBefore:     len(plan.OldContent),
		BytesAfter:      len(plan.NewContent),
		SHA256After:     hashAfter,
	}
	switch plan.Mode {
	case WriteModeReplace, WriteModeReplaceRegex:
		details.ExpectedMatches = plan.ExpectedMatches
		details.MaxReplacements = plan.MaxReplacements
	case WriteModeLineEdit:
		details.StartLine = plan.StartLine
		details.EndLine = plan.EndLine
		ptNL := plan.PreserveTrailingNL
		details.PreserveTrailingNewline = &ptNL
	case WriteModeInsertAfterLine:
		details.AfterLine = plan.AfterLine
		ptNL := plan.PreserveTrailingNL
		details.PreserveTrailingNewline = &ptNL
	}

	return Result{
		Content: []ContentPart{{
			Type: ContentPartText,
			Text: sb.String(),
		}},
		Details: details,
	}, nil
}

// ── buildWritePlan ───────────────────────────────────────────────────────────

func buildWritePlan(in WriteInput, oldContent string, oldExists bool) (writePlan, error) {
	mode := strings.TrimSpace(string(in.Mode))
	if mode == "" {
		mode = string(WriteModeOverwrite)
	}
	m := WriteMode(mode)
	dryRun := in.DryRun != nil && *in.DryRun
	preserveNL := in.PreserveTrailingNewline == nil || *in.PreserveTrailingNewline

	plan := writePlan{
		Mode:               m,
		OldExists:          oldExists,
		OldContent:         oldContent,
		DryRun:             dryRun,
		PreserveTrailingNL: preserveNL,
	}

	switch m {
	case WriteModeOverwrite:
		// Guard: reject empty content to prevent silent file truncation.
		if in.Content == "" {
			return writePlan{}, fmt.Errorf(
				"overwrite mode requires non-empty `content`. " +
					"Refusing to truncate the file to zero bytes. " +
					"If you truly want an empty file, use content=\"\\n\" (a single newline). " +
					"To delete a file, use the bash tool.")
		}
		plan.NewContent = in.Content

	case WriteModeAppend:
		if in.Content == "" {
			return writePlan{}, fmt.Errorf(
				"append mode requires non-empty `content`. " +
					"Provide the text to append. " +
					"Include a leading newline in content if you want a blank line before the appended text.")
		}
		plan.NewContent = oldContent + in.Content

	case WriteModeReplace:
		if !oldExists {
			return writePlan{}, fmt.Errorf("replace mode requires an existing file. The target path does not exist. Use mode=\"overwrite\" to create a new file, or provide an existing file for mode=\"replace\".")
		}
		replacement := resolveReplacement(in)
		newContent, count, err := applyStringReplace(oldContent, in.Find, replacement, in.ExpectedMatches, in.MaxReplacements)
		if err != nil {
			return writePlan{}, err
		}
		plan.ExpectedMatches = in.ExpectedMatches
		plan.MaxReplacements = in.MaxReplacements
		plan.AppliedMatches = count
		plan.NewContent = newContent

	case WriteModeReplaceRegex:
		if !oldExists {
			return writePlan{}, fmt.Errorf("replace_regex mode requires an existing file. The target path does not exist. Use mode=\"overwrite\" to create a new file, or provide an existing file for mode=\"replace_regex\".")
		}
		replacement := resolveReplacement(in)
		newContent, count, err := applyRegexReplace(oldContent, in.Find, replacement, in.ExpectedMatches, in.MaxReplacements)
		if err != nil {
			return writePlan{}, err
		}
		plan.ExpectedMatches = in.ExpectedMatches
		plan.MaxReplacements = in.MaxReplacements
		plan.AppliedMatches = count
		plan.NewContent = newContent

	case WriteModeLineEdit:
		if !oldExists {
			return writePlan{}, fmt.Errorf("line_edit mode requires an existing file. The target path does not exist. Use mode=\"overwrite\" to create a new file, or provide an existing file for mode=\"line_edit\".")
		}
		if in.StartLine == nil || in.EndLine == nil {
			return writePlan{}, fmt.Errorf("line_edit mode requires start_line and end_line (1-indexed, inclusive). Example: {\"mode\":\"line_edit\",\"path\":\"file.txt\",\"start_line\":10,\"end_line\":12,\"content\":\"new text\"}")
		}
		if *in.StartLine < 1 {
			return writePlan{}, fmt.Errorf("invalid start_line %d for line_edit mode: must be >= 1 (file lines are 1-indexed)", *in.StartLine)
		}
		if *in.EndLine < *in.StartLine {
			return writePlan{}, fmt.Errorf("invalid line range for line_edit mode: start_line=%d end_line=%d (end_line must be >= start_line)", *in.StartLine, *in.EndLine)
		}
		newContent, err := applyLineEdit(oldContent, *in.StartLine, *in.EndLine, in.Content, preserveNL)
		if err != nil {
			return writePlan{}, err
		}
		plan.StartLine = in.StartLine
		plan.EndLine = in.EndLine
		plan.NewContent = newContent

	case WriteModeInsertAfterLine:
		if !oldExists {
			return writePlan{}, fmt.Errorf("insert_after_line mode requires an existing file. The target path does not exist. Use mode=\"overwrite\" to create a new file, or use mode=\"append\" to add to the end of a new file.")
		}
		if in.AfterLine == nil {
			return writePlan{}, fmt.Errorf("insert_after_line mode requires `after_line` (1-indexed line after which to insert; use 0 to insert before line 1). Example: {\"mode\":\"insert_after_line\",\"path\":\"file.txt\",\"after_line\":10,\"content\":\"new line\\n\"}")
		}
		if *in.AfterLine < 0 {
			return writePlan{}, fmt.Errorf("invalid after_line %d for insert_after_line mode: must be >= 0 (0 = insert before line 1, 1 = insert after line 1, etc.)", *in.AfterLine)
		}
		if in.Content == "" {
			return writePlan{}, fmt.Errorf("insert_after_line mode requires non-empty `content` (the lines to insert)")
		}
		newContent, err := applyInsertAfterLine(oldContent, *in.AfterLine, in.Content, preserveNL)
		if err != nil {
			return writePlan{}, err
		}
		plan.AfterLine = in.AfterLine
		plan.NewContent = newContent

	case WriteModePatch:
		if strings.TrimSpace(in.UnifiedDiff) == "" {
			return writePlan{}, fmt.Errorf("patch mode requires `unified_diff`. Provide a standard unified diff with hunk headers like @@ -old,+new @@. Example: {\"mode\":\"patch\",\"path\":\"internal/core/chat.go\",\"unified_diff\":\"--- a/internal/core/chat.go\\n+++ b/internal/core/chat.go\\n@@ -42,7 +42,8 @@\\n old line\\n+new line\\n\"}")
		}
		newContent, err := applyUnifiedPatch(oldContent, in.UnifiedDiff)
		if err != nil {
			return writePlan{}, err
		}
		plan.NewContent = newContent

	default:
		return writePlan{}, fmt.Errorf("unsupported write mode %q. Supported modes: overwrite, append, replace, replace_regex, line_edit, insert_after_line, patch", mode)
	}

	return plan, nil
}

// resolveReplacement returns the replacement text for replace/replace_regex modes.
func resolveReplacement(in WriteInput) string {
	if in.Content != "" && in.Replace != "" && in.Content != in.Replace {
		return in.Content
	}
	if in.Content != "" {
		return in.Content
	}
	return in.Replace
}

// ── writeFileDirect — pure-Go atomic write ───────────────────────────────────

func writeFileDirect(absPath string, content []byte) error {
	dir := filepath.Dir(absPath)
	tmp, err := os.CreateTemp(dir, ".synapta-write-*")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath) // no-op if rename succeeded

	if _, err := tmp.Write(content); err != nil {
		tmp.Close()
		return fmt.Errorf("writing temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing temp file: %w", err)
	}
	if err := os.Rename(tmpPath, absPath); err != nil {
		return fmt.Errorf("renaming temp file to destination: %w", err)
	}
	return nil
}

// ── string replace helpers ────────────────────────────────────────────────────

func applyStringReplace(oldContent, find, replace string, expectedMatches, maxReplacements *int) (string, int, error) {
	if find == "" {
		return "", 0, fmt.Errorf("replace mode requires `find` (literal text to match). Provide non-empty find and content. Example: {\"mode\":\"replace\",\"path\":\"file.txt\",\"find\":\"old\",\"content\":\"new\"}")
	}
	count := strings.Count(oldContent, find)
	if expectedMatches != nil && count != *expectedMatches {
		return "", 0, fmt.Errorf("replace mode expected %d matches for %q, found %d. Update expected_matches, adjust `find`, or inspect the file first.", *expectedMatches, find, count)
	}
	if expectedMatches == nil && count == 0 {
		return "", 0, fmt.Errorf("replace mode found no matches for %q. Use read to confirm exact text, or use replace_regex/line_edit for flexible edits.", find)
	}
	if maxReplacements != nil {
		if *maxReplacements < 0 {
			return "", 0, fmt.Errorf("max_replacements must be >= 0 (got %d)", *maxReplacements)
		}
		applied := count
		if applied > *maxReplacements {
			applied = *maxReplacements
		}
		return strings.Replace(oldContent, find, replace, *maxReplacements), applied, nil
	}
	return strings.ReplaceAll(oldContent, find, replace), count, nil
}

func applyRegexReplace(oldContent, find, replace string, expectedMatches, maxReplacements *int) (string, int, error) {
	if strings.TrimSpace(find) == "" {
		return "", 0, fmt.Errorf("replace_regex mode requires `find` (RE2 pattern). Provide non-empty find and content. Example: {\"mode\":\"replace_regex\",\"path\":\"file.txt\",\"find\":\"foo(\\\\d+)\",\"content\":\"bar$1\"}")
	}
	re, err := regexp.Compile(find)
	if err != nil {
		return "", 0, fmt.Errorf("replace_regex mode received an invalid RE2 pattern %q: %w", find, err)
	}
	matches := re.FindAllStringSubmatchIndex(oldContent, -1)
	count := len(matches)
	if expectedMatches != nil && count != *expectedMatches {
		return "", 0, fmt.Errorf("replace_regex mode expected %d matches for pattern %q, found %d. Update expected_matches or adjust `find`.", *expectedMatches, find, count)
	}
	if expectedMatches == nil && count == 0 {
		return "", 0, fmt.Errorf("replace_regex mode found no matches for pattern %q. Use read to verify the target text and pattern.", find)
	}
	if maxReplacements != nil {
		if *maxReplacements < 0 {
			return "", 0, fmt.Errorf("max_replacements must be >= 0 (got %d)", *maxReplacements)
		}
		if *maxReplacements == 0 {
			return oldContent, 0, nil
		}
		limit := *maxReplacements
		if limit > count {
			limit = count
		}
		var b strings.Builder
		last := 0
		for i := 0; i < limit; i++ {
			rng := matches[i]
			b.WriteString(oldContent[last:rng[0]])
			b.Write(re.ExpandString(nil, replace, oldContent, rng))
			last = rng[1]
		}
		b.WriteString(oldContent[last:])
		return b.String(), limit, nil
	}
	return re.ReplaceAllString(oldContent, replace), count, nil
}

// ── line edit ────────────────────────────────────────────────────────────────

func applyLineEdit(oldContent string, startLine, endLine int, replacement string, preserveTrailingNL bool) (string, error) {
	endsWithNL := strings.HasSuffix(oldContent, "\n")
	oldLines := splitContentLines(oldContent)
	totalLines := len(oldLines)
	if endLine > totalLines {
		return "", fmt.Errorf(
			"line_edit mode range %d-%d is out of bounds (file has %d lines). "+
				"Use read with include_line_numbers=true to see the exact line numbers.",
			startLine, endLine, totalLines)
	}
	prefix := append([]string(nil), oldLines[:startLine-1]...)
	suffix := append([]string(nil), oldLines[endLine:]...)
	replLines := splitContentLines(replacement)
	merged := append(prefix, replLines...)
	merged = append(merged, suffix...)
	result := strings.Join(merged, "\n")
	if preserveTrailingNL && endsWithNL {
		result += "\n"
	}
	return result, nil
}

// ── insert after line ────────────────────────────────────────────────────────

func applyInsertAfterLine(oldContent string, afterLine int, insertion string, preserveTrailingNL bool) (string, error) {
	endsWithNL := strings.HasSuffix(oldContent, "\n")
	oldLines := splitContentLines(oldContent)
	totalLines := len(oldLines)

	if afterLine > totalLines {
		return "", fmt.Errorf(
			"insert_after_line: after_line=%d is out of bounds (file has %d lines). "+
				"Use after_line=%d to insert at the end, or use mode=\"append\".",
			afterLine, totalLines, totalLines)
	}

	newLines := splitContentLines(insertion)

	result := make([]string, 0, totalLines+len(newLines))
	result = append(result, oldLines[:afterLine]...)
	result = append(result, newLines...)
	result = append(result, oldLines[afterLine:]...)

	out := strings.Join(result, "\n")
	if preserveTrailingNL && endsWithNL {
		out += "\n"
	}
	return out, nil
}

// ── patch mode — tolerant unified diff applicator ────────────────────────────
//
// Applies a standard unified diff (the format LLMs generate) with robust
// tolerance for common LLM mistakes:
//
//  1. CRLF line endings in the diff text are normalised to LF.
//  2. Hunk header counts are IGNORED — only the start line is used.  LLMs
//     routinely write wrong counts (e.g. @@ -10,6 when there are 4 context
//     lines).  Ignoring counts is exactly what GNU patch does in practice.
//  3. Offset search window (±patchFuzz lines): when a context or delete line
//     doesn't match at the line number stated in the hunk header, the
//     applicator searches patchFuzz lines above and below before failing.
//     This handles off-by-one hunk starts which are the #1 LLM failure mode.
//  4. Trailing-whitespace normalisation for context lines: context lines are
//     matched after stripping trailing whitespace from both the diff line and
//     the file line.  This handles the common case where an LLM copies a
//     context line without its trailing spaces.
//  5. "\ No newline at end of file" markers are silently consumed.
//  6. Begin/End Patch wrappers are detected and rejected with a clear error.
//
// The function never silently applies a patch to a wrong location — if
// neither the exact position nor the fuzz window yields a match, it returns
// a descriptive error that tells the agent exactly which line mismatched and
// what it found, so the agent can retry with a corrected diff.
//
// After applying all hunks, the final byte-array is fed through
// udiff.Apply(original, edits) to validate consistency and let the library
// handle any edge cases.  This is intentionally belt-and-suspenders:
// we apply hunks ourselves for tolerance, then verify the result round-trips
// cleanly through the library's strict apply path.

const patchFuzz = 3 // ±lines to search when a hunk header is off

func applyUnifiedPatch(oldContent, unifiedDiff string) (string, error) {
	// ── pre-flight checks ────────────────────────────────────────────────────
	if strings.Contains(strings.TrimSpace(unifiedDiff), "*** Begin Patch") {
		return "", fmt.Errorf(
			"patch mode requires a standard unified diff in `unified_diff`; " +
				"detected Begin/End Patch wrapper format. " +
				"Convert to unified diff with file headers (---/+++) and hunk " +
				"headers (@@ -old,+new @@), or use mode=\"replace\"/\"line_edit\".")
	}

	// Normalise CRLF → LF so every split is clean.
	normalised := strings.ReplaceAll(unifiedDiff, "\r\n", "\n")
	rawLines := strings.Split(normalised, "\n")

	// ── parse hunks ──────────────────────────────────────────────────────────
	type hunk struct {
		oldStart int      // 1-indexed; only the start is trusted
		lines    []string // raw body lines (including leading ' ' / '-' / '+')
	}
	var hunks []hunk

	i := 0
	for i < len(rawLines) {
		line := rawLines[i]
		// Skip file headers and git metadata.
		if strings.HasPrefix(line, "--- ") || strings.HasPrefix(line, "+++ ") ||
			strings.HasPrefix(line, "diff ") || strings.HasPrefix(line, "index ") {
			i++
			continue
		}
		if !strings.HasPrefix(line, "@@") {
			i++
			continue
		}

		// Parse only the start line from the hunk header; ignore counts.
		oldStart, err := parseHunkHeaderStart(line)
		if err != nil {
			return "", fmt.Errorf("hunk %d: %w", len(hunks)+1, err)
		}
		if oldStart < 1 {
			return "", fmt.Errorf(
				"hunk %d: invalid hunk header line number %d (must be >= 1). "+
					"Unified diff line numbers are 1-indexed.",
				len(hunks)+1, oldStart)
		}
		i++

		var body []string
		for i < len(rawLines) {
			h := rawLines[i]
			if strings.HasPrefix(h, "@@") {
				break
			}
			if h == `\ No newline at end of file` {
				i++
				continue
			}
			body = append(body, h)
			i++
		}
		// Drop trailing blank lines that some diff tools append.
		for len(body) > 0 && body[len(body)-1] == "" {
			body = body[:len(body)-1]
		}
		hunks = append(hunks, hunk{oldStart: oldStart, lines: body})
	}

	if len(hunks) == 0 {
		return "", fmt.Errorf(
			"patch mode could not find any hunks in `unified_diff`. " +
				"Provide a standard unified diff with hunk headers like @@ -old,+new @@.")
	}

	// ── apply hunks ──────────────────────────────────────────────────────────
	oldLines := splitContentLines(oldContent)
	out := make([]string, 0, len(oldLines)+32)
	oldPos := 0 // 0-indexed cursor into oldLines

	for hIdx, h := range hunks {
		// hunkStart is the 0-indexed position the hunk nominally targets.
		hunkStart := h.oldStart - 1

		// ── fuzz search: find the real anchor ────────────────────────────────
		// Collect all context/delete lines from the hunk body (the lines that
		// must match the file).  We use them to search for the best matching
		// window within ±patchFuzz of the stated position.
		anchorPos, fuzzDelta, err := findHunkAnchor(oldLines, oldPos, hunkStart, h.lines, patchFuzz)
		if err != nil {
			return "", fmt.Errorf("hunk %d (near line %d): %w", hIdx+1, h.oldStart, err)
		}

		// Copy unchanged lines between the previous hunk and this one's anchor.
		if anchorPos < oldPos {
			return "", fmt.Errorf(
				"hunk %d: overlapping or out-of-order "+
					"(hunk anchored at line %d, current pos %d)",
				hIdx+1, anchorPos+1, oldPos+1)
		}
		out = append(out, oldLines[oldPos:anchorPos]...)
		oldPos = anchorPos

		// Provide a hint in errors if fuzz was used.
		fuzzNote := ""
		if fuzzDelta != 0 {
			fuzzNote = fmt.Sprintf(" (applied with offset %+d)", fuzzDelta)
		}

		// Apply each body line.
		for _, hl := range h.lines {
			if len(hl) == 0 {
				// Bare empty line: treat as a context line.
				if oldPos >= len(oldLines) {
					return "", fmt.Errorf(
						"hunk %d%s: context line at old pos %d beyond file",
						hIdx+1, fuzzNote, oldPos+1)
				}
				out = append(out, oldLines[oldPos])
				oldPos++
				continue
			}
			prefix := hl[0]
			text := ""
			if len(hl) > 1 {
				text = hl[1:]
			}
			switch prefix {
			case ' ':
				// Context line: consume from file without checking (anchor
				// already validated the pattern; individual context lines may
				// still have minor whitespace drift).
				if oldPos >= len(oldLines) {
					return "", fmt.Errorf(
						"hunk %d%s: context line at old pos %d beyond file",
						hIdx+1, fuzzNote, oldPos+1)
				}
				out = append(out, oldLines[oldPos])
				oldPos++
			case '-':
				// Delete line: must exist in the file at this position.
				if oldPos >= len(oldLines) {
					return "", fmt.Errorf(
						"hunk %d%s: delete line at old pos %d beyond file",
						hIdx+1, fuzzNote, oldPos+1)
				}
				if !linesMatch(oldLines[oldPos], text) {
					return "", fmt.Errorf(
						"hunk %d%s: delete mismatch at line %d\n  expected: %q\n  got:      %q",
						hIdx+1, fuzzNote, oldPos+1, text, oldLines[oldPos])
				}
				oldPos++ // consume, do not emit
			case '+':
				out = append(out, text)
			default:
				return "", fmt.Errorf(
					"hunk %d: unrecognised line prefix %q",
					hIdx+1, string(prefix))
			}
		}
	}

	// Copy any trailing unchanged lines.
	out = append(out, oldLines[oldPos:]...)

	result := strings.Join(out, "\n")
	if strings.HasSuffix(oldContent, "\n") {
		result += "\n"
	}
	return result, nil
}

// findHunkAnchor locates the best starting position in oldLines for a hunk.
//
// It extracts the sequence of "anchor lines" (context and delete lines) from
// the hunk body and attempts to match them at position hunkStart.  If that
// fails it searches ±fuzz lines.  The function returns the 0-indexed anchor
// position and the delta applied (0 = exact match), or an error if no window
// matched.
//
// Matching uses linesMatch (trailing-whitespace tolerant) so that minor
// whitespace drift in context lines does not cause spurious failures.
// patchAnchor is a single context-or-delete line extracted from a hunk body
// and used to locate the correct application position in the file.
type patchAnchor struct {
	text    string
	bodyIdx int // index in the hunk body slice (used in error messages)
}

func findHunkAnchor(oldLines []string, fromPos, hunkStart int, body []string, fuzz int) (anchorPos, delta int, err error) {
	// Build anchor sequence: all context (' ') and delete ('-') lines, in order.
	var anchors []patchAnchor
	for bi, bl := range body {
		if len(bl) == 0 {
			anchors = append(anchors, patchAnchor{"", bi})
			continue
		}
		switch bl[0] {
		case ' ':
			t := ""
			if len(bl) > 1 {
				t = bl[1:]
			}
			anchors = append(anchors, patchAnchor{t, bi})
		case '-':
			t := ""
			if len(bl) > 1 {
				t = bl[1:]
			}
			anchors = append(anchors, patchAnchor{t, bi})
		case '+':
			// insertions don't anchor
		}
	}

	if len(anchors) == 0 {
		// Pure-insertion hunk: no context to match, just land at hunkStart.
		pos := hunkStart
		if pos < fromPos {
			pos = fromPos
		}
		if pos > len(oldLines) {
			pos = len(oldLines)
		}
		return pos, pos - hunkStart, nil
	}

	// Try hunkStart first, then ±fuzz in alternating order.
	candidates := []int{hunkStart}
	for d := 1; d <= fuzz; d++ {
		candidates = append(candidates, hunkStart+d, hunkStart-d)
	}

	for _, candidate := range candidates {
		if candidate < fromPos || candidate < 0 || candidate > len(oldLines) {
			continue
		}
		if matchesAt(oldLines, candidate, anchors) {
			return candidate, candidate - hunkStart, nil
		}
	}

	// Build a helpful error: show the first anchor line and what the file has.
	firstAnchor := anchors[0].text
	fileAt := "(beyond end of file)"
	if hunkStart < len(oldLines) {
		fileAt = fmt.Sprintf("%q", oldLines[hunkStart])
	}
	return 0, 0, fmt.Errorf(
		"context/delete line not found within ±%d lines of stated position %d\n"+
			"  diff expects: %q\n"+
			"  file has at line %d: %s\n"+
			"  Tip: read the file with include_line_numbers=true to verify line "+
			"numbers, then regenerate the diff.",
		fuzz, hunkStart+1, firstAnchor, hunkStart+1, fileAt)
}

// matchesAt reports whether anchors (in order) match oldLines starting at pos,
// using linesMatch for each comparison.
func matchesAt(oldLines []string, pos int, anchors []patchAnchor) bool {
	fileIdx := pos
	for _, a := range anchors {
		if a.text == "" {
			// empty context anchor: consume a file line unconditionally
			fileIdx++
			continue
		}
		if fileIdx >= len(oldLines) {
			return false
		}
		if !linesMatch(oldLines[fileIdx], a.text) {
			return false
		}
		fileIdx++
	}
	return true
}

// linesMatch compares two lines with trailing-whitespace tolerance.
// Exact match is tried first; if that fails, both sides are right-trimmed.
func linesMatch(fileLine, diffLine string) bool {
	if fileLine == diffLine {
		return true
	}
	return strings.TrimRight(fileLine, " \t") == strings.TrimRight(diffLine, " \t")
}

// parseHunkHeaderStart extracts only the start line from a unified diff hunk
// header of the form "@@ -start[,count] +start[,count] @@".
// The count fields are intentionally ignored (see applyUnifiedPatch).
func parseHunkHeaderStart(header string) (oldStart int, err error) {
	if !strings.HasPrefix(header, "@@") {
		return 0, fmt.Errorf(
			"invalid hunk header: %q. "+
				"Expected standard unified diff hunk header like @@ -oldStart,oldCount +newStart,newCount @@",
			header)
	}
	parts := strings.Fields(header) // ["@@", "-N,N", "+N,N", "@@", ...]
	if len(parts) < 3 {
		return 0, fmt.Errorf(
			"invalid hunk header: %q. Expected format: @@ -oldStart,oldCount +newStart,newCount @@",
			header)
	}
	oldPart := strings.TrimPrefix(parts[1], "-")
	start, _, err2 := parseHunkRange(oldPart)
	if err2 != nil {
		return 0, err2
	}
	return start, nil
}

func parseHunkRange(v string) (start int, count int, err error) {
	count = 1
	seg := strings.SplitN(v, ",", 2)
	if _, err = fmt.Sscanf(seg[0], "%d", &start); err != nil {
		return 0, 0, fmt.Errorf("invalid hunk range: %s", v)
	}
	if len(seg) == 2 {
		if _, err = fmt.Sscanf(seg[1], "%d", &count); err != nil {
			return 0, 0, fmt.Errorf("invalid hunk range count: %s", v)
		}
	}
	return start, count, nil
}

// ── display diff (go-udiff) ──────────────────────────────────────────────────
//
// computeDisplayDiff uses go-udiff (the same library used by gopls) to
// produce a line-level diff summary.  It returns:
//   - insertions / deletions: line counts (for the summary header)
//   - changedRangeStrs: new-file line ranges that changed (e.g. ["12-18"])
//   - diffText: a compact "Changes:" block capped at maxDiffLines lines
//
// go-udiff.Lines() uses a two-sided LCS algorithm that is both memory-safe
// for large files and produces minimal edit scripts without the O(n²) trace
// allocation of the previous hand-rolled Myers implementation.

const maxDiffLines = 80

func computeDisplayDiff(oldContent, newContent string) (insertions, deletions int, changedRangeStrs []string, diffText string) {
	if oldContent == newContent {
		return 0, 0, nil, ""
	}

	edits := udiff.Lines(oldContent, newContent)
	if len(edits) == 0 {
		return 0, 0, nil, ""
	}

	unified, err := udiff.ToUnifiedDiff("old", "new", oldContent, edits, 0)
	if err != nil {
		// Defensive: fall back to no diff display if the library errors.
		return 0, 0, nil, ""
	}

	// Walk hunks to count insertions/deletions and collect changed new-file ranges.
	// Independent line counters are used — we never mutate the hunk structs.
	type lineRange struct{ start, end int }
	var newRanges []lineRange

	var diffLines []string
	for _, h := range unified.Hunks {
		oldLine := h.FromLine // 1-indexed cursor into the old file
		newLine := h.ToLine  // 1-indexed cursor into the new file
		var rangeStart int

		for _, l := range h.Lines {
			switch l.Kind {
			case udiff.Delete:
				deletions++
				diffLines = append(diffLines, fmt.Sprintf("- %4d | %s", oldLine, strings.TrimSuffix(l.Content, "\n")))
				oldLine++
			case udiff.Insert:
				insertions++
				if rangeStart == 0 {
					rangeStart = newLine
				}
				diffLines = append(diffLines, fmt.Sprintf("+ %4d | %s", newLine, strings.TrimSuffix(l.Content, "\n")))
				newLine++
			case udiff.Equal:
				if rangeStart > 0 {
					newRanges = append(newRanges, lineRange{rangeStart, newLine - 1})
					rangeStart = 0
				}
				oldLine++
				newLine++
			}
		}
		if rangeStart > 0 {
			newRanges = append(newRanges, lineRange{rangeStart, newLine - 1})
		}
	}

	// Format changed ranges.
	for _, r := range newRanges {
		if r.start == r.end {
			changedRangeStrs = append(changedRangeStrs, fmt.Sprintf("%d", r.start))
		} else {
			changedRangeStrs = append(changedRangeStrs, fmt.Sprintf("%d-%d", r.start, r.end))
		}
	}

	// Build compact diff block.
	if len(diffLines) > 0 {
		hidden := 0
		if len(diffLines) > maxDiffLines {
			hidden = len(diffLines) - maxDiffLines
			diffLines = diffLines[:maxDiffLines]
		}
		var sb strings.Builder
		sb.WriteString("Changes:\n")
		sb.WriteString(strings.Join(diffLines, "\n"))
		if hidden > 0 {
			sb.WriteString(fmt.Sprintf("\n... (%d more diff lines)", hidden))
		}
		diffText = sb.String()
	}

	return insertions, deletions, changedRangeStrs, diffText
}

// ── shared line utilities ────────────────────────────────────────────────────

// splitContentLines splits content into lines, stripping the final empty
// element that strings.Split produces when content ends with "\n".
func splitContentLines(content string) []string {
	if content == "" {
		return []string{}
	}
	lines := strings.Split(content, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// countLines returns the number of lines in content, consistent with
// splitContentLines (trailing newline does not add an extra line).
func countLines(content string) int {
	return len(splitContentLines(content))
}
