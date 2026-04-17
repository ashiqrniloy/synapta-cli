package core

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ashiqrniloy/synapta-cli/internal/llm"
)

// ParsedToolCall is the normalized/decoded representation of an LLM tool call.
type ParsedToolCall struct {
	Name         string
	RawArguments json.RawMessage
	Decoded      any
	Path         string
	Command      string
}

// ParseToolCall decodes a tool call input and extracts lightweight metadata
// (path/command), validating tool existence when a registry is provided.
func ParseToolCall(tc llm.ToolCall, registry *ToolRegistry) (ParsedToolCall, error) {
	name := strings.TrimSpace(tc.Function.Name)
	parsed := ParsedToolCall{Name: name}
	if name == "" {
		return parsed, fmt.Errorf("unknown tool: empty name")
	}

	args := strings.TrimSpace(tc.Function.Arguments)
	if args == "" {
		args = "{}"
	}
	raw := json.RawMessage(args)
	if !json.Valid(raw) {
		return parsed, fmt.Errorf("invalid %s arguments: invalid JSON", name)
	}

	parsed.RawArguments = raw
	if registry == nil {
		parsed.Path, parsed.Command = extractToolCallMeta(raw)
		return parsed, nil
	}

	decoded, err := registry.Decode(name, args)
	if err != nil {
		return parsed, err
	}
	meta := registry.Metadata(name, decoded)
	parsed.Decoded = decoded
	parsed.Path = strings.TrimSpace(meta.Path)
	parsed.Command = strings.TrimSpace(meta.Command)
	return parsed, nil
}

func extractToolCallMeta(raw json.RawMessage) (path string, command string) {
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return "", ""
	}
	if v, ok := m["path"].(string); ok {
		path = strings.TrimSpace(v)
	}
	if v, ok := m["command"].(string); ok {
		command = strings.TrimSpace(v)
	}
	return path, command
}
