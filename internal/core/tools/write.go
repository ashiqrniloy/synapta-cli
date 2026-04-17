package tools

import (
	"fmt"
	"os"
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

func writeFileDirect(absPath string, content []byte) error {
	dir := filepath.Dir(absPath)
	tmp, err := os.CreateTemp(dir, ".synapta-write-*")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

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

const patchFuzz = 3

func applyUnifiedPatch(oldContent, unifiedDiff string) (string, error) {
	if strings.Contains(strings.TrimSpace(unifiedDiff), "*** Begin Patch") {
		return "", fmt.Errorf("patch mode requires a standard unified diff in `unified_diff`; detected Begin/End Patch wrapper format. Convert to unified diff with file headers (---/+++) and hunk headers (@@ -old,+new @@), or use mode=\"replace\"/\"line_edit\".")
	}

	normalised := strings.ReplaceAll(unifiedDiff, "\r\n", "\n")
	rawLines := strings.Split(normalised, "\n")

	type hunk struct {
		oldStart int
		lines    []string
	}
	var hunks []hunk

	i := 0
	for i < len(rawLines) {
		line := rawLines[i]
		if strings.HasPrefix(line, "--- ") || strings.HasPrefix(line, "+++ ") || strings.HasPrefix(line, "diff ") || strings.HasPrefix(line, "index ") {
			i++
			continue
		}
		if !strings.HasPrefix(line, "@@") {
			i++
			continue
		}

		oldStart, err := parseHunkHeaderStart(line)
		if err != nil {
			return "", fmt.Errorf("hunk %d: %w", len(hunks)+1, err)
		}
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
		for len(body) > 0 && body[len(body)-1] == "" {
			body = body[:len(body)-1]
		}
		hunks = append(hunks, hunk{oldStart: oldStart, lines: body})
	}

	if len(hunks) == 0 {
		return "", fmt.Errorf("patch mode could not find any hunks in `unified_diff`. Provide a standard unified diff with hunk headers like @@ -old,+new @@.")
	}

	oldLines := splitContentLines(oldContent)
	out := make([]string, 0, len(oldLines)+32)
	oldPos := 0

	for hIdx, h := range hunks {
		hunkStart := h.oldStart - 1
		anchorPos, fuzzDelta, err := findHunkAnchor(oldLines, oldPos, hunkStart, h.lines, patchFuzz)
		if err != nil {
			return "", fmt.Errorf("hunk %d (near line %d): %w", hIdx+1, h.oldStart, err)
		}

		if anchorPos < oldPos {
			return "", fmt.Errorf("hunk %d: overlapping or out-of-order (hunk anchored at line %d, current pos %d)", hIdx+1, anchorPos+1, oldPos+1)
		}
		out = append(out, oldLines[oldPos:anchorPos]...)
		oldPos = anchorPos

		fuzzNote := ""
		if fuzzDelta != 0 {
			fuzzNote = fmt.Sprintf(" (applied with offset %+d)", fuzzDelta)
		}

		for _, hl := range h.lines {
			if len(hl) == 0 {
				if oldPos >= len(oldLines) {
					return "", fmt.Errorf("hunk %d%s: context line at old pos %d beyond file", hIdx+1, fuzzNote, oldPos+1)
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
				if oldPos >= len(oldLines) {
					return "", fmt.Errorf("hunk %d%s: context line at old pos %d beyond file", hIdx+1, fuzzNote, oldPos+1)
				}
				out = append(out, oldLines[oldPos])
				oldPos++
			case '-':
				if oldPos >= len(oldLines) {
					return "", fmt.Errorf("hunk %d%s: delete line at old pos %d beyond file", hIdx+1, fuzzNote, oldPos+1)
				}
				if !linesMatch(oldLines[oldPos], text) {
					return "", fmt.Errorf("hunk %d%s: delete mismatch at line %d\n  expected: %q\n  got:      %q", hIdx+1, fuzzNote, oldPos+1, text, oldLines[oldPos])
				}
				oldPos++
			case '+':
				out = append(out, text)
			default:
				return "", fmt.Errorf("hunk %d: unrecognised line prefix %q", hIdx+1, string(prefix))
			}
		}
	}

	out = append(out, oldLines[oldPos:]...)
	result := strings.Join(out, "\n")
	if strings.HasSuffix(oldContent, "\n") {
		result += "\n"
	}
	return result, nil
}

type patchAnchor struct {
	text    string
	bodyIdx int
}

func findHunkAnchor(oldLines []string, fromPos, hunkStart int, body []string, fuzz int) (anchorPos, delta int, err error) {
	var anchors []patchAnchor
	for bi, bl := range body {
		if len(bl) == 0 {
			anchors = append(anchors, patchAnchor{"", bi})
			continue
		}
		switch bl[0] {
		case ' ', '-':
			t := ""
			if len(bl) > 1 {
				t = bl[1:]
			}
			anchors = append(anchors, patchAnchor{t, bi})
		}
	}

	if len(anchors) == 0 {
		pos := hunkStart
		if pos < fromPos {
			pos = fromPos
		}
		if pos > len(oldLines) {
			pos = len(oldLines)
		}
		return pos, pos - hunkStart, nil
	}

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

	firstAnchor := anchors[0].text
	fileAt := "(beyond end of file)"
	if hunkStart < len(oldLines) {
		fileAt = fmt.Sprintf("%q", oldLines[hunkStart])
	}
	return 0, 0, fmt.Errorf("context/delete line not found within ±%d lines of stated position %d\n  diff expects: %q\n  file has at line %d: %s\n  Tip: read the file with include_line_numbers=true to verify line numbers, then regenerate the diff.", fuzz, hunkStart+1, firstAnchor, hunkStart+1, fileAt)
}

func matchesAt(oldLines []string, pos int, anchors []patchAnchor) bool {
	fileIdx := pos
	for _, a := range anchors {
		if a.text == "" {
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

func linesMatch(fileLine, diffLine string) bool {
	if fileLine == diffLine {
		return true
	}
	return strings.TrimRight(fileLine, " \t") == strings.TrimRight(diffLine, " \t")
}

func parseHunkHeaderStart(header string) (oldStart int, err error) {
	if !strings.HasPrefix(header, "@@") {
		return 0, fmt.Errorf("invalid hunk header: %q. Expected standard unified diff hunk header like @@ -oldStart,oldCount +newStart,newCount @@", header)
	}
	parts := strings.Fields(header)
	if len(parts) < 3 {
		return 0, fmt.Errorf("invalid hunk header: %q. Expected format: @@ -oldStart,oldCount +newStart,newCount @@", header)
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
