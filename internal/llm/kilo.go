package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/ashiqrniloy/synapta-cli/internal/httpclient"
)

// ─── Constants ──────────────────────────────────────────────────────

const (
	KiloAPIBase        = "https://api.kilo.ai"
	KiloGatewayBase    = KiloAPIBase + "/api/gateway"
	KiloDeviceAuthURL  = KiloAPIBase + "/api/device-auth/codes"
	KiloProfileURL     = KiloAPIBase + "/api/profile"
	KiloPollInterval   = 3 * time.Second
	KiloTokenExpiry    = 365 * 24 * time.Hour // 1 year
	KiloModelsCacheTTL = 24 * time.Hour
)

// ─── API Response Types ─────────────────────────────────────────────

// DeviceAuthResponse is returned when initiating device auth.
type DeviceAuthResponse struct {
	Code            string `json:"code"`
	VerificationURL string `json:"verificationUrl"`
	ExpiresIn       int    `json:"expiresIn"`
}

// DeviceAuthPollResponse is the poll response for device auth.
type DeviceAuthPollResponse struct {
	Status    string `json:"status"` // pending, approved, denied, expired
	Token     string `json:"token,omitempty"`
	UserEmail string `json:"userEmail,omitempty"`
}

// OpenRouterModel represents a model from the OpenRouter-compatible API.
type OpenRouterModel struct {
	ID                  string   `json:"id"`
	Name                string   `json:"name"`
	ContextLength       int      `json:"context_length"`
	MaxCompletionTokens *int     `json:"max_completion_tokens,omitempty"`
	Pricing             *Pricing `json:"pricing,omitempty"`
	Architecture        *Arch    `json:"architecture,omitempty"`
	TopProvider         *struct {
		MaxCompletionTokens *int `json:"max_completion_tokens,omitempty"`
	} `json:"top_provider,omitempty"`
	SupportedParams []string `json:"supported_parameters,omitempty"`
}

// Pricing represents model costs.
type Pricing struct {
	Prompt          string `json:"prompt,omitempty"`
	Completion      string `json:"completion,omitempty"`
	InputCacheWrite string `json:"input_cache_write,omitempty"`
	InputCacheRead  string `json:"input_cache_read,omitempty"`
}

// Arch represents model architecture details.
type Arch struct {
	InputModalities  []string `json:"input_modalities,omitempty"`
	OutputModalities []string `json:"output_modalities,omitempty"`
}

// ModelsResponse is the response from the models endpoint.
type ModelsResponse struct {
	Data []OpenRouterModel `json:"data"`
}

// BalanceResponse is the response from the balance endpoint.
type BalanceResponse struct {
	Balance float64 `json:"balance"`
}

// ─── Kilo Gateway Client ────────────────────────────────────────────

// KiloGateway handles all Kilo Gateway API interactions.
type KiloGateway struct {
	httpClient *http.Client

	cacheMu      sync.RWMutex
	modelsByToken map[string]cachedModelSet
}

type cachedModelSet struct {
	models    []*Model
	expiresAt time.Time
}

// NewKiloGateway creates a new Kilo Gateway client using the shared default HTTP client.
func NewKiloGateway() *KiloGateway {
	return NewKiloGatewayWithClient(httpclient.Default)
}

// NewKiloGatewayWithClient creates a new Kilo Gateway client with an injected HTTP client.
func NewKiloGatewayWithClient(client *http.Client) *KiloGateway {
	if client == nil {
		client = httpclient.Default
	}

	return &KiloGateway{
		httpClient:    client,
		modelsByToken: make(map[string]cachedModelSet),
	}
}

// ─── Model Fetching ─────────────────────────────────────────────────

// FetchModels fetches models from Kilo Gateway.
// If token is empty, only free models are returned.
func (g *KiloGateway) FetchModels(token string) ([]*Model, error) {
	cacheKey := strings.TrimSpace(token)
	isFreeOnly := cacheKey == ""
	if cached, ok := g.getCachedModels(cacheKey); ok {
		return cached, nil
	}

	headers := map[string]string{
		"Content-Type": "application/json",
		"User-Agent":   "synapta-kilo-provider",
	}
	if cacheKey != "" {
		headers["Authorization"] = "Bearer " + cacheKey
	}

	models, err := g.fetchModelsFromAPI(headers, isFreeOnly)
	if err != nil {
		return nil, err
	}

	g.setCachedModels(cacheKey, models)
	return cloneModels(models), nil
}

// FetchFreeModels fetches only free models (no auth required).
func (g *KiloGateway) FetchFreeModels() ([]*Model, error) {
	return g.FetchModels("")
}

func (g *KiloGateway) getCachedModels(cacheKey string) ([]*Model, bool) {
	g.cacheMu.RLock()
	cached, ok := g.modelsByToken[cacheKey]
	g.cacheMu.RUnlock()
	if !ok || time.Now().After(cached.expiresAt) {
		return nil, false
	}
	return cloneModels(cached.models), true
}

