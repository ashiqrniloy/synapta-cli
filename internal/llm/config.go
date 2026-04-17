package llm

import "github.com/ashiqrniloy/synapta-cli/internal/fsutil"

const (
	AppName = "synapta"
)

// GetAgentDir returns the directory for Synapta configuration.
// Uses $SYNAPTA_DIR if set, otherwise ~/.synapta.
func GetAgentDir() string {
	return fsutil.ResolveAgentDir(AppName, "SYNAPTA_DIR")
}
