package application

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/ashiqrniloy/synapta-cli/internal/core"
)

type ExtensionService struct{}

func NewExtensionService() *ExtensionService {
	return &ExtensionService{}
}

func (s *ExtensionService) Load(cwd, agentDir string) core.ExtensionsResult {
	return core.LoadExtensions(core.LoadExtensionsOptions{CWD: cwd, AgentDir: agentDir})
}

func (s *ExtensionService) FindByID(extensions []core.Extension, id string) (core.Extension, bool) {
	for _, ext := range extensions {
		if ext.ID == id {
			return ext, true
		}
	}
	return core.Extension{}, false
}

func (s *ExtensionService) LaunchCommand(ext core.Extension, cwd string) *exec.Cmd {
	command := strings.TrimSpace(ext.Command)
	if command == "" {
		return nil
	}
	args := append([]string(nil), ext.Args...)
	wd := strings.TrimSpace(ext.WorkDir)
	if wd == "" {
		wd = ext.Dir
	}
	if wd == "" {
		wd = cwd
	}
	cmd := exec.Command(command, args...)
	cmd.Dir = wd
	return cmd
}

func (s *ExtensionService) LaunchLabel(ext core.Extension, cwd string) string {
	parts := []string{ext.Command}
	parts = append(parts, ext.Args...)
	cmdline := strings.TrimSpace(strings.Join(parts, " "))
	if cmdline == "" {
		cmdline = "(invalid command)"
	}
	wd := strings.TrimSpace(ext.WorkDir)
	if wd == "" {
		wd = ext.Dir
	}
	if wd == "" {
		wd = cwd
	}
	return fmt.Sprintf("%s · %s · cwd=%s", ext.Name, cmdline, wd)
}

func (s *ExtensionService) TouchLastLaunched(path string) {
	now := time.Now()
	_ = os.Chtimes(path, now, now)
}