func (g *KiloGateway) setCachedModels(cacheKey string, models []*Model) {
	g.cacheMu.Lock()
	defer g.cacheMu.Unlock()
	g.modelsByToken[cacheKey] = cachedModelSet{
		models:    cloneModels(models),
		expiresAt: time.Now().Add(KiloModelsCacheTTL),
	}
}

func cloneModels(models []*Model) []*Model {
	if len(models) == 0 {
		return nil
	}
	out := make([]*Model, 0, len(models))
	for _, m := range models {
		if m == nil {
			continue
		}
		copyM := *m
		if len(m.Input) > 0 {
			copyM.Input = append([]InputModality(nil), m.Input...)
		}
		if m.Compat != nil {
			compatCopy := *m.Compat
			copyM.Compat = &compatCopy
		}
		out = append(out, &copyM)
	}
	return out
}

func (g *KiloGateway) fetchModelsFromAPI(headers map[string]string, freeOnly bool) ([]*Model, error) {
	req, err := http.NewRequest("GET", KiloGatewayBase+"/models", nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := g.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching models: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API error (HTTP %d): %s", resp.StatusCode, string(body))
	}

	var modelsResp ModelsResponse
	if err := json.NewDecoder(resp.Body).Decode(&modelsResp); err != nil {
		return nil, fmt.Errorf("parsing response: %w", err)
	}

	var models []*Model
	for _, m := range modelsResp.Data {
		// Skip image generation models
		if m.Architecture != nil {
			hasImageOutput := false
			for _, mod := range m.Architecture.OutputModalities {
				if mod == "image" {
					hasImageOutput = true
					break
				}
			}
			if hasImageOutput {
				continue
			}
		}

		// When unauthenticated, only show free models
		if freeOnly && !g.isFreeModel(m) {
			continue
		}

		models = append(models, g.mapModel(m))
	}

	return models, nil
}

// isFreeModel checks if a model is free to use.
func (g *KiloGateway) isFreeModel(m OpenRouterModel) bool {
	promptCost := parsePriceStr(m.Pricing.Prompt)
	completionCost := parsePriceStr(m.Pricing.Completion)

	if promptCost != 0 || completionCost != 0 {
		return false
	}

	// Check for :free suffix (OpenRouter convention)
	if strings.Contains(m.ID, ":free") {
		return true
	}

	// Kilo-native models (no vendor prefix)
	if !strings.Contains(m.ID, "/") {
		return true
	}

	// Known free routers
	if strings.HasPrefix(m.ID, "kilo/") || strings.HasPrefix(m.ID, "openrouter/") {
		return true
	}

	return false
}

// mapModel converts an OpenRouter model to our Model type.
func (g *KiloGateway) mapModel(m OpenRouterModel) *Model {
	supportsImages := false
	if m.Architecture != nil {
		for _, mod := range m.Architecture.InputModalities {
			if mod == "image" {
				supportsImages = true
				break
			}
		}
	}

	supportsReasoning := false
	for _, param := range m.SupportedParams {
		if param == "reasoning" {
			supportsReasoning = true
			break
		}
	}

	maxTokens := m.ContextLength / 5 // default to 20% of context
	if m.TopProvider != nil && m.TopProvider.MaxCompletionTokens != nil {
		maxTokens = *m.TopProvider.MaxCompletionTokens
	} else if m.MaxCompletionTokens != nil {
		maxTokens = *m.MaxCompletionTokens
	}

	inputModalities := []InputModality{InputText}
	if supportsImages {
		inputModalities = append(inputModalities, InputImage)
	}

	return &Model{
		ID:            m.ID,
		Name:          m.Name,
		Provider:      "kilo",
		API:           APIOpenAICompletions,
		Reasoning:     supportsReasoning,
		Input:         inputModalities,
		ContextWindow: m.ContextLength,
		MaxTokens:     maxTokens,
		Cost: Cost{
			Input:      parsePriceStr(m.Pricing.Prompt),
			Output:     parsePriceStr(m.Pricing.Completion),
			CacheRead:  parsePriceStr(m.Pricing.InputCacheRead),
			CacheWrite: parsePriceStr(m.Pricing.InputCacheWrite),
		},
	}
}

// ─── Device Auth Flow ───────────────────────────────────────────────

// DeviceAuthCallbacks provides callbacks for the device auth flow.
type DeviceAuthCallbacks struct {
	OnAuth     func(url string, code string)
	OnProgress func(message string)
	Signal     context.Context
}

