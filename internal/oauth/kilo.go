package oauth

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/ashiqrniloy/synapta-cli/internal/llm"
)

// Kilo Gateway OAuth provider implementation.

const (
	kiloAPIBaseDefault = "https://api.kilo.ai"
	kiloPollIntervalMs = 3000
	kiloTokenExpiryMs  = 365 * 24 * 60 * 60 * 1000 // 1 year
)

// KiloOAuth implements OAuthProvider for Kilo Gateway.
type KiloOAuth struct {
	APIBase string
}

// NewKiloOAuth creates a new Kilo OAuth provider.
func NewKiloOAuth() *KiloOAuth {
	return &KiloOAuth{
		APIBase: kiloAPIBaseDefault,
	}
}

func (k *KiloOAuth) ID() string {
	return "kilo"
}

func (k *KiloOAuth) Name() string {
	return "Kilo Gateway"
}

type kiloDeviceAuthResponse struct {
	Code            string `json:"code"`
	VerificationURL string `json:"verificationUrl"`
	ExpiresIn       int    `json:"expiresIn"`
}

type kiloDeviceAuthPollResponse struct {
	Status    string `json:"status"`
	Token     string `json:"token,omitempty"`
	UserEmail string `json:"userEmail,omitempty"`
}

type kiloBalanceResponse struct {
	Balance float64 `json:"balance,omitempty"`
}

type kiloModelsResponse struct {
	Data []kiloModel `json:"data,omitempty"`
}

type kiloModel struct {
	ID                  string                 `json:"id"`
	Name                string                 `json:"name"`
	ContextLength       int                    `json:"context_length"`
	MaxCompletionTokens *int                   `json:"max_completion_tokens,omitempty"`
	Pricing             *kiloModelPricing      `json:"pricing,omitempty"`
	Architecture        *kiloModelArchitecture `json:"architecture,omitempty"`
	TopProvider         *kiloTopProvider       `json:"top_provider,omitempty"`
	SupportedParameters []string               `json:"supported_parameters,omitempty"`
}

type kiloModelPricing struct {
	Prompt          *string `json:"prompt,omitempty"`
	Completion      *string `json:"completion,omitempty"`
	InputCacheRead  *string `json:"input_cache_read,omitempty"`
	InputCacheWrite *string `json:"input_cache_write,omitempty"`
}

type kiloModelArchitecture struct {
	InputModalities  []string `json:"input_modalities,omitempty"`
	OutputModalities []string `json:"output_modalities,omitempty"`
}

type kiloTopProvider struct {
	MaxCompletionTokens *int `json:"max_completion_tokens,omitempty"`
}

func (k *KiloOAuth) startDeviceAuth() (*kiloDeviceAuthResponse, error) {
	endpoint := fmt.Sprintf("%s/api/device-auth/codes", k.APIBase)

	resp, err := http.Post(endpoint, "application/json", nil)
	if err != nil {
		return nil, fmt.Errorf("device auth request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, fmt.Errorf("too many pending authorization requests, please try again later")
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("device auth failed (HTTP %d): %s", resp.StatusCode, string(body))
	}

	var result kiloDeviceAuthResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parsing device auth response: %w", err)
	}

	if result.Code == "" || result.VerificationURL == "" {
		return nil, fmt.Errorf("invalid device auth response")
	}

	return &result, nil
}

func (k *KiloOAuth) pollDeviceAuth(code string) (*kiloDeviceAuthPollResponse, error) {
	endpoint := fmt.Sprintf("%s/api/device-auth/codes/%s", k.APIBase, code)

	resp, err := http.Get(endpoint)
	if err != nil {
		return nil, fmt.Errorf("poll request failed: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusAccepted:
		return &kiloDeviceAuthPollResponse{Status: "pending"}, nil
	case http.StatusForbidden:
		return &kiloDeviceAuthPollResponse{Status: "denied"}, nil
	case http.StatusGone:
		return &kiloDeviceAuthPollResponse{Status: "expired"}, nil
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("poll failed (HTTP %d): %s", resp.StatusCode, string(body))
	}

	var result kiloDeviceAuthPollResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parsing poll response: %w", err)
	}

	return &result, nil
}

func (k *KiloOAuth) Login(callbacks llm.OAuthLoginCallbacks) (*llm.OAuthCredentials, error) {
	callbacks.OnProgress("Initiating device authorization...")

	authResp, err := k.startDeviceAuth()
	if err != nil {
		return nil, fmt.Errorf("starting device auth: %w", err)
	}

	callbacks.OnAuth(authResp.VerificationURL, fmt.Sprintf("Enter code: %s", authResp.Code))
	callbacks.OnProgress("Waiting for browser authorization...")

	deadline := time.Now().Add(time.Duration(authResp.ExpiresIn) * time.Second)

	for time.Now().Before(deadline) {
		if callbacks.Signal != nil {
			select {
			case <-callbacks.Signal.Done():
				return nil, fmt.Errorf("login cancelled")
			default:
			}
		}

		time.Sleep(time.Duration(kiloPollIntervalMs) * time.Millisecond)

		pollResp, err := k.pollDeviceAuth(authResp.Code)
		if err != nil {
			continue
		}

		switch pollResp.Status {
		case "approved":
			if pollResp.Token == "" {
				return nil, fmt.Errorf("authorization approved but no token received")
			}
			callbacks.OnProgress("Login successful!")
			return &llm.OAuthCredentials{
				Refresh: pollResp.Token,
				Access:  pollResp.Token,
				Expires: time.Now().Add(kiloTokenExpiryMs * time.Millisecond).UnixMilli(),
			}, nil

		case "denied":
			return nil, fmt.Errorf("authorization denied by user")

		case "expired":
			return nil, fmt.Errorf("authorization code expired, please try again")
		}

		remaining := time.Until(deadline).Seconds()
		callbacks.OnProgress(fmt.Sprintf("Waiting for browser authorization... (%.0fs remaining)", remaining))
	}

	return nil, fmt.Errorf("authentication timed out, please try again")
}

