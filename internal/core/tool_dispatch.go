package core

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ashiqrniloy/synapta-cli/internal/core/tools"
)

func (s *ChatService) executeToolCall(ctx context.Context, parsed ParsedToolCall, callID string, onToolEvent func(event ToolEvent) error) (any, error) {
	if s.tools == nil {
		return nil, fmt.Errorf("tool set not configured")
	}

	switch in := parsed.Input.(type) {
	case tools.ReadInput:
		return s.tools.Read.Execute(ctx, in)
	case tools.WriteInput:
		return s.tools.Write.Execute(ctx, in)
	case tools.BashInput:
		return s.tools.Bash.Execute(ctx, in, func(update tools.Result) {
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
	default:
		return nil, fmt.Errorf("unknown tool: %s", parsed.Name)
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