// Login initiates the device auth flow and returns credentials.
func (g *KiloGateway) Login(ctx context.Context, callbacks DeviceAuthCallbacks) (*OAuthCredentials, error) {
	callbacks.OnProgress("Initiating device authorization...")

	// Step 1: Get device code
	authData, err := g.initiateDeviceAuth()
	if err != nil {
		return nil, fmt.Errorf("initiating device auth: %w", err)
	}

	// Step 2: Show verification URL and code
	callbacks.OnAuth(authData.VerificationURL, authData.Code)
	callbacks.OnProgress("Waiting for browser authorization...")

	// Step 3: Poll for approval
	deadline := time.Now().Add(time.Duration(authData.ExpiresIn) * time.Second)

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("login cancelled")
		case <-callbacks.Signal.Done():
			return nil, fmt.Errorf("login cancelled")
		default:
		}

		time.Sleep(KiloPollInterval)

		result, err := g.pollDeviceAuth(authData.Code)
		if err != nil {
			callbacks.OnProgress(fmt.Sprintf("Poll error: %v", err))
			continue
		}

		switch result.Status {
		case "approved":
			if result.Token == "" {
				return nil, fmt.Errorf("authorization approved but no token received")
			}
			callbacks.OnProgress("Login successful!")
			return &OAuthCredentials{
				Refresh: result.Token,
				Access:  result.Token,
				Expires: time.Now().Add(KiloTokenExpiry).UnixMilli(),
			}, nil

		case "denied":
			return nil, fmt.Errorf("authorization denied by user")

		case "expired":
			return nil, fmt.Errorf("authorization code expired, please try again")

		default: // pending
			remaining := time.Until(deadline).Truncate(time.Second)
			callbacks.OnProgress(fmt.Sprintf("Waiting for browser authorization... (%s remaining)", remaining))
		}
	}

	return nil, fmt.Errorf("authentication timed out")
}

func (g *KiloGateway) initiateDeviceAuth() (*DeviceAuthResponse, error) {
	req, err := http.NewRequest("POST", KiloDeviceAuthURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := g.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, fmt.Errorf("too many pending authorization requests, please try again later")
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	var authResp DeviceAuthResponse
	if err := json.NewDecoder(resp.Body).Decode(&authResp); err != nil {
		return nil, err
	}

	return &authResp, nil
}

func (g *KiloGateway) pollDeviceAuth(code string) (*DeviceAuthPollResponse, error) {
	url := fmt.Sprintf("%s/%s", KiloDeviceAuthURL, code)

	resp, err := g.httpClient.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusAccepted: // 202
		return &DeviceAuthPollResponse{Status: "pending"}, nil
	case http.StatusForbidden: // 403
		return &DeviceAuthPollResponse{Status: "denied"}, nil
	case http.StatusGone: // 410
		return &DeviceAuthPollResponse{Status: "expired"}, nil
	case http.StatusOK: // 200
		var pollResp DeviceAuthPollResponse
		if err := json.NewDecoder(resp.Body).Decode(&pollResp); err != nil {
			return nil, err
		}
		return &pollResp, nil
	default:
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(body))
	}
}

// ─── Balance ────────────────────────────────────────────────────────

// FetchBalance fetches the account balance.
func (g *KiloGateway) FetchBalance(ctx context.Context, token string) (float64, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", KiloProfileURL+"/balance", nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := g.httpClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	var balanceResp BalanceResponse
	if err := json.NewDecoder(resp.Body).Decode(&balanceResp); err != nil {
		return 0, err
	}

	return balanceResp.Balance, nil
}

// ─── Helpers ────────────────────────────────────────────────────────

// parsePriceStr parses an OpenRouter price string (per-token) to per-million-token.
func parsePriceStr(price string) float64 {
	if price == "" {
		return 0
	}
	var p float64
	fmt.Sscanf(price, "%f", &p)
	return p * 1_000_000
}

// FormatBalance formats a balance for display.
func FormatBalance(balance float64) string {
	if balance >= 1000 {
		return fmt.Sprintf("$%.1fk", balance/1000)
	}
	return fmt.Sprintf("$%.2f", balance)
}

// ─── Provider Factory ───────────────────────────────────────────────

// NewKiloProviderWithAuth creates a Kilo provider with the given token.
// If token is empty, creates a provider with free models only.
var defaultKiloGateway = NewKiloGateway()

func NewKiloProviderWithAuth(token string) (*KiloProvider, error) {
	gateway := defaultKiloGateway

	var models []*Model
	var err error

	if token != "" {
		models, err = gateway.FetchModels(token)
	} else {
		models, err = gateway.FetchFreeModels()
	}

	if err != nil {
		// Fallback to default models if fetch fails
		models = KiloDefaultModels()
	}

	headers := map[string]string{
		"X-KILOCODE-EDITORNAME": "Synapta",
		"User-Agent":            "synapta-kilo-provider",
	}

	return &KiloProvider{
		OpenAIProvider: NewOpenAIProvider(
			"kilo",
			"Kilo Gateway",
			KiloGatewayBase,
			token,
			headers,
			models,
			&CompatConfig{},
		),
	}, nil
}

// KiloAPIKeyEnv returns the environment variable name for Kilo API key.
func KiloAPIKeyEnv() string {
	return "KILO_API_KEY"
}
