package core

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ashiqrniloy/synapta-cli/internal/core/tools"
	"github.com/ashiqrniloy/synapta-cli/internal/llm"
)

// ParsedToolCall is the normalized/decoded representation of an LLM tool call.
type ParsedToolCall struct {
	Name    string
	Input   any
	Path    string
	Command string
}

// ParseToolCall decodes a tool call input, extracts lightweight metadata
// (path/command), and returns a validation error when parsing/shape is invalid.
func ParseToolCall(tc llm.ToolCall) (ParsedToolCall, error) {
	name := strings.TrimSpace(tc.Function.Name)
	parsed := ParsedToolCall{Name: name}

	switch name {
	case "read":
		var in tools.ReadInput
		if err := json.Unmarshal([]byte(tc.Function.Arguments), &in); err != nil {
			return parsed, fmt.Errorf("invalid read arguments: %w", err)
		}
		parsed.Input = in
		parsed.Path = strings.TrimSpace(in.Path)
		return parsed, nil
	case "write":
		var in tools.WriteInput
		if err := json.Unmarshal([]byte(tc.Function.Arguments), &in); err != nil {
			return parsed, fmt.Errorf("invalid write arguments: %w", err)
		}
		parsed.Input = in
		parsed.Path = strings.TrimSpace(in.Path)
		return parsed, nil
	case "bash":
		var in tools.BashInput
		if err := json.Unmarshal([]byte(tc.Function.Arguments), &in); err != nil {
			return parsed, fmt.Errorf("invalid bash arguments: %w", err)
		}
		parsed.Input = in
		parsed.Command = strings.TrimSpace(in.Command)
		return parsed, nil
	default:
		return parsed, fmt.Errorf("unknown tool: %s", name)
	}
}
