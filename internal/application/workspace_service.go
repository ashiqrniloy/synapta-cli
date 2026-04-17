package application

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ashiqrniloy/synapta-cli/internal/core/tools"
)

type WorkspaceService struct{}

func NewWorkspaceService() *WorkspaceService {
	return &WorkspaceService{}
}

func (s *WorkspaceService) ResolveCurrentDir(current string) string {
	cwd := strings.TrimSpace(current)
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	return cwd
}

func (s *WorkspaceService) ParseCDCommand(command string) (target string, ok bool) {
	trimmed := strings.TrimSpace(command)
	if trimmed == "cd" {
		return "~", true
	}
	if !strings.HasPrefix(trimmed, "cd ") {
		return "", false
	}
	target = strings.TrimSpace(strings.TrimPrefix(trimmed, "cd"))
	if target == "" {
		return "~", true
	}
	if strings.Contains(target, "&&") || strings.Contains(target, "||") || strings.ContainsAny(target, ";|><`") {
		return "", false
	}
	if (strings.HasPrefix(target, "\"") && strings.HasSuffix(target, "\"")) || (strings.HasPrefix(target, "'") && strings.HasSuffix(target, "'")) {
		target = target[1 : len(target)-1]
	}
	return target, true
}

func (s *WorkspaceService) ResolveCDTarget(baseCwd, target string) (string, error) {
	if strings.HasPrefix(target, "~") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		if target == "~" {
			target = home
		} else {
			target = filepath.Join(home, strings.TrimPrefix(target, "~/"))
		}
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(baseCwd, target)
	}
	resolved, err := filepath.Abs(filepath.Clean(target))
	if err != nil {
		return "", err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%s is not a directory", resolved)
	}
	return resolved, nil
}

func (s *WorkspaceService) ExecuteBash(ctx context.Context, cwd, command string) (string, error) {
	bashTool := tools.NewBashTool(cwd)
	res, err := bashTool.Execute(ctx, tools.BashInput{Command: command}, nil)
	output := ToolResultPlainText(res)
	if strings.TrimSpace(output) == "" && err != nil {
		output = err.Error()
	}
	return output, err
}

func ToolResultPlainText(result tools.Result) string {
	var b strings.Builder
	for _, c := range result.Content {
		if c.Type == tools.ContentPartText && c.Text != "" {
			if b.Len() > 0 {
				b.WriteString("\n")
			}
			b.WriteString(c.Text)
		}
	}
	return strings.TrimSpace(b.String())
}
