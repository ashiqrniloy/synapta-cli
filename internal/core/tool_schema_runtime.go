package core

import "github.com/ashiqrniloy/synapta-cli/internal/llm"

func (s *ChatService) toolDefinitions() []llm.ToolDefinition {
	if s.registry == nil {
		return nil
	}
	return s.registry.Definitions()
}
