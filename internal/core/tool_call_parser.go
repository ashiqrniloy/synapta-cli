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
	Library      string
	Version      string
	Query        string
	Invocation   ToolInvocationMeta
}

func (m ToolInvocationMeta) normalize() ToolInvocationMeta {
	m.Name = strings.TrimSpace(m.Name)
	m.Path = strings.TrimSpace(m.Path)
	m.Command = strings.TrimSpace(m.Command)
	m.Library = strings.TrimSpace(m.Library)
	m.Version = strings.TrimSpace(m.Version)
	m.Query = strings.TrimSpace(m.Query)
	return m
}

// ParseToolCall decodes a tool call input and extracts lightweight metadata
// (path/command/library/version/query), validating tool existence when a registry is provided.
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
		parsed.Invocation = extractToolCallMeta(raw)
		parsed.Invocation.Name = name
		parsed.Invocation = parsed.Invocation.normalize()
		parsed.Path = parsed.Invocation.Path
		parsed.Command = parsed.Invocation.Command
		parsed.Library = parsed.Invocation.Library
		parsed.Version = parsed.Invocation.Version
		parsed.Query = parsed.Invocation.Query
		return parsed, nil
	}

	decoded, err := registry.Decode(name, args)
	if err != nil {
		return parsed, err
	}
	meta := registry.Metadata(name, decoded)
	parsed.Decoded = decoded
	parsed.Invocation = ToolInvocationMeta{
		Name:    name,
		Path:    meta.Path,
		Command: meta.Command,
		Library: meta.Library,
		Version: meta.Version,
		Query:   meta.Query,
	}.normalize()
	parsed.Path = parsed.Invocation.Path
	parsed.Command = parsed.Invocation.Command
	parsed.Library = parsed.Invocation.Library
	parsed.Version = parsed.Invocation.Version
	parsed.Query = parsed.Invocation.Query
	return parsed, nil
}

func extractToolCallMeta(raw json.RawMessage) ToolInvocationMeta {
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return ToolInvocationMeta{}
	}
	meta := ToolInvocationMeta{}
	if v, ok := m["path"].(string); ok {
		meta.Path = v
	}
	if v, ok := m["command"].(string); ok {
		meta.Command = v
	}
	if v, ok := m["library_name"].(string); ok {
		meta.Library = v
	}
	if v, ok := m["version"].(string); ok {
		meta.Version = v
	}
	if v, ok := m["query"].(string); ok {
		meta.Query = v
	}
	return meta.normalize()
}
