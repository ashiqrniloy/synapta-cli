package core

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"sync"
	"strings"
	"time"

	"github.com/ashiqrniloy/synapta-cli/internal/core/tools"
	"github.com/ashiqrniloy/synapta-cli/internal/llm"
	"github.com/ashiqrniloy/synapta-cli/internal/oauth"
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
	Type           ToolEventType
	CallID         string
	ToolName       string
	Output         string
	IsError        bool
	IsPartial      bool
	Path           string
	Command        string
	ContextContent string
}

// ChatService provides chat + tool-calling runtime behavior.
type ChatService struct {
	auth      *llm.AuthStorage
	tools     *tools.ToolSet
	mu        sync.RWMutex
	providers map[string]cachedProvider
}

type cachedProvider struct {
	provider llm.Provider
	token    string
}

func NewChatService(auth *llm.AuthStorage, toolset *tools.ToolSet) *ChatService {
	return &ChatService{
		auth:      auth,
		tools:     toolset,
		providers: make(map[string]cachedProvider),
	}
}

// Tools returns the underlying tool set for direct access.
func (s *ChatService) Tools() *tools.ToolSet {
	return s.tools
}

// InvalidateProviderCache clears all cached provider instances.
func (s *ChatService) InvalidateProviderCache() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.providers = make(map[string]cachedProvider)
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

	requestMessages := BuildCompactionRequestMessages(messages, previousSummary)
	resp, err := provider.Chat(ctx, llm.ChatRequest{
		Model:    modelID,
		Messages: requestMessages,
		Stream:   false,
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
			path, command := toolEventMetadata(tc.Function.Name, tc.Function.Arguments)

			if onToolEvent != nil {
				_ = onToolEvent(ToolEvent{Type: ToolEventStart, CallID: callID, ToolName: tc.Function.Name, Path: path, Command: command})
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

func (s *ChatService) toolDefinitions() []llm.ToolDefinition {
	return []llm.ToolDefinition{
		{Type: "function", Function: llm.ToolFunctionDefinition{
			Name:        "read",
			Description: s.tools.Read.Description(),
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path":   map[string]any{"type": "string", "description": "Path to the file to read (relative or absolute)"},
					"offset": map[string]any{"type": "number", "description": "Line number to start reading from (1-indexed)"},
					"limit":  map[string]any{"type": "number", "description": "Maximum number of lines to read"},
					"include_line_numbers": map[string]any{
						"type":        "boolean",
						"description": "Prefix each output line with its 1-indexed line number. Use this instead of running nl or cat -n in bash.",
					},
					"pattern": map[string]any{
						"type":        "string",
						"description": "Search for a literal string (or RE2 regex when pattern_is_regex=true) and return matching lines with line numbers and optional context. Use this instead of grep/nl in bash.",
					},
					"pattern_is_regex": map[string]any{
						"type":        "boolean",
						"description": "When true, treat pattern as a RE2 regex. Default false (literal string search).",
					},
					"context_lines": map[string]any{
						"type":        "number",
						"description": "Number of surrounding lines to include around each pattern match. Default 0.",
					},
				},
				"required": []string{"path"},
			},
		}},
		{Type: "function", Function: llm.ToolFunctionDefinition{
			Name:        "write",
			Description: s.tools.Write.Description(),
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]any{"type": "string", "description": "Path to the file to edit (relative or absolute)"},
					"mode": map[string]any{
						"type": "string",
						"enum": []string{"overwrite", "replace", "replace_regex", "line_edit", "patch"},
						"description": "Edit strategy. overwrite(default)=replace whole file via content. replace=literal find/replace in existing file. replace_regex=RE2 find/replace in existing file. line_edit=replace start_line..end_line (1-indexed, inclusive) in existing file. patch=apply standard unified_diff with file headers (---/+++) and hunk headers (@@ -old,+new @@).",
					},
					"content":          map[string]any{"type": "string", "description": "New file content (overwrite) or replacement text (replace/replace_regex/line_edit)"},
					"find":             map[string]any{"type": "string", "description": "Text to find (replace) or RE2 pattern (replace_regex)"},
					"replace":          map[string]any{"type": "string", "description": "Replacement text for replace/replace_regex. Note: `content` is also accepted as replacement text for compatibility."},
					"expected_matches": map[string]any{"type": "number", "description": "Expected number of matches in replace/replace_regex mode; fail if mismatch"},
					"max_replacements": map[string]any{"type": "number", "description": "Maximum replacements to apply in replace/replace_regex mode"},
					"start_line":       map[string]any{"type": "number", "description": "1-indexed start line (line_edit mode)"},
					"end_line":         map[string]any{"type": "number", "description": "1-indexed end line inclusive (line_edit mode)"},
					"unified_diff":     map[string]any{"type": "string", "description": "Standard unified diff text (patch mode). Required in patch mode; must include hunk headers like @@ -old,+new @@."},
					"dry_run":          map[string]any{"type": "boolean", "description": "Plan and diff without writing changes"},
					"preserve_trailing_newline": map[string]any{"type": "boolean", "description": "Preserve original trailing newline in line_edit mode (default true)"},
					"include_preview":  map[string]any{"type": "boolean", "description": "When true, append a head-truncated preview of the resulting file. Default false — keeps responses compact."},
				},
				"required": []string{"path"},
			},
		}},
		{Type: "function", Function: llm.ToolFunctionDefinition{
			Name:        "bash",
			Description: s.tools.Bash.Description(),
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"command": map[string]any{"type": "string", "description": "Bash command to execute"},
					"timeout": map[string]any{"type": "number", "description": "Timeout in seconds (optional, no default timeout)"},
				},
				"required": []string{"command"},
			},
		}},
	}
}