func (k *KiloOAuth) RefreshToken(credentials *llm.OAuthCredentials) (*llm.OAuthCredentials, error) {
	if credentials.Expires > time.Now().UnixMilli() {
		return credentials, nil
	}
	return nil, fmt.Errorf("Kilo token expired, please re-authenticate")
}

func (k *KiloOAuth) GetAPIKey(credentials *llm.OAuthCredentials) string {
	return credentials.Access
}

func (k *KiloOAuth) ModifyModels(models []*llm.Model, credentials *llm.OAuthCredentials) []*llm.Model {
	return models
}

// FetchModels fetches available models from Kilo Gateway.
func (k *KiloOAuth) FetchModels(token string, freeOnly bool) ([]*llm.Model, error) {
	endpoint := fmt.Sprintf("%s/api/gateway/models", k.APIBase)

	req, err := http.NewRequest("GET", endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "synapta-kilo-provider")

	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching models: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetching models failed (HTTP %d): %s", resp.StatusCode, string(body))
	}

	var modelsResp kiloModelsResponse
	if err := json.Unmarshal(body, &modelsResp); err != nil {
		return nil, fmt.Errorf("parsing models response: %w", err)
	}

	var models []*llm.Model
	for _, m := range modelsResp.Data {
		if m.Architecture != nil && contains(m.Architecture.OutputModalities, "image") {
			continue
		}

		if freeOnly && !isFreeModel(m) {
			continue
		}

		model := k.mapModel(m)
		models = append(models, model)
	}

	return models, nil
}

func (k *KiloOAuth) mapModel(m kiloModel) *llm.Model {
	var inputModalities []string
	if m.Architecture != nil {
		inputModalities = m.Architecture.InputModalities
	}
	if len(inputModalities) == 0 {
		inputModalities = []string{"text"}
	}

	var input []llm.InputModality
	for _, mod := range inputModalities {
		input = append(input, llm.InputModality(mod))
	}

	supportsReasoning := contains(m.SupportedParameters, "reasoning")

	maxTokens := 16384
	if m.TopProvider != nil && m.TopProvider.MaxCompletionTokens != nil {
		maxTokens = *m.TopProvider.MaxCompletionTokens
	} else if m.MaxCompletionTokens != nil {
		maxTokens = *m.MaxCompletionTokens
	} else {
		maxTokens = int(float64(m.ContextLength) * 0.2)
	}

	return &llm.Model{
		ID:            m.ID,
		Name:          m.Name,
		Provider:      "kilo",
		API:           llm.APIOpenAICompletions,
		BaseURL:       fmt.Sprintf("%s/api/gateway", k.APIBase),
		Reasoning:     supportsReasoning,
		Input:         input,
		Cost:          k.parseCost(m.Pricing),
		ContextWindow: m.ContextLength,
		MaxTokens:     maxTokens,
	}
}

func (k *KiloOAuth) parseCost(pricing *kiloModelPricing) llm.Cost {
	if pricing == nil {
		return llm.Cost{}
	}
	return llm.Cost{
		Input:      parsePrice(pricing.Prompt),
		Output:     parsePrice(pricing.Completion),
		CacheRead:  parsePrice(pricing.InputCacheRead),
		CacheWrite: parsePrice(pricing.InputCacheWrite),
	}
}

// FetchBalance fetches the user's credit balance.
func (k *KiloOAuth) FetchBalance(token string) (float64, error) {
	endpoint := fmt.Sprintf("%s/api/profile/balance", k.APIBase)

	req, err := http.NewRequest("GET", endpoint, nil)
	if err != nil {
		return 0, fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("fetching balance: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("fetching balance failed (HTTP %d)", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, fmt.Errorf("reading response: %w", err)
	}

	var result kiloBalanceResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return 0, fmt.Errorf("parsing balance response: %w", err)
	}

	return result.Balance, nil
}

func parsePrice(price *string) float64 {
	if price == nil {
		return 0
	}
	parsed := 0.0
	fmt.Sscanf(*price, "%f", &parsed)
	return parsed * 1_000_000
}

func isFreeModel(m kiloModel) bool {
	if m.Pricing != nil && m.Pricing.Prompt != nil && m.Pricing.Completion != nil {
		prompt := 0.0
		completion := 0.0
		fmt.Sscanf(*m.Pricing.Prompt, "%f", &prompt)
		fmt.Sscanf(*m.Pricing.Completion, "%f", &completion)
		if prompt != 0 || completion != 0 {
			return false
		}
	}
	if strings.Contains(m.ID, ":free") {
		return true
	}
	if !strings.Contains(m.ID, "/") {
		return true
	}
	if strings.HasPrefix(m.ID, "kilo/") || strings.HasPrefix(m.ID, "openrouter/") {
		return true
	}
	return false
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
