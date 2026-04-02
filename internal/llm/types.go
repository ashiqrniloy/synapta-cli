package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
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

// Message represents a chat message.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ChatRequest represents a request to the LLM.
type ChatRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
	Stream   bool      `json:"stream"`
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

// Credentials represents either OAuth or API key credentials.
type Credentials struct {
	Type  string            `json:"type"`
	OAuth *OAuthCredentials `json:"oauth,omitempty"`
	API   *APICredentials   `json:"api,omitempty"`
}

// OAuthLoginCallbacks provides callbacks for the OAuth login flow.
type OAuthLoginCallbacks struct {
	OnAuth     func(url string, instructions string)
	OnProgress func(message string)
	OnPrompt   func(message string, placeholder string, allowEmpty bool) (string, error)
	Signal     context.Context
}

// OAuthProvider defines the interface for OAuth-based authentication.
type OAuthProvider interface {
	ID() string
	Name() string
	Login(callbacks OAuthLoginCallbacks) (*OAuthCredentials, error)
	RefreshToken(credentials *OAuthCredentials) (*OAuthCredentials, error)
	GetAPIKey(credentials *OAuthCredentials) string
	ModifyModels(models []*Model, credentials *OAuthCredentials) []*Model
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

// NewInMemoryAuthStorage creates an in-memory auth storage.
func NewInMemoryAuthStorage() *AuthStorage {
	return &AuthStorage{
		data: make(map[string]*AuthEntry),
	}
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
		return entry.OAuth != nil && entry.OAuth.Access != ""
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

// Registry manages LLM providers and their models.
type Registry struct {
	mu        sync.RWMutex
	providers map[string]Provider
	models    []*Model
}

// NewRegistry creates a new model registry.
func NewRegistry() *Registry {
	return &Registry{
		providers: make(map[string]Provider),
		models:    make([]*Model, 0),
	}
}

func (r *Registry) RegisterProvider(provider Provider) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.providers[provider.ID()] = provider
	for _, model := range provider.Models() {
		r.models = append(r.models, model)
	}
}

func (r *Registry) GetProvider(providerID string) (Provider, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	provider, ok := r.providers[providerID]
	return provider, ok
}

func (r *Registry) GetAllModels() []*Model {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]*Model, len(r.models))
	copy(result, r.models)
	return result
}

func (r *Registry) GetAvailableModels() []*Model {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var available []*Model
	for _, model := range r.models {
		if provider, ok := r.providers[model.Provider]; ok && provider.HasAuth() {
			available = append(available, model)
		}
	}
	return available
}

func (r *Registry) FindModel(provider, modelID string) (*Model, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, m := range r.models {
		if m.Provider == provider && m.ID == modelID {
			return m, true
		}
	}
	return nil, false
}

func (r *Registry) GetModelProvider(model *Model) (Provider, bool) {
	provider, ok := r.providers[model.Provider]
	return provider, ok
}

func (r *Registry) RefreshModels() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.models = make([]*Model, 0)
	for _, provider := range r.providers {
		for _, model := range provider.Models() {
			r.models = append(r.models, model)
		}
	}
}

// Manager coordinates providers, authentication, and model registry.
type Manager struct {
	Registry *Registry
	Auth     *AuthStorage
	oauth    map[string]OAuthProvider
}

// NewManager creates a new LLM manager.
func NewManager(authStorage *AuthStorage) *Manager {
	return &Manager{
		Registry: NewRegistry(),
		Auth:     authStorage,
		oauth:    make(map[string]OAuthProvider),
	}
}

func (m *Manager) RegisterOAuthProvider(provider OAuthProvider) {
	m.oauth[provider.ID()] = provider
}

func (m *Manager) GetOAuthProvider(providerID string) (OAuthProvider, bool) {
	provider, ok := m.oauth[providerID]
	return provider, ok
}

func (m *Manager) ListOAuthProviders() []OAuthProvider {
	providers := make([]OAuthProvider, 0, len(m.oauth))
	for _, p := range m.oauth {
		providers = append(providers, p)
	}
	return providers
}

func (m *Manager) LoginOAuth(providerID string, callbacks OAuthLoginCallbacks) error {
	oauthProvider, ok := m.oauth[providerID]
	if !ok {
		return fmt.Errorf("OAuth provider not found: %s", providerID)
	}

	creds, err := oauthProvider.Login(callbacks)
	if err != nil {
		return fmt.Errorf("OAuth login failed: %w", err)
	}

	if err := m.Auth.SetOAuthCredentials(providerID, creds); err != nil {
		return fmt.Errorf("storing credentials: %w", err)
	}

	m.refreshProviderModels(providerID, oauthProvider, creds)
	return nil
}

func (m *Manager) LogoutOAuth(providerID string) error {
	return m.Auth.Remove(providerID)
}

func (m *Manager) SetAPIKey(providerID, apiKey string) error {
	return m.Auth.SetAPIKey(providerID, apiKey)
}

func (m *Manager) RemoveAPIKey(providerID string) error {
	return m.Auth.Remove(providerID)
}

func (m *Manager) refreshProviderModels(providerID string, oauthProvider OAuthProvider, creds *OAuthCredentials) {
	// This would update the provider with new credentials
	// Implementation depends on provider type
	m.Registry.RefreshModels()
}

func (m *Manager) GetAPIKeyForModel(model *Model) (string, error) {
	apiKey, err := m.Auth.GetAPIKey(model.Provider)
	if err == nil && apiKey != "" {
		return apiKey, nil
	}

	// Check environment variables
	envKey := os.Getenv(m.getEnvVarName(model.Provider))
	if envKey != "" {
		return envKey, nil
	}

	return "", fmt.Errorf("no API key found for provider: %s", model.Provider)
}

func (m *Manager) getEnvVarName(provider string) string {
	switch provider {
	case "anthropic":
		return "ANTHROPIC_API_KEY"
	case "openai":
		return "OPENAI_API_KEY"
	case "openrouter":
		return "OPENROUTER_API_KEY"
	default:
		return provider + "_API_KEY"
	}
}

func (m *Manager) IsUsingOAuth(model *Model) bool {
	return m.Auth.HasAuth(model.Provider)
}

// FormatCredits formats a credit balance for display.
func FormatCredits(balance float64) string {
	if balance >= 1000 {
		return fmt.Sprintf("$%.1fk", balance/1000)
	}
	return fmt.Sprintf("$%.2f", balance)
}

// Time returns the current time in milliseconds.
func Time() int64 {
	return time.Now().UnixMilli()
}
