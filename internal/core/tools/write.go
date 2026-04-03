package tools

import (
	"context"
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
	return "Write content to a file. Creates the file if it doesn't exist, overwrites if it does. Automatically creates parent directories."
}

func (t *WriteTool) Execute(ctx context.Context, in WriteInput) (Result, error) {
	if strings.TrimSpace(in.Path) == "" {
		return Result{}, fmt.Errorf("path is required")
	}

	absPath := resolveToCwd(in.Path, t.cwd)
	dir := filepath.Dir(absPath)

	err := withFileMutationQueue(absPath, func() error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("creating parent dirs: %w", err)
		}

		if err := os.WriteFile(absPath, []byte(in.Content), 0o644); err != nil {
			return fmt.Errorf("writing file: %w", err)
		}
		return nil
	})
	if err != nil {
		return Result{}, err
	}

	preview, trunc := truncateHead(in.Content, 60, 8*1024)
	message := fmt.Sprintf("Successfully wrote %d bytes to %s", len(in.Content), in.Path)
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
