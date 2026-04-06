package components

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/ashiqrniloy/synapta-cli/internal/core"
)

type extensionProcessDoneMsg struct {
	ExtensionID string
	Err         error
}

func (m *CodeAgentModel) reloadAvailableExtensions() {
	result := core.LoadExtensions(core.LoadExtensionsOptions{
		CWD:      m.currentCwd,
		AgentDir: m.agentDir,
	})
	m.availableExtensions = result.Extensions
	for _, warning := range result.Warnings {
		m.appendSystemMessage("[Extensions] "+warning, "error")
	}
	if m.picker != nil {
		m.picker.SetRootItems(commandItemsWithExtensions(m.availableExtensions))
	}
}

func (m *CodeAgentModel) extensionByID(id string) (core.Extension, bool) {
	for _, ext := range m.availableExtensions {
		if ext.ID == id {
			return ext, true
		}
	}
	return core.Extension{}, false
}

func (m *CodeAgentModel) launchExtensionCmd(ext core.Extension) tea.Cmd {
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
		wd = m.currentCwd
	}

	return tea.ExecProcess(exec.Command(command, args...), func(err error) tea.Msg {
		return extensionProcessDoneMsg{ExtensionID: ext.ID, Err: err}
	})
}

func (m *CodeAgentModel) extensionLaunchLabel(ext core.Extension) string {
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
		wd = m.currentCwd
	}
	return fmt.Sprintf("%s · %s · cwd=%s", ext.Name, cmdline, wd)
}

func (m *CodeAgentModel) handleExtensionDone(msg extensionProcessDoneMsg) {
	if msg.Err != nil {
		m.appendSystemMessage("[Extension] ✗ Failed: "+msg.Err.Error(), "error")
		return
	}
	name := msg.ExtensionID
	if ext, ok := m.extensionByID(msg.ExtensionID); ok {
		name = ext.Name
	}
	m.appendSystemMessage("[Extension] ✓ Finished: "+name, "done")
}

func (m *CodeAgentModel) extensionKeybinding() string {
	if m.cfg != nil && strings.TrimSpace(m.cfg.Keybindings.Extensions) != "" {
		return normalizeKeyName(m.cfg.Keybindings.Extensions)
	}
	return "ctrl+e"
}

func sanitizeCommandForLog(ext core.Extension) string {
	parts := []string{ext.Command}
	parts = append(parts, ext.Args...)
	return strings.TrimSpace(strings.Join(parts, " "))
}

func touchExtensionLastLaunched(path string) {
	now := time.Now()
	_ = os.Chtimes(path, now, now)
}
