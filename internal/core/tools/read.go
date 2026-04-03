package tools

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
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
	return "Read the contents of a file. Supports text files and images (jpg, png, gif, webp). Images are returned as base64 attachments. For text files, output is truncated to 2000 lines or 50KB (whichever is hit first). Use offset/limit for large files."
}

type ReadDetails struct {
	Truncation *Truncation `json:"truncation,omitempty"`
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

	out, trunc := truncateHead(selected, DefaultMaxLines, DefaultMaxBytes)
	details := ReadDetails{}
	output := out

	if trunc.FirstLineExceedsLimit {
		firstLineSize := formatSize(len([]byte(allLines[startLine])))
		output = fmt.Sprintf("[Line %d is %s, exceeds %s limit. Use bash: sed -n '%dp' %s | head -c %d]", startDisplay, firstLineSize, formatSize(DefaultMaxBytes), startDisplay, in.Path, DefaultMaxBytes)
		details.Truncation = &trunc
	} else if trunc.Truncated {
		endDisplay := startDisplay + trunc.OutputLines - 1
		nextOffset := endDisplay + 1
		if trunc.TruncatedBy == TruncationByLines {
			output += fmt.Sprintf("\n\n[Showing lines %d-%d of %d. Use offset=%d to continue.]", startDisplay, endDisplay, totalLines, nextOffset)
		} else {
			output += fmt.Sprintf("\n\n[Showing lines %d-%d of %d (%s limit). Use offset=%d to continue.]", startDisplay, endDisplay, totalLines, formatSize(DefaultMaxBytes), nextOffset)
		}
		details.Truncation = &trunc
	} else if userLimited >= 0 && startLine+userLimited < totalLines {
		remaining := totalLines - (startLine + userLimited)
		nextOffset := startLine + userLimited + 1
		output += fmt.Sprintf("\n\n[%d more lines in file. Use offset=%d to continue.]", remaining, nextOffset)
	}

	var anyDetails any
	if details.Truncation != nil {
		anyDetails = details
	}

	return Result{
		Content: []ContentPart{{Type: ContentPartText, Text: output}},
		Details: anyDetails,
	}, nil
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
