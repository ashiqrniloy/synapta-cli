package tools

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
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
- overwrite (default): Replace the entire file with new content. Use for new files or full rewrites. Requires: path, content.
- replace: Find and replace an exact literal string. Requires: path, find, content (the replacement text). Optional: expected_matches (safety check).
- replace_regex: Find and replace using a RE2 regex. Requires: path, find (regex pattern), content (replacement). Optional: expected_matches.
- line_edit: Replace a range of lines (1-indexed, inclusive). Requires: path, start_line, end_line, content (new lines to substitute in).
- patch: Apply a unified diff. Requires: path, unified_diff.

IMPORTANT: Never use bash with sed/awk/python to edit files. Always use this write tool for all file modifications.

EXAMPLES:
  Replace a function: {"path":"foo.go","mode":"replace","find":"func old()","content":"func new()"}
  Edit lines 5-8:     {"path":"foo.go","mode":"line_edit","start_line":5,"end_line":8,"content":"new line 5\nnew line 6\n"}
  New file:           {"path":"bar.go","mode":"overwrite","content":"package main\n"}`
}

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

type WriteDetails struct {
	Mode                    WriteMode   `json:"mode"`
	DryRun                  bool        `json:"dryRun"`
	Changed                 bool        `json:"changed"`
	AppliedMatches          int         `json:"appliedMatches,omitempty"`
	ExpectedMatches         *int        `json:"expectedMatches,omitempty"`
	MaxReplacements         *int        `json:"maxReplacements,omitempty"`
	StartLine               *int        `json:"startLine,omitempty"`
	EndLine                 *int        `json:"endLine,omitempty"`
	BytesBefore             int         `json:"bytesBefore"`
	BytesAfter              int         `json:"bytesAfter"`
	Truncation              *Truncation `json:"truncation,omitempty"`
	PreserveTrailingNewline bool        `json:"preserveTrailingNewline,omitempty"`
}

func (t *WriteTool) Execute(ctx context.Context, in WriteInput) (Result, error) {
	if strings.TrimSpace(in.Path) == "" {
		return Result{}, fmt.Errorf("path is required")
	}

	absPath := resolveToCwd(in.Path, t.cwd)
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

			if plan.OldExists {
				if err := applyExistingFileWithPatch(ctx, absPath, []byte(plan.OldContent), []byte(plan.NewContent)); err != nil {
					return err
				}
			} else {
				if err := writeNewFileWithCat(ctx, absPath, []byte(plan.NewContent)); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			return Result{}, err
		}
	}

	changed := plan.OldContent != plan.NewContent
	verb := "wrote"
	if plan.DryRun {
		verb = "planned write"
	}
	message := fmt.Sprintf("Successfully %s %d bytes to %s (mode=%s)", verb, len(plan.NewContent), in.Path, string(plan.Mode))
	if !changed {
		message = fmt.Sprintf("No changes for %s (mode=%s)", in.Path, string(plan.Mode))
	}
	if plan.Mode == WriteModeReplace || plan.Mode == WriteModeReplaceRegex {
		message += fmt.Sprintf("\nApplied matches: %d", plan.AppliedMatches)
		if plan.ExpectedMatches != nil {
			message += fmt.Sprintf(" (expected %d)", *plan.ExpectedMatches)
		}
		if plan.MaxReplacements != nil {
			message += fmt.Sprintf(" (max_replacements=%d)", *plan.MaxReplacements)
		}
	}
	if plan.Mode == WriteModeLineEdit && plan.StartLine != nil && plan.EndLine != nil {
		message += fmt.Sprintf("\nLine range: %d-%d", *plan.StartLine, *plan.EndLine)
	}

	diffSummary := buildWriteDiffSummary(in.Path, plan.OldContent, plan.NewContent)
	if diffSummary != "" {
		message += "\n\n" + diffSummary
	}

	preview, trunc := truncateHead(plan.NewContent, 60, 8*1024)
	if strings.TrimSpace(preview) != "" {
		message += "\n\n--- file preview ---\n" + preview
		if trunc.Truncated {
			message += fmt.Sprintf("\n\n[Preview truncated to %d lines / %s]", trunc.OutputLines, formatSize(trunc.OutputBytes))
		}
	}

	details := WriteDetails{
		Mode:                    plan.Mode,
		DryRun:                  plan.DryRun,
		Changed:                 changed,
		AppliedMatches:          plan.AppliedMatches,
		ExpectedMatches:         plan.ExpectedMatches,
		MaxReplacements:         plan.MaxReplacements,
		StartLine:               plan.StartLine,
		EndLine:                 plan.EndLine,
		BytesBefore:             len(plan.OldContent),
		BytesAfter:              len(plan.NewContent),
		PreserveTrailingNewline: plan.PreserveTrailingNL,
	}
	if trunc.Truncated {
		details.Truncation = &trunc
	}

	return Result{
		Content: []ContentPart{{
			Type: ContentPartText,
			Text: message,
		}},
		Details: details,
	}, nil
}

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
		plan.NewContent = in.Content
	case WriteModeReplace:
		if !oldExists {
			return writePlan{}, fmt.Errorf("replace mode requires existing file")
		}
		newContent, count, err := applyStringReplace(oldContent, in.Find, in.Replace, in.ExpectedMatches, in.MaxReplacements)
		if err != nil {
			return writePlan{}, err
		}
		plan.ExpectedMatches = in.ExpectedMatches
		plan.MaxReplacements = in.MaxReplacements
		plan.AppliedMatches = count
		plan.NewContent = newContent
	case WriteModeReplaceRegex:
		if !oldExists {
			return writePlan{}, fmt.Errorf("replace_regex mode requires existing file")
		}
		newContent, count, err := applyRegexReplace(oldContent, in.Find, in.Replace, in.ExpectedMatches, in.MaxReplacements)
		if err != nil {
			return writePlan{}, err
		}
		plan.ExpectedMatches = in.ExpectedMatches
		plan.MaxReplacements = in.MaxReplacements
		plan.AppliedMatches = count
		plan.NewContent = newContent
	case WriteModeLineEdit:
		if !oldExists {
			return writePlan{}, fmt.Errorf("line_edit mode requires existing file")
		}
		if in.StartLine == nil || in.EndLine == nil {
			return writePlan{}, fmt.Errorf("start_line and end_line are required for line_edit mode")
		}
		if *in.StartLine < 1 || *in.EndLine < *in.StartLine {
			return writePlan{}, fmt.Errorf("invalid line range: %d-%d", *in.StartLine, *in.EndLine)
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
			return writePlan{}, fmt.Errorf("unified_diff is required for patch mode")
		}
		newContent, err := applyUnifiedPatchToContent(oldContent, in.UnifiedDiff)
		if err != nil {
			return writePlan{}, err
		}
		plan.NewContent = newContent
	default:
		return writePlan{}, fmt.Errorf("unsupported write mode: %s", mode)
	}

	return plan, nil
}

func applyStringReplace(oldContent, find, replace string, expectedMatches, maxReplacements *int) (string, int, error) {
	if find == "" {
		return "", 0, fmt.Errorf("find is required for replace mode")
	}
	count := strings.Count(oldContent, find)
	if expectedMatches != nil && count != *expectedMatches {
		return "", 0, fmt.Errorf("replace expected %d matches, found %d", *expectedMatches, count)
	}
	if expectedMatches == nil && count == 0 {
		return "", 0, fmt.Errorf("replace found no matches")
	}
	if maxReplacements != nil {
		if *maxReplacements < 0 {
			return "", 0, fmt.Errorf("max_replacements must be >= 0")
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
		return "", 0, fmt.Errorf("find is required for replace_regex mode")
	}
	re, err := regexp.Compile(find)
	if err != nil {
		return "", 0, fmt.Errorf("invalid regex pattern: %w", err)
	}
	matches := re.FindAllStringSubmatchIndex(oldContent, -1)
	count := len(matches)
	if expectedMatches != nil && count != *expectedMatches {
		return "", 0, fmt.Errorf("replace_regex expected %d matches, found %d", *expectedMatches, count)
	}
	if expectedMatches == nil && count == 0 {
		return "", 0, fmt.Errorf("replace_regex found no matches")
	}
	if maxReplacements != nil {
		if *maxReplacements < 0 {
			return "", 0, fmt.Errorf("max_replacements must be >= 0")
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

func applyLineEdit(oldContent string, startLine, endLine int, replacement string, preserveTrailingNL bool) (string, error) {
	endsWithNL := strings.HasSuffix(oldContent, "\n")
	oldLines := splitContentLines(oldContent)
	if endLine > len(oldLines) {
		return "", fmt.Errorf("line range %d-%d is out of bounds (file has %d lines)", startLine, endLine, len(oldLines))
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

func applyUnifiedPatchToContent(oldContent, unifiedDiff string) (string, error) {
	lines := strings.Split(strings.ReplaceAll(unifiedDiff, "\r\n", "\n"), "\n")
	oldLines := splitContentLines(oldContent)
	var out []string
	oldPos := 0
	i := 0

	for i < len(lines) {
		line := lines[i]
		if strings.HasPrefix(line, "--- ") || strings.HasPrefix(line, "+++ ") || strings.HasPrefix(line, "diff ") || strings.HasPrefix(line, "index ") {
			i++
			continue
		}
		if !strings.HasPrefix(line, "@@") {
			i++
			continue
		}

		oldStart, _, err := parseUnifiedHunkHeader(line)
		if err != nil {
			return "", err
		}
		target := oldStart - 1
		if target < oldPos || target > len(oldLines) {
			return "", fmt.Errorf("invalid hunk target line %d", oldStart)
		}
		out = append(out, oldLines[oldPos:target]...)
		oldPos = target
		i++

		for i < len(lines) {
			h := lines[i]
			if strings.HasPrefix(h, "@@") {
				break
			}
			if len(h) == 0 {
				i++
				continue
			}
			if h == "\\ No newline at end of file" {
				i++
				continue
			}
			prefix := h[0]
			text := ""
			if len(h) > 1 {
				text = h[1:]
			}
			switch prefix {
			case ' ':
				if oldPos >= len(oldLines) || oldLines[oldPos] != text {
					return "", fmt.Errorf("patch context mismatch at old line %d", oldPos+1)
				}
				out = append(out, text)
				oldPos++
			case '-':
				if oldPos >= len(oldLines) || oldLines[oldPos] != text {
					return "", fmt.Errorf("patch delete mismatch at old line %d", oldPos+1)
				}
				oldPos++
			case '+':
				out = append(out, text)
			default:
				return "", fmt.Errorf("invalid patch hunk line prefix: %q", string(prefix))
			}
			i++
		}
	}

	out = append(out, oldLines[oldPos:]...)
	result := strings.Join(out, "\n")
	if strings.HasSuffix(oldContent, "\n") {
		result += "\n"
	}
	return result, nil
}

func parseUnifiedHunkHeader(header string) (oldStart int, newStart int, err error) {
	// format: @@ -a,b +c,d @@ optional
	if !strings.HasPrefix(header, "@@") {
		return 0, 0, fmt.Errorf("invalid hunk header: %s", header)
	}
	parts := strings.Split(header, " ")
	if len(parts) < 3 {
		return 0, 0, fmt.Errorf("invalid hunk header: %s", header)
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

func writeNewFileWithCat(ctx context.Context, absPath string, newContent []byte) error {
	if _, err := exec.LookPath("cat"); err != nil {
		return fmt.Errorf("cat command is required to create new files: %w", err)
	}

	tmp, err := os.CreateTemp(filepath.Dir(absPath), ".synapta-new-*")
	if err != nil {
		return fmt.Errorf("creating temp file for new content: %w", err)
	}
	tmpPath := tmp.Name()
	_ = tmp.Close()
	defer os.Remove(tmpPath)

	if err := os.WriteFile(tmpPath, newContent, 0o644); err != nil {
		return fmt.Errorf("writing temp content: %w", err)
	}

	dst, err := os.OpenFile(absPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("opening destination file: %w", err)
	}
	defer dst.Close()

	catCmd := exec.CommandContext(ctx, "cat", tmpPath)
	catCmd.Stdout = dst
	var stderr bytes.Buffer
	catCmd.Stderr = &stderr
	if err := catCmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return fmt.Errorf("writing new file with cat: %w: %s", err, msg)
		}
		return fmt.Errorf("writing new file with cat: %w", err)
	}
	return nil
}

func applyExistingFileWithPatch(ctx context.Context, absPath string, oldContent, newContent []byte) error {
	if bytes.Equal(oldContent, newContent) {
		return nil
	}
	if _, err := exec.LookPath("patch"); err != nil {
		return fmt.Errorf("gnu patch is required to edit existing files: %w", err)
	}
	if _, err := exec.LookPath("diff"); err != nil {
		return fmt.Errorf("diff command is required to generate patch: %w", err)
	}

	dir := filepath.Dir(absPath)
	oldTmp, err := os.CreateTemp(dir, ".synapta-old-*")
	if err != nil {
		return fmt.Errorf("creating temp old file: %w", err)
	}
	oldTmpPath := oldTmp.Name()
	_ = oldTmp.Close()
	defer os.Remove(oldTmpPath)

	newTmp, err := os.CreateTemp(dir, ".synapta-new-*")
	if err != nil {
		return fmt.Errorf("creating temp new file: %w", err)
	}
	newTmpPath := newTmp.Name()
	_ = newTmp.Close()
	defer os.Remove(newTmpPath)

	if err := os.WriteFile(oldTmpPath, oldContent, 0o644); err != nil {
		return fmt.Errorf("writing temp old content: %w", err)
	}
	if err := os.WriteFile(newTmpPath, newContent, 0o644); err != nil {
		return fmt.Errorf("writing temp new content: %w", err)
	}

	var patchBuf bytes.Buffer
	var diffErr bytes.Buffer
	diffCmd := exec.CommandContext(ctx, "diff", "-u", oldTmpPath, newTmpPath)
	diffCmd.Stdout = &patchBuf
	diffCmd.Stderr = &diffErr
	err = diffCmd.Run()
	if err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 {
			msg := strings.TrimSpace(diffErr.String())
			if msg != "" {
				return fmt.Errorf("generating patch: %w: %s", err, msg)
			}
			return fmt.Errorf("generating patch: %w", err)
		}
	}

	if patchBuf.Len() == 0 {
		return nil
	}

	var patchErr bytes.Buffer
	patchCmd := exec.CommandContext(ctx, "patch", "--silent", "--output", absPath, oldTmpPath)
	patchCmd.Stdin = bytes.NewReader(patchBuf.Bytes())
	patchCmd.Stderr = &patchErr
	if err := patchCmd.Run(); err != nil {
		msg := strings.TrimSpace(patchErr.String())
		if msg != "" {
			return fmt.Errorf("applying patch: %w: %s", err, msg)
		}
		return fmt.Errorf("applying patch: %w", err)
	}

	return nil
}

func buildWriteDiffSummary(path, oldContent, newContent string) string {
	oldLines := splitContentLines(oldContent)
	newLines := splitContentLines(newContent)
	ops := computeLineDiffOps(oldLines, newLines)

	if len(ops) == 0 {
		return fmt.Sprintf("--- write summary ---\nFile: %s\nNo line changes detected", path)
	}

	oldRanges, newRanges := collectChangedRanges(ops)

	const maxShownDiffLines = 240
	diffLines := make([]string, 0, len(ops))
	for _, op := range ops {
		switch op.Kind {
		case diffDel:
			diffLines = append(diffLines, fmt.Sprintf("- %4d | %s", op.OldLine, op.Text))
		case diffAdd:
			diffLines = append(diffLines, fmt.Sprintf("+ %4d | %s", op.NewLine, op.Text))
		}
	}

	hidden := 0
	if len(diffLines) > maxShownDiffLines {
		hidden = len(diffLines) - maxShownDiffLines
		diffLines = diffLines[:maxShownDiffLines]
	}

	var b strings.Builder
	b.WriteString("--- write summary ---\n")
	b.WriteString("File: ")
	b.WriteString(path)
	b.WriteString("\n")
	b.WriteString("Changed ranges (old): ")
	b.WriteString(formatRanges(oldRanges))
	b.WriteString("\n")
	b.WriteString("Changed ranges (new): ")
	b.WriteString(formatRanges(newRanges))
	b.WriteString("\n\n--- line diff ---\n")
	b.WriteString(strings.Join(diffLines, "\n"))
	if hidden > 0 {
		b.WriteString(fmt.Sprintf("\n... (%d more diff lines)", hidden))
	}

	return b.String()
}

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

func computeLineDiffOps(oldLines, newLines []string) []diffOp {
	n, m := len(oldLines), len(newLines)
	if n == 0 && m == 0 {
		return nil
	}

	// Guard against huge quadratic matrices; fallback to full replacement summary.
	if n*m > 2_000_000 {
		ops := make([]diffOp, 0, n+m)
		for i, line := range oldLines {
			ops = append(ops, diffOp{Kind: diffDel, Text: line, OldLine: i + 1})
		}
		for i, line := range newLines {
			ops = append(ops, diffOp{Kind: diffAdd, Text: line, NewLine: i + 1})
		}
		return ops
	}

	dp := make([][]int, n+1)
	for i := range dp {
		dp[i] = make([]int, m+1)
	}

	for i := 1; i <= n; i++ {
		for j := 1; j <= m; j++ {
			if oldLines[i-1] == newLines[j-1] {
				dp[i][j] = dp[i-1][j-1] + 1
			} else if dp[i-1][j] >= dp[i][j-1] {
				dp[i][j] = dp[i-1][j]
			} else {
				dp[i][j] = dp[i][j-1]
			}
		}
	}

	rev := make([]diffOp, 0, n+m)
	i, j := n, m
	for i > 0 || j > 0 {
		if i > 0 && j > 0 && oldLines[i-1] == newLines[j-1] {
			rev = append(rev, diffOp{Kind: diffEqual, Text: oldLines[i-1]})
			i--
			j--
			continue
		}
		if j > 0 && (i == 0 || dp[i][j-1] > dp[i-1][j]) {
			rev = append(rev, diffOp{Kind: diffAdd, Text: newLines[j-1]})
			j--
			continue
		}
		if i > 0 {
			rev = append(rev, diffOp{Kind: diffDel, Text: oldLines[i-1]})
			i--
		}
	}

	ops := make([]diffOp, 0, len(rev))
	for k := len(rev) - 1; k >= 0; k-- {
		ops = append(ops, rev[k])
	}

	oldLine, newLine := 1, 1
	for idx := range ops {
		switch ops[idx].Kind {
		case diffEqual:
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

	filtered := make([]diffOp, 0, len(ops))
	for _, op := range ops {
		if op.Kind != diffEqual {
			filtered = append(filtered, op)
		}
	}
	return filtered
}

func collectChangedRanges(ops []diffOp) (oldRanges []lineRange, newRanges []lineRange) {
	oldRanges = collectRangesForKind(ops, diffDel)
	newRanges = collectRangesForKind(ops, diffAdd)
	return oldRanges, newRanges
}

func collectRangesForKind(ops []diffOp, kind diffOpKind) []lineRange {
	ranges := make([]lineRange, 0)
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

func formatRanges(ranges []lineRange) string {
	if len(ranges) == 0 {
		return "none"
	}
	parts := make([]string, 0, len(ranges))
	for _, r := range ranges {
		if r.Start == r.End {
			parts = append(parts, fmt.Sprintf("%d", r.Start))
		} else {
			parts = append(parts, fmt.Sprintf("%d-%d", r.Start, r.End))
		}
	}
	return strings.Join(parts, ", ")
}
