package core

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/synapta/synapta-cli/internal/core/tools"
	"github.com/synapta/synapta-cli/internal/llm"
	"github.com/synapta/synapta-cli/internal/oauth"
)

const (
	ProviderKilo          = "kilo"
	ProviderGitHubCopilot = "github-copilot"
)

type ToolEventType string

const (
	ToolEventStart  ToolEventType = "start"
	ToolEventUpdate ToolEventType = "update"
	ToolEventEnd    ToolEventType = "end"
)

type ToolEvent struct {
	Type      ToolEventType
	CallID    string
	ToolName  string
	Output    string
	IsError   bool
	IsPartial bool
}

// ChatService provides chat + tool-calling runtime behavior.
type ChatService struct {
	auth  *llm.AuthStorage
	tools *tools.ToolSet
}

func NewChatService(auth *llm.AuthStorage, toolset *tools.ToolSet) *ChatService {
	return &ChatService{auth: auth, tools: toolset}
}

func (s *ChatService) AvailableModels(ctx context.Context) ([]*llm.Model, error) {
	_ = ctx

	models := make([]*llm.Model, 0)

	kiloProvider, err := s.providerFor(ProviderKilo)
	if err == nil {
		models = append(models, kiloProvider.Models()...)
	}

	copilotProvider, err := s.providerFor(ProviderGitHubCopilot)
	if err == nil {
		models = append(models, copilotProvider.Models()...)
	}

	if len(models) == 0 {
		return nil, fmt.Errorf("no models available: authenticate with /add-provider or set provider credentials")
	}

	return models, nil
}

func (s *ChatService) ModelContextWindow(ctx context.Context, providerID, modelID string) (int, error) {
	models, err := s.AvailableModels(ctx)
	if err != nil {
		return 0, err
	}
	for _, m := range models {
		if m.Provider == providerID && m.ID == modelID {
			if m.ContextWindow > 0 {
				return m.ContextWindow, nil
			}
			return 128000, nil
		}
	}
	return 0, fmt.Errorf("model not found: %s/%s", providerID, modelID)
}

func (s *ChatService) SummarizeCompaction(ctx context.Context, providerID, modelID string, messages []llm.Message, previousSummary string) (string, error) {
	provider, err := s.providerFor(providerID)
	if err != nil {
		return "", err
	}

	prompt := BuildCompactionSummarizationInput(messages, previousSummary)
	resp, err := provider.Chat(ctx, llm.ChatRequest{
		Model: modelID,
		Messages: []llm.Message{
			{Role: "system", Content: CompactionSummarizationSystemPrompt},
			{Role: "user", Content: prompt},
		},
		Stream: false,
	})
	if err != nil {
		return "", err
	}
	if resp == nil || len(resp.Choices) == 0 {
		return "", fmt.Errorf("empty summarization response")
	}
	summary := strings.TrimSpace(resp.Choices[0].Message.Content)
	if summary == "" {
		return "", fmt.Errorf("empty compaction summary")
	}
	return summary, nil
}

// Stream runs a tool-capable assistant loop (pi-style):
// assistant response -> execute requested tools -> continue until final assistant text.
func (s *ChatService) Stream(
	ctx context.Context,
	providerID string,
	modelID string,
	messages []llm.Message,
	onDelta func(text string) error,
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

		messages = append(messages, llm.Message{Role: "assistant", Content: assistantText, ToolCalls: toolCalls})

		for _, tc := range toolCalls {
			callID := tc.ID
			if callID == "" {
				callID = fmt.Sprintf("tool_%d_%s", round, tc.Function.Name)
			}

			if onToolEvent != nil {
				_ = onToolEvent(ToolEvent{Type: ToolEventStart, CallID: callID, ToolName: tc.Function.Name})
			}

			toolResult, execErr := s.executeToolCall(ctx, tc, callID, onToolEvent)
			if execErr != nil {
				toolResult = map[string]any{"error": execErr.Error()}
			}

			payload, _ := json.Marshal(toolResult)
			messages = append(messages, llm.Message{Role: "tool", ToolCallID: callID, Name: tc.Function.Name, Content: string(payload)})

			if onToolEvent != nil {
				output := toolResultText(toolResult)
				_ = onToolEvent(ToolEvent{
					Type:     ToolEventEnd,
					CallID:   callID,
					ToolName: tc.Function.Name,
					Output:   output,
					IsError:  execErr != nil,
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

func (s *ChatService) toolDefinitions() []llm.ToolDefinition {
	return []llm.ToolDefinition{
		{Type: "function", Function: llm.ToolFunctionDefinition{Name: "read", Description: s.tools.Read.Description(), Parameters: map[string]any{"type": "object", "properties": map[string]any{"path": map[string]any{"type": "string", "description": "Path to the file to read (relative or absolute)"}, "offset": map[string]any{"type": "number", "description": "Line number to start reading from (1-indexed)"}, "limit": map[string]any{"type": "number", "description": "Maximum number of lines to read"}}, "required": []string{"path"}}}},
		{Type: "function", Function: llm.ToolFunctionDefinition{Name: "write", Description: s.tools.Write.Description(), Parameters: map[string]any{"type": "object", "properties": map[string]any{"path": map[string]any{"type": "string", "description": "Path to the file to write (relative or absolute)"}, "content": map[string]any{"type": "string", "description": "Content to write to the file"}}, "required": []string{"path", "content"}}}},
		{Type: "function", Function: llm.ToolFunctionDefinition{Name: "bash", Description: s.tools.Bash.Description(), Parameters: map[string]any{"type": "object", "properties": map[string]any{"command": map[string]any{"type": "string", "description": "Bash command to execute"}, "timeout": map[string]any{"type": "number", "description": "Timeout in seconds (optional, no default timeout)"}}, "required": []string{"command"}}}},
	}
}

func (s *ChatService) providerFor(providerID string) (llm.Provider, error) {
	switch providerID {
	case ProviderKilo:
		token := s.tokenFromAuthOrEnv(ProviderKilo, llm.KiloAPIKeyEnv())
		provider, err := llm.NewKiloProviderWithAuth(token)
		if err != nil {
			return nil, fmt.Errorf("creating kilo provider: %w", err)
		}
		return provider, nil
	case ProviderGitHubCopilot:
		token := s.tokenFromAuthOrEnv(ProviderGitHubCopilot, "GITHUB_COPILOT_API_KEY")
		if token == "" {
			return nil, fmt.Errorf("no credentials for provider %q", ProviderGitHubCopilot)
		}
		baseURL := oauth.GetBaseUrlFromToken(token)
		if baseURL == "" {
			baseURL = "https://api.individual.githubcopilot.com"
		}
		return llm.NewGitHubCopilotProvider(baseURL, token, llm.GitHubCopilotDefaultModels()), nil
	default:
		return nil, fmt.Errorf("unsupported provider: %s", providerID)
	}
}

func (s *ChatService) tokenFromAuthOrEnv(providerID, envVar string) string {
	if s.auth != nil {
		if entry := s.auth.Get(providerID); entry != nil {
			switch entry.Type {
			case "oauth":
				if entry.OAuth != nil && entry.OAuth.Access != "" {
					return entry.OAuth.Access
				}
			case "api":
				if entry.API != nil && entry.API.APIKey != "" {
					return entry.API.APIKey
				}
			}
		}
	}
	if envVar == "" {
		return ""
	}
	return strings.TrimSpace(os.Getenv(envVar))
}
