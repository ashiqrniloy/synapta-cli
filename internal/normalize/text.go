package normalize

import "strings"

// NonEmpty trims surrounding whitespace and reports whether a value remains.
func NonEmpty(value string) (string, bool) {
	trimmed := strings.TrimSpace(value)
	return trimmed, trimmed != ""
}

// ID trims and lowercases identifier-like values.
func ID(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

// NonEmptyOr returns the trimmed value (possibly empty).
func NonEmptyOr(value string) string {
	trimmed, _ := NonEmpty(value)
	return trimmed
}
