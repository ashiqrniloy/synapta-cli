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

// GetAuthPath returns the path to the auth.json file.
func GetAuthPath() string {
	return filepath.Join(GetAgentDir(), "auth.json")
}

// GetModelsPath returns the path to the models.json file.
func GetModelsPath() string {
	return filepath.Join(GetAgentDir(), "models.json")
}
