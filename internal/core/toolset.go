package core

import "github.com/synapta/synapta-cli/internal/core/tools"

// NewToolSet returns the shared core tool bundle (read, write, bash).
// Tools are cwd-scoped and can be reused by any agent runtime.
func NewToolSet(cwd string) *tools.ToolSet {
	return tools.NewToolSet(cwd)
}
