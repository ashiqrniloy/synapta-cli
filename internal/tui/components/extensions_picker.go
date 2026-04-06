package components

import (
	"fmt"
	"strings"

	"github.com/ashiqrniloy/synapta-cli/internal/core"
)

func extensionCommandID(ext core.Extension) string {
	return "extension:" + ext.ID
}

func parseExtensionCommandID(id string) (string, bool) {
	const prefix = "extension:"
	if !strings.HasPrefix(id, prefix) {
		return "", false
	}
	extID := strings.TrimSpace(strings.TrimPrefix(id, prefix))
	if extID == "" {
		return "", false
	}
	return extID, true
}

func commandItemsWithExtensions(exts []core.Extension) []CommandItem {
	items := append([]CommandItem{}, DefaultCommands()...)
	for _, ext := range exts {
		name := ext.Name
		if strings.TrimSpace(ext.Description) != "" {
			name = fmt.Sprintf("%s — %s", ext.Name, ext.Description)
		}
		items = append(items, CommandItem{ID: extensionCommandID(ext), Name: name})
	}
	return items
}
