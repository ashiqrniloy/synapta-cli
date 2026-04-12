package tools

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type ReadTool struct {
	cwd string
}

func NewReadTool(cwd string) *ReadTool {
	return &ReadTool{cwd: cwd}
}

func (t *ReadTool) Name() string { return "read" }

func (t *ReadTool) Description() string {
	return `Read the contents of a file. Supports text files and images (jpg, png, gif, webp). Images are returned as base64 attachments. For text files, output is truncated to 2000 lines or 50KB (whichever is hit first). Use offset/limit for large files.

PARAMETERS:
- path (required): File to read.
- offset: 1-indexed line to start from.
- limit: Maximum number of lines to return.
- include_line_numbers: When true, prefix each line with its 1-indexed line number. Eliminates the need for shell commands like nl or cat -n.
- pattern: Search for a literal string (or RE2 regex when pattern_is_regex=true) and return matching lines with line numbers. Replaces grep/nl workflows.
- pattern_is_regex: Treat pattern as a RE2 regex (default false).
- context_lines: Number of lines of surrounding context to include around each match (default 0).`
}

// ReadDetails carries compaction-friendly metadata about the file and result.
type ReadDetails struct {
	// File facts — stable identifiers the agent can use to detect stale context.
	AbsPath    string `json:"abs_path"`
	LineCount  int    `json:"line_count"`
	ByteCount  int    `json:"byte_count"`
	SHA256     string `json:"sha256"`

	// Result shape
	Mode        string      `json:"mode"` // "read" | "locate"
	Truncation  *Truncation `json:"truncation,omitempty"`

	// Locate-specific
	MatchCount  int `json:"match_count,omitempty"`
}

func (t *ReadTool) Execute(ctx context.Context, in ReadInput) (Result, error) {
	_ = ctx
	if strings.TrimSpace(in.Path) == "" {
		return Result{}, fmt.Errorf("path is required")
	}

	absPath := resolveReadPath(in.Path, t.cwd)
	b, err := os.ReadFile(absPath)
	if err != nil {
		return Result{}, err
	}

	// Images — unchanged behaviour.
	if mime, ok := detectImageMime(filepath.Ext(absPath)); ok {
		encoded := base64.StdEncoding.EncodeToString(b)
		return Result{
			Content: []ContentPart{
				{Type: ContentPartText, Text: fmt.Sprintf("Read image file [%s]", mime)},
				{Type: ContentPartImage, Data: encoded, MimeType: mime},
			},
		}, nil
	}

	text := string(b)
	allLines := strings.Split(text, "\n")
	totalLines := len(allLines)
	totalBytes := len(b)
	hash := fmt.Sprintf("%x", sha256.Sum256(b))

	baseDetails := ReadDetails{
		AbsPath:   absPath,
		LineCount: totalLines,
		ByteCount: totalBytes,
		SHA256:    hash,
	}

	// ── Locate mode ──────────────────────────────────────────────────────────
	if strings.TrimSpace(in.Pattern) != "" {
		baseDetails.Mode = "locate"
		return t.executeLocate(in, allLines, totalLines, baseDetails)
	}

	// ── Normal read mode ─────────────────────────────────────────────────────
	baseDetails.Mode = "read"

	startLine := 0
	if in.Offset != nil {
		if *in.Offset < 1 {
			startLine = 0
		} else {
			startLine = *in.Offset - 1
		}
	}
	startDisplay := startLine + 1
	if startLine >= totalLines {
		return Result{}, fmt.Errorf("offset %d is beyond end of file (%d lines total)", derefInt(in.Offset, 0), totalLines)
	}

	var selected string
	userLimited := -1
	if in.Limit != nil {
		end := startLine + max(*in.Limit, 0)
		if end > totalLines {
			end = totalLines
		}
		selected = strings.Join(allLines[startLine:end], "\n")
		userLimited = end - startLine
	} else {
		selected = strings.Join(allLines[startLine:], "\n")
	}

	// Optionally annotate lines with their 1-indexed numbers.
	wantLineNums := in.IncludeLineNumbers != nil && *in.IncludeLineNumbers
	if wantLineNums {
		selected = annotateLineNumbers(selected, startLine+1)
	}

	out, trunc := truncateHead(selected, DefaultMaxLines, DefaultMaxBytes)
	output := out

	if trunc.FirstLineExceedsLimit {
		firstLineSize := formatSize(len([]byte(allLines[startLine])))
		output = fmt.Sprintf("[Line %d is %s, exceeds %s limit. Use bash: sed -n '%dp' %s | head -c %d]",
			startDisplay, firstLineSize, formatSize(DefaultMaxBytes), startDisplay, in.Path, DefaultMaxBytes)
		baseDetails.Truncation = &trunc
	} else if trunc.Truncated {
		endDisplay := startDisplay + trunc.OutputLines - 1
		nextOffset := endDisplay + 1
		if trunc.TruncatedBy == TruncationByLines {
			output += fmt.Sprintf("\n\n[Showing lines %d-%d of %d. Use offset=%d to continue.]", startDisplay, endDisplay, totalLines, nextOffset)
		} else {
			output += fmt.Sprintf("\n\n[Showing lines %d-%d of %d (%s limit). Use offset=%d to continue.]", startDisplay, endDisplay, totalLines, formatSize(DefaultMaxBytes), nextOffset)
		}
		baseDetails.Truncation = &trunc
	} else if userLimited >= 0 && startLine+userLimited < totalLines {
		remaining := totalLines - (startLine + userLimited)
		nextOffset := startLine + userLimited + 1
		output += fmt.Sprintf("\n\n[%d more lines in file. Use offset=%d to continue.]", remaining, nextOffset)
	}

	return Result{
		Content: []ContentPart{{Type: ContentPartText, Text: output}},
		Details: baseDetails,
	}, nil
}

