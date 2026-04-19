package core

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ashiqrniloy/synapta-cli/internal/core/tools"
)

func (s *ChatService) executeToolCall(ctx context.Context, parsed ParsedToolCall, callID string, onToolEvent func(event ToolEvent) error) (any, error) {
	if s.registry == nil {
		return nil, fmt.Errorf("tool registry not configured")
	}

	spec, ok := s.registry.Get(parsed.Name)
	if !ok {
		return nil, fmt.Errorf("unknown tool: %s", parsed.Name)
	}

	input := parsed.Decoded
	if input == nil {
		decoded, err := s.registry.Decode(parsed.Name, string(parsed.RawArguments))
		if err != nil {
			return nil, err
		}
		input = decoded
	}

	return spec.Executor(ctx, input, func(update tools.Result) {
		if onToolEvent == nil {
			return
		}
		_ = onToolEvent(ToolEvent{
			Type:      ToolEventUpdate,
			CallID:    callID,
			ToolName:  parsed.Name,
			Output:    toolResultText(update),
			IsPartial: true,
		})
	})
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

func summarizeToolResult(v any) ToolResultSummary {
	summary := ToolResultSummary{Text: toolResultText(v)}
	switch r := v.(type) {
	case map[string]any:
		if errText, ok := r["error"].(string); ok {
			summary.Error = strings.TrimSpace(errText)
		}
	case map[string]string:
		summary.Error = strings.TrimSpace(r["error"])
	}
	if summary.Error == "" {
		if trimmed := strings.TrimSpace(summary.Text); strings.HasPrefix(trimmed, "{") && strings.HasSuffix(trimmed, "}") {
			var payload map[string]any
			if err := json.Unmarshal([]byte(trimmed), &payload); err == nil {
				if errText, ok := payload["error"].(string); ok {
					summary.Error = strings.TrimSpace(errText)
				}
			}
		}
	}
	return summary
}
