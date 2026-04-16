package core

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/ashiqrniloy/synapta-cli/internal/llm"
)

// Stream runs a tool-capable assistant loop (pi-style):
// assistant response -> execute requested tools -> continue until final assistant text.
func (s *ChatService) Stream(
	ctx context.Context,
	providerID string,
	modelID string,
	messages []llm.Message,
	onDelta func(text string) error,
	onAssistantToolCalls func(message llm.Message) error,
	onToolEvent func(event ToolEvent) error,
) error {
	provider, err := s.providerFor(providerID)
	if err != nil {
		return err
	}

	for round := 0; ; round++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		assistantText, toolCalls, err := s.streamAssistantTurn(ctx, provider, modelID, messages, onDelta)
		if err != nil {
			return err
		}

		if len(toolCalls) == 0 {
			if strings.TrimSpace(assistantText) == "" {
				return fmt.Errorf("provider %q model %q returned empty content", providerID, modelID)
			}
			return nil
		}

		assistantMsg := llm.Message{Role: "assistant", Content: assistantText, ToolCalls: append([]llm.ToolCall(nil), toolCalls...)}
		if onAssistantToolCalls != nil {
			if err := onAssistantToolCalls(assistantMsg); err != nil {
				return err
			}
		}

		messages = append(messages, assistantMsg)

		for _, tc := range toolCalls {
			callID := tc.ID
			if callID == "" {
				callID = fmt.Sprintf("tool_%d_%s", round, tc.Function.Name)
			}
			parsed, parseErr := ParseToolCall(tc, s.registry)
			path, command := parsed.Path, parsed.Command

			if onToolEvent != nil {
				_ = onToolEvent(ToolEvent{Type: ToolEventStart, CallID: callID, ToolName: tc.Function.Name, Path: path, Command: command})
			}

			var (
				toolResult any
				execErr    error
			)
			if parseErr != nil {
				execErr = parseErr
				toolResult = map[string]any{"error": parseErr.Error()}
			} else {
				toolResult, execErr = s.executeToolCall(ctx, parsed, callID, onToolEvent)
				if execErr != nil {
					toolResult = map[string]any{"error": execErr.Error()}
				}
			}

			payload, _ := json.Marshal(toolResult)
			messages = append(messages, llm.Message{Role: "tool", ToolCallID: callID, Name: tc.Function.Name, Content: string(payload)})

			if onToolEvent != nil {
				output := toolResultText(toolResult)
				_ = onToolEvent(ToolEvent{
					Type:           ToolEventEnd,
					CallID:         callID,
					ToolName:       tc.Function.Name,
					Output:         output,
					ContextContent: string(payload),
					IsError:        execErr != nil,
					Path:           path,
					Command:        command,
				})
			}
		}
	}
}

func (s *ChatService) streamAssistantTurn(ctx context.Context, provider llm.Provider, modelID string, messages []llm.Message, onDelta func(text string) error) (string, []llm.ToolCall, error) {
	streamReq := llm.ChatRequest{Model: modelID, Messages: messages, Stream: true, Tools: s.toolDefinitions(), ToolChoice: "auto"}

	var contentBuilder strings.Builder
	toolCallByIndex := map[int]*llm.ToolCall{}

	err := provider.ChatStream(ctx, streamReq, func(chunk llm.StreamChunk) error {
		for _, choice := range chunk.Choices {
			if choice.Delta.Content != "" {
				contentBuilder.WriteString(choice.Delta.Content)
				if onDelta != nil {
					if err := onDelta(choice.Delta.Content); err != nil {
						return err
					}
				}
			}
			for _, tc := range choice.Delta.ToolCalls {
				existing := toolCallByIndex[tc.Index]
				if existing == nil {
					copyTC := tc
					toolCallByIndex[tc.Index] = &copyTC
					continue
				}
				if tc.ID != "" {
					existing.ID = tc.ID
				}
				if tc.Type != "" {
					existing.Type = tc.Type
				}
				if tc.Function.Name != "" {
					existing.Function.Name = tc.Function.Name
				}
				if tc.Function.Arguments != "" {
					existing.Function.Arguments += tc.Function.Arguments
				}
			}
		}
		return nil
	})
	if err != nil {
		return "", nil, err
	}

	toolCalls := make([]llm.ToolCall, 0, len(toolCallByIndex))
	if len(toolCallByIndex) > 0 {
		idx := make([]int, 0, len(toolCallByIndex))
		for k := range toolCallByIndex {
			idx = append(idx, k)
		}
		sort.Ints(idx)
		for _, i := range idx {
			toolCalls = append(toolCalls, *toolCallByIndex[i])
		}
	}

	if len(toolCalls) == 0 && strings.TrimSpace(contentBuilder.String()) == "" {
		resp, err := provider.Chat(ctx, llm.ChatRequest{Model: modelID, Messages: messages, Stream: false, Tools: s.toolDefinitions(), ToolChoice: "auto"})
		if err != nil {
			return "", nil, fmt.Errorf("stream produced no output and fallback chat failed: %w", err)
		}
		if resp == nil || len(resp.Choices) == 0 {
			return "", nil, fmt.Errorf("empty response")
		}
		fallbackText := resp.Choices[0].Message.Content
		if strings.TrimSpace(fallbackText) != "" && onDelta != nil {
			if err := onDelta(fallbackText); err != nil {
				return "", nil, err
			}
		}
		return fallbackText, resp.Choices[0].Message.ToolCalls, nil
	}

	return contentBuilder.String(), toolCalls, nil
}
