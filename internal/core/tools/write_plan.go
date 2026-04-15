package tools

import (
	"fmt"
	"strings"
)

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

func buildWritePlan(in WriteInput, oldContent string, oldExists bool) (writePlan, error) {
	mode, dryRun, preserveNL := writePlanOptions(in)

	plan := writePlan{
		Mode:               mode,
		OldExists:          oldExists,
		OldContent:         oldContent,
		DryRun:             dryRun,
		PreserveTrailingNL: preserveNL,
	}

	switch mode {
	case WriteModeOverwrite:
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
		return writePlan{}, fmt.Errorf("unsupported write mode %q. Supported modes: overwrite, append, replace, replace_regex, line_edit, insert_after_line, patch", string(mode))
	}

	return plan, nil
}

func writePlanOptions(in WriteInput) (WriteMode, bool, bool) {
	mode := strings.TrimSpace(string(in.Mode))
	if mode == "" {
		mode = string(WriteModeOverwrite)
	}
	dryRun := in.DryRun != nil && *in.DryRun
	preserveNL := in.PreserveTrailingNewline == nil || *in.PreserveTrailingNewline
	return WriteMode(mode), dryRun, preserveNL
}

func resolveReplacement(in WriteInput) string {
	if in.Content != "" && in.Replace != "" && in.Content != in.Replace {
		return in.Content
	}
	if in.Content != "" {
		return in.Content
	}
	return in.Replace
}
