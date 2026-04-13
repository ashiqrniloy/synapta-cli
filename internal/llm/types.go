package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// API defines the interface for LLM API implementations.
type API string

const (
	APIOpenAICompletions API = "openai-completions"
	APIOpenAIResponses   API = "openai-responses"
)

// InputModality represents the type of input a model accepts.
type InputModality string

const (
	InputText  InputModality = "text"
	InputImage InputModality = "image"
)

// Cost represents the pricing for a model (per million tokens).
type Cost struct {
	Input      float64 `json:"input"`
	Output     float64 `json:"output"`
	CacheRead  float64 `json:"cacheRead"`
	CacheWrite float64 `json:"cacheWrite"`
}

// Model represents an LLM model with its capabilities and configuration.
type Model struct {
	ID            string          `json:"id"`
	Name          string          `json:"name"`
	Provider      string          `json:"provider"`
	API           API             `json:"api"`
	BaseURL       string          `json:"baseUrl"`
	Reasoning     bool            `json:"reasoning"`
	Input         []InputModality `json:"input"`
	Cost          Cost            `json:"cost"`
	ContextWindow int             `json:"contextWindow"`
	MaxTokens     int             `json:"maxTokens"`
	Compat        *CompatConfig   `json:"compat,omitempty"`
}

// CompatConfig defines OpenAI compatibility settings.
type CompatConfig struct {
	SupportsStore            *bool   `json:"supportsStore,omitempty"`
	SupportsDeveloperRole    *bool   `json:"supportsDeveloperRole,omitempty"`
	SupportsReasoningEffort  *bool   `json:"supportsReasoningEffort,omitempty"`
	SupportsUsageInStreaming *bool   `json:"supportsUsageInStreaming,omitempty"`
	MaxTokensField           *string `json:"maxTokensField,omitempty"`
}

// ToolFunctionDefinition describes a callable tool function.
type ToolFunctionDefinition struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Parameters  any    `json:"parameters,omitempty"`
}

// ToolDefinition declares a tool for the model.
type ToolDefinition struct {
	Type     string                 `json:"type"`
	Function ToolFunctionDefinition `json:"function"`
}

// ToolFunctionCall is the model-produced tool call payload.
type ToolFunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// ToolCall represents a single function tool invocation.
type ToolCall struct {
	Index    int              `json:"index,omitempty"`
	ID       string           `json:"id"`
	Type     string           `json:"type"`
	Function ToolFunctionCall `json:"function"`
}

