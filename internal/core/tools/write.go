package tools

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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
	return "Write content to a file. Creates the file if it doesn't exist, overwrites if it does. Uses cat for new files and patch for existing files. Automatically creates parent directories."
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

func (t *WriteTool) Execute(ctx context.Context, in WriteInput) (Result, error) {
	if strings.TrimSpace(in.Path) == "" {
		return Result{}, fmt.Errorf("path is required")
	}

	absPath := resolveToCwd(in.Path, t.cwd)
	dir := filepath.Dir(absPath)

	oldContent, readErr := os.ReadFile(absPath)
	oldExists := readErr == nil
	if readErr != nil && !os.IsNotExist(readErr) {
		return Result{}, fmt.Errorf("reading existing file: %w", readErr)
	}
	newContent := []byte(in.Content)

	err := withFileMutationQueue(absPath, func() error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("creating parent dirs: %w", err)
		}

		if oldExists {
			if err := applyExistingFileWithPatch(ctx, absPath, oldContent, newContent); err != nil {
				return err
			}
		} else {
			if err := writeNewFileWithCat(ctx, absPath, newContent); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return Result{}, err
	}

	message := fmt.Sprintf("Successfully wrote %d bytes to %s", len(in.Content), in.Path)
	diffSummary := buildWriteDiffSummary(in.Path, string(oldContent), in.Content)
	if diffSummary != "" {
		message += "\n\n" + diffSummary
	}

	preview, trunc := truncateHead(in.Content, 60, 8*1024)
	if strings.TrimSpace(preview) != "" {
		message += "\n\n--- file preview ---\n" + preview
		if trunc.Truncated {
			message += fmt.Sprintf("\n\n[Preview truncated to %d lines / %s]", trunc.OutputLines, formatSize(trunc.OutputBytes))
		}
	}

	return Result{
		Content: []ContentPart{{
			Type: ContentPartText,
			Text: message,
		}},
	}, nil
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