func (s *ChatService) providerFor(providerID string) (llm.Provider, error) {
	var token string

	switch providerID {
	case ProviderKilo:
		token = s.tokenFromAuthOrEnv(ProviderKilo, llm.KiloAPIKeyEnv())
	case ProviderGitHubCopilot:
		token = s.tokenFromAuthOrEnv(ProviderGitHubCopilot, "GITHUB_COPILOT_API_KEY")
		if token == "" {
			s.invalidateProvider(providerID)
			return nil, fmt.Errorf("no credentials for provider %q", ProviderGitHubCopilot)
		}
	default:
		return nil, fmt.Errorf("unsupported provider: %s", providerID)
	}

	s.mu.RLock()
	cached, ok := s.providers[providerID]
	s.mu.RUnlock()
	if ok && cached.provider != nil && cached.token == token {
		return cached.provider, nil
	}

	var provider llm.Provider
	switch providerID {
	case ProviderKilo:
		kiloProvider, err := llm.NewKiloProviderWithAuth(token)
		if err != nil {
			return nil, fmt.Errorf("creating kilo provider: %w", err)
		}
		provider = kiloProvider
	case ProviderGitHubCopilot:
		baseURL := oauth.GetBaseUrlFromToken(token)
		if baseURL == "" {
			baseURL = "https://api.individual.githubcopilot.com"
		}
		provider = llm.NewGitHubCopilotProvider(baseURL, token, llm.GitHubCopilotDefaultModels())
	default:
		return nil, fmt.Errorf("unsupported provider: %s", providerID)
	}

	s.mu.Lock()
	s.providers[providerID] = cachedProvider{provider: provider, token: token}
	s.mu.Unlock()

	return provider, nil
}

func (s *ChatService) invalidateProvider(providerID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.providers, providerID)
}

func (s *ChatService) tokenFromAuthOrEnv(providerID, envVar string) string {
	if s.auth != nil {
		if entry := s.auth.Get(providerID); entry != nil {
			switch entry.Type {
			case "oauth":
				if entry.OAuth != nil {
					if providerID == ProviderGitHubCopilot {
						if token := s.ensureFreshCopilotToken(entry.OAuth); token != "" {
							return token
						}
					}
					if entry.OAuth.Access != "" {
						return entry.OAuth.Access
					}
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

func (s *ChatService) ensureFreshCopilotToken(creds *llm.OAuthCredentials) string {
	if creds == nil {
		return ""
	}
	if creds.Access == "" && creds.Refresh == "" {
		return ""
	}

	refreshNeeded := creds.Access == ""
	if !refreshNeeded && creds.Expires > 0 {
		refreshNeeded = time.Now().UnixMilli() >= creds.Expires-60_000
	}
	if !refreshNeeded {
		return creds.Access
	}
	if strings.TrimSpace(creds.Refresh) == "" {
		return creds.Access
	}

	provider := oauth.NewGitHubCopilotOAuth("")
	refreshed, err := provider.RefreshToken(creds)
	if err != nil || refreshed == nil || strings.TrimSpace(refreshed.Access) == "" {
		return creds.Access
	}
	if len(refreshed.ExtraData) == 0 && len(creds.ExtraData) > 0 {
		refreshed.ExtraData = creds.ExtraData
	}
	if s.auth != nil {
		_ = s.auth.SetOAuthCredentials(ProviderGitHubCopilot, refreshed)
	}

	// Auth credentials changed; ensure cached provider is rebuilt with fresh token.
	s.invalidateProvider(ProviderGitHubCopilot)

	return refreshed.Access
}