// executeLocate handles the locate/search sub-mode of read.
func (t *ReadTool) executeLocate(in ReadInput, allLines []string, totalLines int, details ReadDetails) (Result, error) {
	isRegex := in.PatternIsRegex != nil && *in.PatternIsRegex
	ctxLines := 0
	if in.ContextLines != nil && *in.ContextLines > 0 {
		ctxLines = *in.ContextLines
	}

	// Build matcher.
	var matchLine func(string) bool
	if isRegex {
		re, err := regexp.Compile(in.Pattern)
		if err != nil {
			return Result{}, fmt.Errorf("invalid regex pattern: %w", err)
		}
		matchLine = func(s string) bool { return re.MatchString(s) }
	} else {
		matchLine = func(s string) bool { return strings.Contains(s, in.Pattern) }
	}

	// Collect matching line indices.
	type match struct{ lineIdx int }
	var matches []match
	for i, line := range allLines {
		if matchLine(line) {
			matches = append(matches, match{i})
		}
	}

	details.MatchCount = len(matches)

	if len(matches) == 0 {
		msg := fmt.Sprintf("No matches for %q in %s (%d lines)", in.Pattern, in.Path, totalLines)
		return Result{
			Content: []ContentPart{{Type: ContentPartText, Text: msg}},
			Details: details,
		}, nil
	}

	// Build output — merge overlapping/adjacent context windows.
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Found %d match(es) for %q in %s:\n\n", len(matches), in.Pattern, in.Path))

	printed := make(map[int]bool)
	for _, m := range matches {
		lo := m.lineIdx - ctxLines
		if lo < 0 {
			lo = 0
		}
		hi := m.lineIdx + ctxLines
		if hi >= totalLines {
			hi = totalLines - 1
		}

		// Separator between non-contiguous blocks.
		if sb.Len() > 0 && lo > 0 && !printed[lo-1] {
			if !printed[lo] {
				sb.WriteString("---\n")
			}
		}

		for idx := lo; idx <= hi; idx++ {
			if printed[idx] {
				continue
			}
			printed[idx] = true
			marker := "  "
			if idx == m.lineIdx {
				marker = "> "
			}
			sb.WriteString(fmt.Sprintf("%s%4d | %s\n", marker, idx+1, allLines[idx]))
		}
	}

	return Result{
		Content: []ContentPart{{Type: ContentPartText, Text: sb.String()}},
		Details: details,
	}, nil
}

// annotateLineNumbers prefixes each line of text with its 1-indexed number.
// startLineNo is the number for the first line.
func annotateLineNumbers(text string, startLineNo int) string {
	if text == "" {
		return text
	}
	lines := strings.Split(text, "\n")
	var sb strings.Builder
	for i, line := range lines {
		fmt.Fprintf(&sb, "%4d | %s", startLineNo+i, line)
		if i < len(lines)-1 {
			sb.WriteByte('\n')
		}
	}
	return sb.String()
}

func detectImageMime(ext string) (string, bool) {
	switch strings.ToLower(ext) {
	case ".jpg", ".jpeg":
		return "image/jpeg", true
	case ".png":
		return "image/png", true
	case ".gif":
		return "image/gif", true
	case ".webp":
		return "image/webp", true
	default:
		return "", false
	}
}

func derefInt(v *int, fallback int) int {
	if v == nil {
		return fallback
	}
	return *v
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
