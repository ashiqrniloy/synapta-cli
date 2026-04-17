package normalize

import "strings"

// ShortcutKey normalizes ":"-prefixed command shortcuts.
func ShortcutKey(value string) string {
	v := strings.ToLower(strings.TrimSpace(value))
	v = strings.TrimPrefix(v, ":")
	if strings.ContainsAny(v, " \t\n") {
		return ""
	}
	return strings.TrimSpace(v)
}

// KeyName normalizes user key names for Bubble Tea key matching.
func KeyName(value string) string {
	v := strings.ToLower(strings.TrimSpace(value))
	return strings.NewReplacer("escape", "esc").Replace(v)
}
