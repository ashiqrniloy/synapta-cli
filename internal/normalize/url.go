package normalize

import (
	"net/url"
	"strings"
)

func hostFromURL(raw string) (string, bool) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", false
	}
	host, ok := NonEmpty(u.Hostname())
	if !ok {
		return "", false
	}
	return strings.ToLower(host), true
}