// Message represents a chat message.
type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	Name       string     `json:"name,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
}

// ChatRequest represents a request to the LLM.
type ChatRequest struct {
	Model      string           `json:"model"`
	Messages   []Message        `json:"messages"`
	Stream     bool             `json:"stream"`
	Tools      []ToolDefinition `json:"tools,omitempty"`
	ToolChoice any              `json:"tool_choice,omitempty"`
}

// ChatResponse represents a response from the LLM.
type ChatResponse struct {
	ID      string   `json:"id,omitempty"`
	Choices []Choice `json:"choices"`
	Usage   *Usage   `json:"usage,omitempty"`
}

// Choice represents a single completion choice.
type Choice struct {
	Index        int     `json:"index"`
	Message      Message `json:"message"`
	FinishReason string  `json:"finish_reason"`
}

// Usage represents token usage statistics.
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// StreamChunk represents a chunk in a streaming response.
type StreamChunk struct {
	ID      string         `json:"id,omitempty"`
	Choices []StreamChoice `json:"choices"`
	Usage   *Usage         `json:"usage,omitempty"`
}

// StreamChoice represents a choice in a streaming response.
type StreamChoice struct {
	Index        int     `json:"index"`
	Delta        Message `json:"delta"`
	Message      Message `json:"message,omitempty"`
	Text         string  `json:"text,omitempty"`
	FinishReason string  `json:"finish_reason,omitempty"`
}

// StreamCallback is called for each chunk in a streaming response.
type StreamCallback func(chunk StreamChunk) error

// OAuthCredentials stores OAuth authentication credentials.
type OAuthCredentials struct {
	Refresh   string          `json:"refresh"`
	Access    string          `json:"access"`
	Expires   int64           `json:"expires"`
	ExtraData json.RawMessage `json:"extraData,omitempty"`
}

// APICredentials stores API key credentials.
type APICredentials struct {
	APIKey string `json:"apiKey"`
}

// OAuthLoginCallbacks provides callbacks for the OAuth login flow.
type OAuthLoginCallbacks struct {
	OnAuth     func(url string, instructions string)
	OnProgress func(message string)
	OnPrompt   func(message string, placeholder string, allowEmpty bool) (string, error)
	Signal     context.Context
}

// Provider defines the interface for an LLM provider.
type Provider interface {
	ID() string
	Name() string
	Models() []*Model
	Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error)
	ChatStream(ctx context.Context, req ChatRequest, callback StreamCallback) error
	HasAuth() bool
}

// AuthEntry represents stored credentials for a provider.
type AuthEntry struct {
	Type  string            `json:"type"`
	OAuth *OAuthCredentials `json:"oauth,omitempty"`
	API   *APICredentials   `json:"api,omitempty"`
}

// AuthStorage manages credential storage.
type AuthStorage struct {
	mu       sync.RWMutex
	data     map[string]*AuthEntry
	filePath string
}

// NewAuthStorage creates a new auth storage.
func NewAuthStorage(agentDir string) (*AuthStorage, error) {
	if err := os.MkdirAll(agentDir, 0700); err != nil {
		return nil, fmt.Errorf("creating agent dir: %w", err)
	}

	filePath := filepath.Join(agentDir, "auth.json")
	s := &AuthStorage{
		data:     make(map[string]*AuthEntry),
		filePath: filePath,
	}

	if err := s.load(); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("loading auth: %w", err)
	}

	return s, nil
}

func (s *AuthStorage) load() error {
	data, err := os.ReadFile(s.filePath)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, &s.data)
}

func (s *AuthStorage) save() error {
	data, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling auth: %w", err)
	}
	return os.WriteFile(s.filePath, data, 0600)
}

func (s *AuthStorage) Get(provider string) *AuthEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.data[provider]
}

func (s *AuthStorage) HasAuth(provider string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entry, ok := s.data[provider]
	if !ok {
		return false
	}

	switch entry.Type {
	case "oauth":
		return entry.OAuth != nil && (entry.OAuth.Access != "" || entry.OAuth.Refresh != "")
	case "api":
		return entry.API != nil && entry.API.APIKey != ""
	}
	return false
}

func (s *AuthStorage) GetAPIKey(provider string) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entry, ok := s.data[provider]
	if !ok {
		return "", fmt.Errorf("no credentials for provider: %s", provider)
	}

	switch entry.Type {
	case "oauth":
		if entry.OAuth == nil {
			return "", fmt.Errorf("no OAuth credentials for provider: %s", provider)
		}
		return entry.OAuth.Access, nil
	case "api":
		if entry.API == nil {
			return "", fmt.Errorf("no API credentials for provider: %s", provider)
		}
		return entry.API.APIKey, nil
	}
	return "", fmt.Errorf("unknown credential type: %s", entry.Type)
}

func (s *AuthStorage) SetOAuthCredentials(provider string, creds *OAuthCredentials) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.data[provider] = &AuthEntry{
		Type:  "oauth",
		OAuth: creds,
	}
	return s.save()
}

func (s *AuthStorage) SetAPIKey(provider string, apiKey string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.data[provider] = &AuthEntry{
		Type: "api",
		API:  &APICredentials{APIKey: apiKey},
	}
	return s.save()
}

func (s *AuthStorage) Remove(provider string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.data, provider)
	return s.save()
}

func (s *AuthStorage) GetOAuthCredentials(provider string) (*OAuthCredentials, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entry, ok := s.data[provider]
	if !ok {
		return nil, fmt.Errorf("no credentials for provider: %s", provider)
	}

	if entry.Type != "oauth" || entry.OAuth == nil {
		return nil, fmt.Errorf("provider %s is not using OAuth", provider)
	}

	return entry.OAuth, nil
}
