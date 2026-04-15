package core

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/ashiqrniloy/synapta-cli/internal/core/tools"
	"github.com/ashiqrniloy/synapta-cli/internal/llm"
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
