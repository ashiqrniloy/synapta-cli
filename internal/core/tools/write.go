package tools

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
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
- overwrite (default): Replace the entire file with new content.
  Requires: path, content.
  Example: {"path":"internal/core/chat.go","mode":"overwrite","content":"package core\n"}

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

- patch: Apply a standard unified diff to an existing file.
  Requires: path, unified_diff.
  unified_diff is mandatory for patch mode.
  The patch must be a standard unified diff and include hunk headers like @@ -old,+new @@.
  Example: {"path":"internal/core/chat.go","mode":"patch","unified_diff":"--- a/internal/core/chat.go\n+++ b/internal/core/chat.go\n@@ -42,7 +42,8 @@\n old line\n+new line\n"}

IMPORTANT:
- Never use bash with sed/awk/python to edit files. Always use this write tool for file modifications.
- For patch mode, custom wrappers like "*** Begin Patch" / "*** End Patch" are not accepted unless converted to unified diff.
- For replace/replace_regex: always set expected_matches=1 when you intend to change exactly one occurrence. This prevents silent partial edits.
- overwrite with empty content will be rejected. Use mode="overwrite" only when you have the complete new file content ready.

PARAMETERS:
- path: File to edit (relative or absolute). Required.
- mode: Edit strategy (default: overwrite). See MODES above.
- content: New file content (overwrite) or replacement text (replace/replace_regex/line_edit).
- find: Exact text to match (replace) or RE2 regex pattern (replace_regex). Required for those modes.
- expected_matches: Expected number of matches (replace/replace_regex); tool errors if actual count differs. Recommended for safety.
- max_replacements: Limit how many replacements to apply (replace/replace_regex).
- start_line: 1-indexed start line, inclusive (line_edit).
- end_line: 1-indexed end line, inclusive (line_edit).
- unified_diff: Standard unified diff with hunk headers (patch mode). Required for patch.
- preserve_trailing_newline: Keep the original file's trailing newline in line_edit (default true).
- dry_run: Plan and diff without writing changes. Returns what would change.
- include_preview: When true, append a head-truncated preview of the result. Default false.`
}

// ── diff primitives ──────────────────────────────────────────────────────────

type diffOpKind string

const (
	diffEqual diffOpKind = "equal"
	diffDel   diffOpKind = "del"
	diffAdd   diffOpKind = "add"
)

type diffOp struct {
	Kind    diffOpKind
	Text    string
	OldLine int
	NewLine int
}

type lineRange struct {
	Start int
	End   int
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

	// Compaction helpers
	SHA256After string `json:"sha256_after,omitempty"`

	// Optional mode-specific fields
	ExpectedMatches         *int `json:"expected_matches,omitempty"`
	MaxReplacements         *int `json:"max_replacements,omitempty"`
	StartLine               *int `json:"start_line,omitempty"`
	EndLine                 *int `json:"end_line,omitempty"`
	// PreserveTrailingNewline is only emitted for line_edit mode.
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

	// ── Diff ────────────────────────────────────────────────────────────────
	oldLines := splitContentLines(plan.OldContent)
	newLines := splitContentLines(plan.NewContent)
	ops := myersDiff(oldLines, newLines)

	insertions, deletions := 0, 0
	for _, op := range ops {
		switch op.Kind {
		case diffAdd:
			insertions++
		case diffDel:
			deletions++
		}
	}
	_, newRanges := collectChangedRanges(ops)
	changedRangeStrs := formatRangesSlice(newRanges)

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
	if changed && len(changedRangeStrs) > 0 {
		sb.WriteString(fmt.Sprintf("\nChanged ranges (new): %s", strings.Join(changedRangeStrs, ", ")))
	}

	// ── Diff lines (compact, only shown when preview not requested) ──────────
	const maxDiffLines = 80
	diffText := buildCompactDiff(ops, maxDiffLines)
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
	details := WriteDetails{
		Mode:            plan.Mode,
		DryRun:          plan.DryRun,
		Changed:         changed,
		Insertions:      insertions,
		Deletions:       deletions,
		ChangedRanges:   changedRangeStrs,
		AppliedMatches:  plan.AppliedMatches,
		LineCountBefore: len(oldLines),
		LineCountAfter:  len(newLines),
		BytesBefore:     len(plan.OldContent),
		BytesAfter:      len(plan.NewContent),
		SHA256After:     hashAfter,
	}
	// Emit mode-specific optional fields only when relevant.
	if plan.Mode == WriteModeReplace || plan.Mode == WriteModeReplaceRegex {
		details.ExpectedMatches = plan.ExpectedMatches
		details.MaxReplacements = plan.MaxReplacements
	}
	if plan.Mode == WriteModeLineEdit {
		details.StartLine = plan.StartLine
		details.EndLine = plan.EndLine
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
	case WriteModeReplace:
		if !oldExists {
			return writePlan{}, fmt.Errorf("replace mode requires an existing file. The target path does not exist. Use mode=\"overwrite\" to create a new file, or provide an existing file for mode=\"replace\".")
		}
		// Resolve replacement: prefer `content`, fall back to `replace` for compatibility.
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
		// Resolve replacement: prefer `content`, fall back to `replace` for compatibility.
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
	case WriteModePatch:
		if strings.TrimSpace(in.UnifiedDiff) == "" {
			return writePlan{}, fmt.Errorf("patch mode requires `unified_diff`. Provide a standard unified diff with hunk headers like @@ -old,+new @@. Example: {\"mode\":\"patch\",\"path\":\"internal/core/chat.go\",\"unified_diff\":\"--- a/internal/core/chat.go\\n+++ b/internal/core/chat.go\\n@@ -42,7 +42,8 @@\\n old line\\n+new line\\n\"}")
		}
		newContent, err := applyUnifiedPatchToContent(oldContent, in.UnifiedDiff)
		if err != nil {
			return writePlan{}, err
		}
		plan.NewContent = newContent
	default:
		return writePlan{}, fmt.Errorf("unsupported write mode %q. Supported modes: overwrite, replace, replace_regex, line_edit, patch", mode)
	}

	return plan, nil
}

// resolveReplacement returns the replacement text for replace/replace_regex modes.
// `content` is the canonical field; `replace` is accepted as an alias for compatibility.
// If both are set and differ, `content` wins and a warning is embedded in the error.
func resolveReplacement(in WriteInput) string {
	if in.Content != "" && in.Replace != "" && in.Content != in.Replace {
		// Both set and differ — `content` takes precedence; this is a likely LLM mistake.
		// We still proceed with `content` rather than erroring to be forgiving.
		return in.Content
	}
	if in.Content != "" {
		return in.Content
	}
	// `replace` field provided without `content` — honour it for backward compat.
	return in.Replace
}

// ── writeFileDirect — pure-Go atomic write (no external cat/patch) ───────────

// writeFileDirect writes content to absPath via an adjacent temp file, then
// renames into place. This is atomic on POSIX and removes the dependency on
// external `cat` and `patch` binaries.
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

// ── patch mode — hardened pure-Go applicator ─────────────────────────────────
//
// Applies a standard unified diff entirely in Go. No external `patch` binary.
// Validates each hunk header and context line; returns a descriptive error on
// any mismatch so the agent gets actionable feedback rather than a silent
// wrong-content write.

func applyUnifiedPatchToContent(oldContent, unifiedDiff string) (string, error) {
	rawLines := strings.Split(strings.ReplaceAll(unifiedDiff, "\r\n", "\n"), "\n")
	if strings.Contains(strings.TrimSpace(unifiedDiff), "*** Begin Patch") {
		return "", fmt.Errorf("patch mode requires a standard unified diff in `unified_diff`; detected Begin/End Patch wrapper format. Convert to unified diff with file headers (---/+++) and hunk headers (@@ -old,+new @@), or use mode=\"replace\"/\"line_edit\".")
	}
	oldLines := splitContentLines(oldContent)

	// Parse into hunks first so we can validate before mutating.
	type hunk struct {
		oldStart int
		lines    []string // raw hunk body lines (including leading ' ', '-', '+')
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
		oldStart, _, err := parseUnifiedHunkHeader(line)
		if err != nil {
			return "", fmt.Errorf("hunk %d: %w", len(hunks)+1, err)
		}
		// Reject oldStart == 0: unified diffs are 1-indexed; zero means a
		// malformed or fabricated header that would corrupt the output.
		if oldStart < 1 {
			return "", fmt.Errorf("hunk %d: invalid hunk header line number %d (must be >= 1). Unified diff line numbers are 1-indexed.", len(hunks)+1, oldStart)
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
		// Strip trailing empty lines that git sometimes appends.
		for len(body) > 0 && body[len(body)-1] == "" {
			body = body[:len(body)-1]
		}
		hunks = append(hunks, hunk{oldStart: oldStart, lines: body})
	}

	if len(hunks) == 0 {
		trimmed := strings.TrimSpace(unifiedDiff)
		if strings.Contains(trimmed, "*** Begin Patch") {
			return "", fmt.Errorf("patch mode requires a standard unified diff in `unified_diff`; detected Begin/End Patch wrapper format. Convert to unified diff with file headers (---/+++) and hunk headers (@@ -old,+new @@), or use mode=\"replace\"/\"line_edit\".")
		}
		return "", fmt.Errorf("patch mode could not find any hunks in `unified_diff`. Provide a standard unified diff with hunk headers like @@ -old,+new @@.")
	}

	// Apply hunks sequentially.
	out := make([]string, 0, len(oldLines)+32)
	oldPos := 0 // 0-indexed cursor into oldLines

	for hIdx, h := range hunks {
		target := h.oldStart - 1 // convert to 0-indexed
		if target < oldPos {
			return "", fmt.Errorf("hunk %d: overlapping or out-of-order (target line %d, current pos %d)", hIdx+1, h.oldStart, oldPos+1)
		}
		if target > len(oldLines) {
			return "", fmt.Errorf("hunk %d: target line %d beyond end of file (%d lines)", hIdx+1, h.oldStart, len(oldLines))
		}
		// Copy unchanged lines before this hunk.
		out = append(out, oldLines[oldPos:target]...)
		oldPos = target

		for _, hl := range h.lines {
			if len(hl) == 0 {
				// Treat bare empty line as context (some tools emit them).
				if oldPos >= len(oldLines) {
					return "", fmt.Errorf("hunk %d: context line at old pos %d beyond file", hIdx+1, oldPos+1)
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
				// Context line — must match.
				if oldPos >= len(oldLines) {
					return "", fmt.Errorf("hunk %d: context line at old pos %d beyond file", hIdx+1, oldPos+1)
				}
				if oldLines[oldPos] != text {
					return "", fmt.Errorf("hunk %d: context mismatch at line %d\n  expected: %q\n  got:      %q",
						hIdx+1, oldPos+1, text, oldLines[oldPos])
				}
				out = append(out, text)
				oldPos++
			case '-':
				// Delete line — must match.
				if oldPos >= len(oldLines) {
					return "", fmt.Errorf("hunk %d: delete line at old pos %d beyond file", hIdx+1, oldPos+1)
				}
				if oldLines[oldPos] != text {
					return "", fmt.Errorf("hunk %d: delete mismatch at line %d\n  expected: %q\n  got:      %q",
						hIdx+1, oldPos+1, text, oldLines[oldPos])
				}
				oldPos++ // consume, do not emit
			case '+':
				out = append(out, text)
			default:
				return "", fmt.Errorf("hunk %d: unrecognised line prefix %q", hIdx+1, string(prefix))
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

func parseUnifiedHunkHeader(header string) (oldStart int, newStart int, err error) {
	if !strings.HasPrefix(header, "@@") {
		return 0, 0, fmt.Errorf("invalid hunk header: %q. Expected standard unified diff hunk header like @@ -oldStart,oldCount +newStart,newCount @@", header)
	}
	parts := strings.Split(header, " ")
	if len(parts) < 3 {
		return 0, 0, fmt.Errorf("invalid hunk header: %q. Expected format: @@ -oldStart,oldCount +newStart,newCount @@", header)
	}
	oldPart := strings.TrimPrefix(parts[1], "-")
	newPart := strings.TrimPrefix(parts[2], "+")
	oldStart, _, err = parseHunkRange(oldPart)
	if err != nil {
		return 0, 0, err
	}
	newStart, _, err = parseHunkRange(newPart)
	if err != nil {
		return 0, 0, err
	}
	return oldStart, newStart, nil
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

// ── Myers diff — O(nd) time, O(n+m) space ────────────────────────────────────
//
// Classic Myers shortest-edit-script algorithm. Replaces the old O(n*m) LCS
// DP which would allocate a 4 MB+ matrix for 1 k-line files and fell back to a
// useless "delete everything, add everything" summary beyond 2 M cells.
//
// This implementation uses the linear-space variant: it records the furthest-
// reaching diagonal at each edit distance and then backtracks.

func myersDiff(a, b []string) []diffOp {
	n, m := len(a), len(b)
	if n == 0 && m == 0 {
		return nil
	}
	if n == 0 {
		ops := make([]diffOp, m)
		for i, line := range b {
			ops[i] = diffOp{Kind: diffAdd, Text: line, NewLine: i + 1}
		}
		return ops
	}
	if m == 0 {
		ops := make([]diffOp, n)
		for i, line := range a {
			ops[i] = diffOp{Kind: diffDel, Text: line, OldLine: i + 1}
		}
		return ops
	}

	max := n + m
	size := 2*max + 1
	// v[k] = furthest x reached on diagonal k
	v := make([]int, size)
	// trace[d] = snapshot of v at edit distance d
	trace := make([][]int, 0, max+1)

	offset := max // index offset: real diagonal k → v[k+offset]

	for d := 0; d <= max; d++ {
		snap := make([]int, size)
		copy(snap, v)
		trace = append(trace, snap)

		for k := -d; k <= d; k += 2 {
			var x int
			if k == -d || (k != d && v[k-1+offset] < v[k+1+offset]) {
				x = v[k+1+offset] // move down
			} else {
				x = v[k-1+offset] + 1 // move right
			}
			y := x - k
			for x < n && y < m && a[x] == b[y] {
				x++
				y++
			}
			v[k+offset] = x
			if x >= n && y >= m {
				// Found shortest edit — backtrack.
				return myersBacktrack(a, b, trace, d, offset)
			}
		}
	}
	// Shouldn't reach here, but fall back gracefully.
	return myersFallback(a, b)
}

func myersBacktrack(a, b []string, trace [][]int, d, offset int) []diffOp {
	n, m := len(a), len(b)
	ops := make([]diffOp, 0, d*2+max(n, m))

	x, y := n, m
	for curD := d; curD > 0; curD-- {
		v := trace[curD]
		k := x - y

		var prevK int
		if k == -curD || (k != curD && v[k-1+offset] < v[k+1+offset]) {
			prevK = k + 1
		} else {
			prevK = k - 1
		}

		prevX := v[prevK+offset]
		prevY := prevX - prevK

		// Walk diagonal (equal lines) from (prevX,prevY) to start of snake.
		for x > prevX && y > prevY {
			x--
			y--
			ops = append(ops, diffOp{Kind: diffEqual, Text: a[x]})
		}

		if curD > 0 {
			if x == prevX {
				// y moved: insertion
				y--
				ops = append(ops, diffOp{Kind: diffAdd, Text: b[y]})
			} else {
				// x moved: deletion
				x--
				ops = append(ops, diffOp{Kind: diffDel, Text: a[x]})
			}
		}
	}
	// Handle any remaining snake at d=0.
	for x > 0 && y > 0 {
		x--
		y--
		ops = append(ops, diffOp{Kind: diffEqual, Text: a[x]})
	}

	// Reverse.
	for i, j := 0, len(ops)-1; i < j; i, j = i+1, j-1 {
		ops[i], ops[j] = ops[j], ops[i]
	}

	// Assign line numbers.
	oldLine, newLine := 1, 1
	for idx := range ops {
		switch ops[idx].Kind {
		case diffEqual:
			ops[idx].OldLine = oldLine
			ops[idx].NewLine = newLine
			oldLine++
			newLine++
		case diffDel:
			ops[idx].OldLine = oldLine
			oldLine++
		case diffAdd:
			ops[idx].NewLine = newLine
			newLine++
		}
	}
	return ops
}

func myersFallback(a, b []string) []diffOp {
	ops := make([]diffOp, 0, len(a)+len(b))
	for i, line := range a {
		ops = append(ops, diffOp{Kind: diffDel, Text: line, OldLine: i + 1})
	}
	for i, line := range b {
		ops = append(ops, diffOp{Kind: diffAdd, Text: line, NewLine: i + 1})
	}
	return ops
}

// ── diff summary helpers ──────────────────────────────────────────────────────

// buildCompactDiff renders only changed lines (no context), capped at maxLines.
func buildCompactDiff(ops []diffOp, maxLines int) string {
	var lines []string
	for _, op := range ops {
		switch op.Kind {
		case diffDel:
			lines = append(lines, fmt.Sprintf("- %4d | %s", op.OldLine, op.Text))
		case diffAdd:
			lines = append(lines, fmt.Sprintf("+ %4d | %s", op.NewLine, op.Text))
		}
	}
	if len(lines) == 0 {
		return ""
	}
	hidden := 0
	if len(lines) > maxLines {
		hidden = len(lines) - maxLines
		lines = lines[:maxLines]
	}
	var sb strings.Builder
	sb.WriteString("Changes:\n")
	sb.WriteString(strings.Join(lines, "\n"))
	if hidden > 0 {
		sb.WriteString(fmt.Sprintf("\n... (%d more diff lines)", hidden))
	}
	return sb.String()
}

func collectChangedRanges(ops []diffOp) (oldRanges []lineRange, newRanges []lineRange) {
	oldRanges = collectRangesForKind(ops, diffDel)
	newRanges = collectRangesForKind(ops, diffAdd)
	return oldRanges, newRanges
}

func collectRangesForKind(ops []diffOp, kind diffOpKind) []lineRange {
	var ranges []lineRange
	current := lineRange{Start: -1, End: -1}
	for _, op := range ops {
		if op.Kind != kind {
			if current.Start != -1 {
				ranges = append(ranges, current)
				current = lineRange{Start: -1, End: -1}
			}
			continue
		}
		lineNo := op.OldLine
		if kind == diffAdd {
			lineNo = op.NewLine
		}
		if lineNo <= 0 {
			continue
		}
		if current.Start == -1 {
			current = lineRange{Start: lineNo, End: lineNo}
			continue
		}
		if lineNo == current.End+1 {
			current.End = lineNo
		} else {
			ranges = append(ranges, current)
			current = lineRange{Start: lineNo, End: lineNo}
		}
	}
	if current.Start != -1 {
		ranges = append(ranges, current)
	}
	return ranges
}

func formatRangesSlice(ranges []lineRange) []string {
	if len(ranges) == 0 {
		return nil
	}
	out := make([]string, 0, len(ranges))
	for _, r := range ranges {
		if r.Start == r.End {
			out = append(out, fmt.Sprintf("%d", r.Start))
		} else {
			out = append(out, fmt.Sprintf("%d-%d", r.Start, r.End))
		}
	}
	return out
}

// ── shared line utilities ────────────────────────────────────────────────────

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
