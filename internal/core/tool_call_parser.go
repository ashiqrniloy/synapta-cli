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
	Path         string
	Command      string
}

// ParseToolCall decodes a tool call input, extracts lightweight metadata
// (path/command), and validates tool existence when registry is provided.
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

	if registry != nil {
		spec, ok := registry.Get(name)
		if !ok {
			return parsed, fmt.Errorf("unknown tool: %s", name)
		}
		if err := json.Unmarshal(raw, new(any)); err != nil {
			return parsed, fmt.Errorf("invalid %s arguments: %w", name, err)
		}
		if len(spec.Parameters) == 0 {
			parsed.RawArguments = raw
			parsed.Path, parsed.Command = extractToolCallMeta(name, raw)
			return parsed, nil
		}
	}

	parsed.RawArguments = raw
	parsed.Path, parsed.Command = extractToolCallMeta(name, raw)
	return parsed, nil
}

func extractToolCallMeta(name string, raw json.RawMessage) (path string, command string) {
	type pathOnly struct {
		Path string `json:"path"`
	}
	type cmdOnly struct {
		Command string `json:"command"`
	}

	switch name {
	case "read", "write":
		var p pathOnly
		if err := json.Unmarshal(raw, &p); err == nil {
			return strings.TrimSpace(p.Path), ""
		}
	case "bash":
		var c cmdOnly
		if err := json.Unmarshal(raw, &c); err == nil {
			return "", strings.TrimSpace(c.Command)
		}
	}

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
