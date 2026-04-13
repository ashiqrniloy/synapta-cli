package llm

import (
	"os"
	"path/filepath"
)

const (
	AppName = "synapta"
)

// GetAgentDir returns the directory for Synapta configuration.
// Uses $SYNAPTA_DIR if set, otherwise ~/.synapta.
func GetAgentDir() string {
	if envDir := os.Getenv("SYNAPTA_DIR"); envDir != "" {
		return envDir
	}

	home, err := os.UserHomeDir()
	if err != nil {
		// Fallback to current directory
		return "." + AppName
	}

	return filepath.Join(home, "."+AppName)
}
