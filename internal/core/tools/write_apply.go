package tools

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ── WriteDetails — structured metadata returned to the agent ─────────────────

type WriteDetails struct {
	// Edit facts
	Mode           WriteMode `json:"mode"`
	DryRun         bool      `json:"dry_run"`
	Changed        bool      `json:"changed"`
	Insertions     int       `json:"insertions"`
	Deletions      int       `json:"deletions"`
	ChangedRanges  []string  `json:"changed_ranges,omitempty"`
	AppliedMatches int       `json:"applied_matches,omitempty"`

	LineCountBefore int `json:"line_count_before"`
	LineCountAfter  int `json:"line_count_after"`
	BytesBefore     int `json:"bytes_before"`
	BytesAfter      int `json:"bytes_after"`

	SHA256After string `json:"sha256_after,omitempty"`

	ExpectedMatches *int `json:"expected_matches,omitempty"`
	MaxReplacements *int `json:"max_replacements,omitempty"`
	StartLine       *int `json:"start_line,omitempty"`
	EndLine         *int `json:"end_line,omitempty"`
	AfterLine       *int `json:"after_line,omitempty"`

	PreserveTrailingNewline *bool `json:"preserve_trailing_newline,omitempty"`
}

func (t *WriteTool) Execute(ctx context.Context, in WriteInput) (Result, error) {
	if strings.TrimSpace(in.Path) == "" {
		return Result{}, fmt.Errorf("path is required. Provide `path` (relative or absolute). Example: {\"path\":\"internal/core/chat.go\",\"mode\":\"overwrite\",\"content\":\"...\"}")
	}

	absPath := resolveToCwd(in.Path, t.cwd)
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

	if in.SHA256Before != "" && oldExists {
		actual := fmt.Sprintf("%x", sha256.Sum256(oldContentBytes))
		if actual != in.SHA256Before {
			return Result{}, fmt.Errorf("stale write rejected: file %q has changed since it was last read (expected sha256 %s, got %s). Read the file again to get the latest content, then retry.", in.Path, in.SHA256Before, actual)
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

	return buildWriteResult(in, plan), nil
}

func buildWriteResult(in WriteInput, plan writePlan) Result {
	changed := plan.OldContent != plan.NewContent
	insertions, deletions, changedRangeStrs, diffText := computeDisplayDiff(plan.OldContent, plan.NewContent)
	hashAfter := fmt.Sprintf("%x", sha256.Sum256([]byte(plan.NewContent)))
	summary := renderWriteSummary(in, plan, changed, insertions, deletions, changedRangeStrs, diffText)

	if in.IncludePreview != nil && *in.IncludePreview {
		preview, trunc := truncateHead(plan.NewContent, 60, 8*1024)
		if strings.TrimSpace(preview) != "" {
			summary += "\n\n--- file preview ---\n" + preview
			if trunc.Truncated {
				summary += fmt.Sprintf("\n\n[Preview truncated to %d lines / %s]", trunc.OutputLines, formatSize(trunc.OutputBytes))
			}
		}
	}

	details := WriteDetails{
		Mode:            plan.Mode,
		DryRun:          plan.DryRun,
		Changed:         changed,
		Insertions:      insertions,
		Deletions:       deletions,
		ChangedRanges:   changedRangeStrs,
		AppliedMatches:  plan.AppliedMatches,
		LineCountBefore: countLines(plan.OldContent),
		LineCountAfter:  countLines(plan.NewContent),
		BytesBefore:     len(plan.OldContent),
		BytesAfter:      len(plan.NewContent),
		SHA256After:     hashAfter,
	}
	populateModeSpecificDetails(&details, plan)

	return Result{Content: []ContentPart{{Type: ContentPartText, Text: summary}}, Details: details}
}

func renderWriteSummary(in WriteInput, plan writePlan, changed bool, insertions, deletions int, changedRangeStrs []string, diffText string) string {
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
	return sb.String()
}

func populateModeSpecificDetails(details *WriteDetails, plan writePlan) {
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
}
