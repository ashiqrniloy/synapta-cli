package core

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ashiqrniloy/synapta-cli/internal/core/tools"
	"github.com/ashiqrniloy/synapta-cli/internal/llm"
)

func toolEventMetadata(toolName, args string) (path string, command string) {
	switch toolName {
	case "read":
		var in tools.ReadInput
		if err := json.Unmarshal([]byte(args), &in); err == nil {
			return strings.TrimSpace(in.Path), ""
		}
	case "write":
		var in tools.WriteInput
		if err := json.Unmarshal([]byte(args), &in); err == nil {
			return strings.TrimSpace(in.Path), ""
		}
	case "bash":
		var in tools.BashInput
		if err := json.Unmarshal([]byte(args), &in); err == nil {
			return "", strings.TrimSpace(in.Command)
		}
	}
	return "", ""
}

func (s *ChatService) executeToolCall(ctx context.Context, tc llm.ToolCall, callID string, onToolEvent func(event ToolEvent) error) (any, error) {
	if s.tools == nil {
		return nil, fmt.Errorf("tool set not configured")
	}

	switch tc.Function.Name {
	case "read":
		var in tools.ReadInput
		if err := json.Unmarshal([]byte(tc.Function.Arguments), &in); err != nil {
			return nil, fmt.Errorf("invalid read arguments: %w", err)
		}
		return s.tools.Read.Execute(ctx, in)
	case "write":
		var in tools.WriteInput
		if err := json.Unmarshal([]byte(tc.Function.Arguments), &in); err != nil {
			return nil, fmt.Errorf("invalid write arguments: %w", err)
		}
		return s.tools.Write.Execute(ctx, in)
	case "bash":
		var in tools.BashInput
		if err := json.Unmarshal([]byte(tc.Function.Arguments), &in); err != nil {
			return nil, fmt.Errorf("invalid bash arguments: %w", err)
		}
		return s.tools.Bash.Execute(ctx, in, func(update tools.Result) {
			if onToolEvent == nil {
				return
			}
			_ = onToolEvent(ToolEvent{
				Type:      ToolEventUpdate,
				CallID:    callID,
				ToolName:  "bash",
				Output:    toolResultText(update),
				IsPartial: true,
			})
		})
	default:
		return nil, fmt.Errorf("unknown tool: %s", tc.Function.Name)
	}
}

func toolResultText(v any) string {
	switch r := v.(type) {
	case tools.Result:
		var b strings.Builder
		for _, c := range r.Content {
			if c.Type == tools.ContentPartText && c.Text != "" {
				if b.Len() > 0 {
					b.WriteString("\n")
				}
				b.WriteString(c.Text)
			}
		}
		if b.Len() > 0 {
			return b.String()
		}
		if r.Details != nil {
			data, _ := json.MarshalIndent(r.Details, "", "  ")
			return string(data)
		}
		return ""
	default:
		data, _ := json.MarshalIndent(v, "", "  ")
		return string(data)
	}
}
