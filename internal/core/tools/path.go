package tools

import (
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/text/unicode/norm"
)

var unicodeSpaces = strings.NewReplacer(
	"\u00A0", " ",
	"\u2000", " ", "\u2001", " ", "\u2002", " ", "\u2003", " ", "\u2004", " ",
	"\u2005", " ", "\u2006", " ", "\u2007", " ", "\u2008", " ", "\u2009", " ",
	"\u200A", " ", "\u202F", " ", "\u205F", " ", "\u3000", " ",
)

func normalizeAtPrefix(path string) string {
	if strings.HasPrefix(path, "@") {
		return strings.TrimPrefix(path, "@")
	}
	return path
}

func expandPath(path string) string {
	path = unicodeSpaces.Replace(path)
	path = normalizeAtPrefix(path)
	if path == "~" {
		home, _ := os.UserHomeDir()
		return home
	}
	if strings.HasPrefix(path, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, strings.TrimPrefix(path, "~/"))
	}
	return path
}

func resolveToCwd(path, cwd string) string {
	expanded := expandPath(path)
	if filepath.IsAbs(expanded) {
		return expanded
	}
	return filepath.Join(cwd, expanded)
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func tryMacAmPMVariant(path string) string {
	return strings.NewReplacer(" AM.", "\u202FAM.", " PM.", "\u202FPM.").Replace(path)
}

func tryNFDVariant(path string) string {
	return norm.NFD.String(path)
}

func tryCurlyQuoteVariant(path string) string {
	return strings.ReplaceAll(path, "'", "’")
}

func resolveReadPath(path, cwd string) string {
	resolved := resolveToCwd(path, cwd)
	if fileExists(resolved) {
		return resolved
	}
	if v := tryMacAmPMVariant(resolved); v != resolved && fileExists(v) {
		return v
	}
	nfd := tryNFDVariant(resolved)
	if nfd != resolved && fileExists(nfd) {
		return nfd
	}
	if v := tryCurlyQuoteVariant(resolved); v != resolved && fileExists(v) {
		return v
	}
	if v := tryCurlyQuoteVariant(nfd); v != resolved && fileExists(v) {
		return v
	}
	return resolved
}
