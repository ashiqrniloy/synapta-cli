package normalize

import "strings"

// DomainOrHost normalizes URL/domain input into a hostname-like value.
// Empty input is allowed and returns ok=true with an empty value.
func DomainOrHost(input string) (string, bool) {
	trimmed, ok := NonEmpty(input)
	if !ok {
		return "", true
	}
	if strings.Contains(trimmed, "://") {
		host, valid := hostFromURL(trimmed)
		if !valid {
			return "", false
		}
		return host, true
	}
	if strings.Contains(trimmed, "/") || strings.HasPrefix(trimmed, ".") || strings.HasSuffix(trimmed, ".") || !strings.Contains(trimmed, ".") {
		return "", false
	}
	return strings.ToLower(trimmed), true
}
