package components

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/ashiqrniloy/synapta-cli/internal/application"
	"github.com/ashiqrniloy/synapta-cli/internal/core"
)

type extensionProcessDoneMsg struct {
	ExtensionID string
	Err         error
}

func (m *CodeAgentModel) reloadAvailableExtensions() {
	if m.extensionService == nil {
		return
	}
	result := m.extensionService.Load(m.currentCwd, m.agentDir)
	m.availableExtensions = result.Extensions

	for _, warning := range result.Warnings {
		m.appendSystemMessage("[Extensions] "+warning, "error")
	}
	if m.picker != nil {
		m.picker.SetRootItems(commandItemsWithExtensions(m.availableExtensions))
	}
}

func (m *CodeAgentModel) extensionByID(id string) (core.Extension, bool) {
	if m.extensionService == nil {
		return core.Extension{}, false
	}
	return m.extensionService.FindByID(m.availableExtensions, id)
}

func (m *CodeAgentModel) launchExtensionCmd(ext core.Extension) tea.Cmd {
	if m.extensionService == nil {
		return nil
	}
	cmd := m.extensionService.LaunchCommand(ext, m.currentCwd)
	if cmd == nil {
		return nil
	}

	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		return extensionProcessDoneMsg{ExtensionID: ext.ID, Err: err}
	})
}

func (m *CodeAgentModel) extensionLaunchLabel(ext core.Extension) string {
	if m.extensionService == nil {
		parts := []string{ext.Command}
		parts = append(parts, ext.Args...)
		cmdline := strings.TrimSpace(strings.Join(parts, " "))
		if cmdline == "" {
			cmdline = "(invalid command)"
		}
		return ext.Name + " · " + cmdline
	}
	return m.extensionService.LaunchLabel(ext, m.currentCwd)
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

func touchExtensionLastLaunched(path string) {
	service := application.NewExtensionService()
	service.TouchLastLaunched(path)
}
