package core

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/ashiqrniloy/synapta-cli/internal/core/tools"
	"github.com/ashiqrniloy/synapta-cli/internal/llm"
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
	Library        string
	Version        string
	Query          string
	ContextContent string
}

// ChatService provides chat + tool-calling runtime behaviour.
//
// It delegates all provider-specific concerns (token resolution, token
// refresh, provider construction, model discovery) to the ProviderRegistry
// injected at construction time.  Adding a new provider therefore requires
// no changes to ChatService — only a new ProviderDescriptor registration.
type ChatService struct {
	auth        *llm.AuthStorage
	tools       *tools.ToolSet
	registry    *ToolRegistry
	providerReg *llm.ProviderRegistry
	warnings    []string
	mu          sync.RWMutex
	providers   map[string]cachedProvider
}

type cachedProvider struct {
	provider llm.Provider
	token    string
}

// NewChatService creates a ChatService using the built-in provider registry.
func NewChatService(auth *llm.AuthStorage, toolset *tools.ToolSet) *ChatService {
	return NewChatServiceWithRuntimeTools(auth, toolset, LoadToolRegistryOptions{AgentDir: llm.GetAgentDir()})
}

// NewChatServiceWithRuntimeTools creates a ChatService with explicit tool and
// provider registry options.
func NewChatServiceWithRuntimeTools(auth *llm.AuthStorage, toolset *tools.ToolSet, opts LoadToolRegistryOptions) *ChatService {
	return newChatService(auth, toolset, opts, builtinProviderRegistry)
}

// newChatService is the internal constructor; it accepts an explicit provider
// registry to make unit-testing straightforward.
func newChatService(auth *llm.AuthStorage, toolset *tools.ToolSet, opts LoadToolRegistryOptions, provReg *llm.ProviderRegistry) *ChatService {
	toolReg := NewToolRegistry()
	warnings := make([]string, 0)
	if err := toolReg.RegisterBuiltins(toolset); err != nil {
		warnings = append(warnings, err.Error())
	}
	warnings = append(warnings, toolReg.LoadRuntimeTools(opts)...)

	return &ChatService{
		auth:        auth,
		tools:       toolset,
		registry:    toolReg,
		providerReg: provReg,
		warnings:    warnings,
		providers:   make(map[string]cachedProvider),
	}
}

// Tools returns the underlying tool set for direct access.
func (s *ChatService) Tools() *tools.ToolSet {
	return s.tools
}

func (s *ChatService) ToolRegistry() *ToolRegistry {
	return s.registry
}

func (s *ChatService) ToolRegistryWarnings() []string {
	out := make([]string, len(s.warnings))
	copy(out, s.warnings)
	return out
}

// InvalidateProviderCache clears all cached provider instances.
func (s *ChatService) InvalidateProviderCache() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.providers = make(map[string]cachedProvider)
}

// AvailableModels returns every model across all registered providers for
// which credentials are currently available.  Providers without credentials
// are silently skipped; an error is only returned when no provider at all
// could be initialised.
func (s *ChatService) AvailableModels(ctx context.Context) ([]*llm.Model, error) {
	_ = ctx

	models := make([]*llm.Model, 0)
	for _, desc := range s.providerReg.All() {
		p, err := s.providerFor(desc.ID)
		if err != nil {
			// Provider unavailable (no credentials, network error, etc.) — skip.
			continue
		}
		models = append(models, p.Models()...)
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
	if resp.Usage != nil {
		llm.ObserveTokenUsage(provider.ID(), modelID, requestMessages, resp.Usage)
	}
	summary := strings.TrimSpace(resp.Choices[0].Message.Content)
	if summary == "" {
		return "", fmt.Errorf("empty compaction summary")
	}
	return summary, nil
}
