package core

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	coretools "github.com/ashiqrniloy/synapta-cli/internal/core/tools"
	"github.com/ashiqrniloy/synapta-cli/internal/llm"
)

// fileStateTracker keeps lightweight per-stream file facts so we can:
// 1) keep sha256_before in sync after successful writes, and
// 2) avoid repeatedly sending identical read payloads into LLM context.
type fileStateTracker struct {
	latestSHAByPath map[string]string
	seenReadByKey   map[string]struct{}
}

func newFileStateTracker() *fileStateTracker {
	return &fileStateTracker{
		latestSHAByPath: map[string]string{},
		seenReadByKey:   map[string]struct{}{},
	}
}

func normalizeFileKey(path string) string {
	p := strings.TrimSpace(path)
	if p == "" {
		return ""
	}
	return filepath.Clean(p)
}

func (f *fileStateTracker) setLatest(path, sha string) {
	key := normalizeFileKey(path)
	sha = strings.TrimSpace(sha)
	if key == "" || sha == "" {
		return
	}
	f.latestSHAByPath[key] = sha
}

func (f *fileStateTracker) latest(path string) (string, bool) {
	key := normalizeFileKey(path)
	if key == "" {
		return "", false
	}
	sha, ok := f.latestSHAByPath[key]
	return sha, ok && strings.TrimSpace(sha) != ""
}

func makeReadSeenKey(path, sha string) string {
	key := normalizeFileKey(path)
	sha = strings.TrimSpace(sha)
	if key == "" || sha == "" {
		return ""
	}
	return key + "#" + sha
}

func (f *fileStateTracker) markRead(path, sha string) (alreadySeen bool) {
	key := makeReadSeenKey(path, sha)
	if key == "" {
		return false
	}
	_, already := f.seenReadByKey[key]
	f.seenReadByKey[key] = struct{}{}
	return already
}

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
	files := newFileStateTracker()

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

		assistantMsg := llm.Message{Role: llm.RoleAssistant, Content: assistantText, ToolCalls: append([]llm.ToolCall(nil), toolCalls...)}
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
			library, version, query := parsed.Library, parsed.Version, parsed.Query

			if parseErr == nil {
				autofillWriteSHA(&parsed, files)
				path, command = parsed.Path, parsed.Command
				library, version, query = parsed.Library, parsed.Version, parsed.Query
			}

			if onToolEvent != nil {
				_ = onToolEvent(ToolEvent{Type: ToolEventStart, CallID: callID, ToolName: tc.Function.Name, Path: path, Command: command, Library: library, Version: version, Query: query})
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

			contextPayload := buildToolContextPayload(tc.Function.Name, path, toolResult, files)
			messages = append(messages, llm.Message{Role: llm.RoleTool, ToolCallID: callID, Name: tc.Function.Name, Content: contextPayload})

			if onToolEvent != nil {
				output := toolResultText(toolResult)
				_ = onToolEvent(ToolEvent{
					Type:           ToolEventEnd,
					CallID:         callID,
					ToolName:       tc.Function.Name,
					Output:         output,
					ContextContent: contextPayload,
					IsError:        execErr != nil,
					Path:           path,
					Command:        command,
					Library:        library,
					Version:        version,
					Query:          query,
				})
			}
		}
	}
}

func autofillWriteSHA(parsed *ParsedToolCall, files *fileStateTracker) {
	if parsed == nil || files == nil || parsed.Decoded == nil {
		return
	}
	in, ok := parsed.Decoded.(coretools.WriteInput)
	if !ok {
		return
	}
	if strings.TrimSpace(in.SHA256Before) != "" {
		return
	}
	if next, ok := files.latest(in.Path); ok {
		in.SHA256Before = next
		parsed.Decoded = in
	}
	if strings.TrimSpace(parsed.Path) == "" {
		parsed.Path = strings.TrimSpace(in.Path)
	}
}

func buildToolContextPayload(toolName, path string, result any, files *fileStateTracker) string {
	tool := strings.TrimSpace(toolName)
	switch r := result.(type) {
	case coretools.Result:
		if tool == "read" {
			if payload, ok := compactReadResult(path, r, files); ok {
				return payload
			}
		}
		if tool == "write" {
			if payload, ok := compactWriteResult(path, r, files); ok {
				return payload
			}
		}
		payload, _ := json.Marshal(r)
		return string(payload)
	default:
		payload, _ := json.Marshal(result)
		return string(payload)
	}
}

func compactReadResult(path string, res coretools.Result, files *fileStateTracker) (string, bool) {
	if files == nil || res.Details == nil {
		return "", false
	}
	details, ok := res.Details.(coretools.ReadDetails)
	if !ok {
		return "", false
	}
	sha := strings.TrimSpace(details.SHA256)
	if sha == "" {
		return "", false
	}
	usePath := strings.TrimSpace(path)
	if usePath == "" {
		usePath = strings.TrimSpace(details.AbsPath)
	}
	if usePath == "" {
		return "", false
	}
	files.setLatest(usePath, sha)
	if abs := strings.TrimSpace(details.AbsPath); abs != "" {
		files.setLatest(abs, sha)
	}
	if files.markRead(usePath, sha) {
		summary := map[string]any{
			"tool":              "read",
			"path":              usePath,
			"sha256":            sha,
			"duplicate_read":    true,
			"context_compacted": true,
			"message":           "Identical file version already captured earlier; skipped repeating full file content.",
		}
		b, _ := json.Marshal(summary)
		return string(b), true
	}
	b, _ := json.Marshal(res)
	return string(b), true
}

func compactWriteResult(path string, res coretools.Result, files *fileStateTracker) (string, bool) {
	if files == nil || res.Details == nil {
		return "", false
	}
	details, ok := res.Details.(coretools.WriteDetails)
	if !ok {
		return "", false
	}
	if sha := strings.TrimSpace(details.SHA256After); sha != "" {
		if usePath := strings.TrimSpace(path); usePath != "" {
			files.setLatest(usePath, sha)
		}
	}
	b, _ := json.Marshal(res)
	return string(b), true
}

func (s *ChatService) streamAssistantTurn(ctx context.Context, provider llm.Provider, modelID string, messages []llm.Message, onDelta func(text string) error) (string, []llm.ToolCall, error) {
	streamReq := llm.ChatRequest{Model: modelID, Messages: messages, Stream: true, Tools: s.toolDefinitions(), ToolChoice: "auto"}

	var contentBuilder strings.Builder
	toolCallByIndex := map[int]*llm.ToolCall{}
	var latestUsage *llm.Usage

	err := provider.ChatStream(ctx, streamReq, func(chunk llm.StreamChunk) error {
		if chunk.Usage != nil {
			u := *chunk.Usage
			latestUsage = &u
		}
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
	if latestUsage != nil {
		llm.ObserveTokenUsage(provider.ID(), modelID, messages, latestUsage)
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
		if resp.Usage != nil {
			llm.ObserveTokenUsage(provider.ID(), modelID, messages, resp.Usage)
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
