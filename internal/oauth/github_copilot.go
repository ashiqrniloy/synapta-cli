package oauth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ashiqrniloy/synapta-cli/internal/llm"
)

// GitHub Copilot OAuth provider implementation.

const encodedClientID = "SXYxLmI1MDdhMDhjODdlY2ZlOTg="

var clientID string

func init() {
	decoded, err := base64.StdEncoding.DecodeString(encodedClientID)
	if err != nil {
		clientID = ""
		return
	}
	clientID = string(decoded)
}

// CopilotHeaders are required for GitHub Copilot API requests.
var CopilotHeaders = map[string]string{
	"User-Agent":             "GitHubCopilotChat/0.35.0",
	"Editor-Version":         "vscode/1.107.0",
	"Editor-Plugin-Version":  "copilot-chat/0.35.0",
	"Copilot-Integration-Id": "vscode-chat",
}

// GitHubCopilotOAuth implements OAuthProvider for GitHub Copilot.
type GitHubCopilotOAuth struct {
	EnterpriseDomain string
}

// NewGitHubCopilotOAuth creates a new GitHub Copilot OAuth provider.
func NewGitHubCopilotOAuth(enterpriseDomain string) *GitHubCopilotOAuth {
	return &GitHubCopilotOAuth{
		EnterpriseDomain: enterpriseDomain,
	}
}

type deviceCodeResponse struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	Interval        int    `json:"interval"`
	ExpiresIn       int    `json:"expires_in"`
}

type tokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	Scope       string `json:"scope"`
	Error       string `json:"error,omitempty"`
	ErrorDesc   string `json:"error_description,omitempty"`
	Interval    int    `json:"interval,omitempty"`
}

type copilotTokenResponse struct {
	Token     string `json:"token"`
	ExpiresAt int64  `json:"expires_at"`
}

type CopilotExtraData struct {
	EnterpriseDomain string `json:"enterpriseDomain,omitempty"`
}

func (g *GitHubCopilotOAuth) getDomain() string {
	if g.EnterpriseDomain != "" {
		return g.EnterpriseDomain
	}
	return "github.com"
}

func (g *GitHubCopilotOAuth) getURLs(domain string) (deviceCode, accessToken, copilotToken string) {
	return fmt.Sprintf("https://%s/login/device/code", domain),
		fmt.Sprintf("https://%s/login/oauth/access_token", domain),
		fmt.Sprintf("https://api.%s/copilot_internal/v2/token", domain)
}

func normalizeDomain(input string) (string, bool) {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return "", true
	}
	if strings.Contains(trimmed, "://") {
		u, err := url.Parse(trimmed)
		if err != nil || strings.TrimSpace(u.Hostname()) == "" {
			return "", false
		}
		return strings.TrimSpace(u.Hostname()), true
	}
	if strings.Contains(trimmed, "/") || strings.HasPrefix(trimmed, ".") || strings.HasSuffix(trimmed, ".") || !strings.Contains(trimmed, ".") {
		return "", false
	}
	return trimmed, true
}

func (g *GitHubCopilotOAuth) Login(callbacks llm.OAuthLoginCallbacks) (*llm.OAuthCredentials, error) {
	domain := g.getDomain()
	if g.EnterpriseDomain == "" {
		input, err := callbacks.OnPrompt("GitHub Enterprise URL/domain (blank for github.com)", "company.ghe.com", true)
		if err != nil {
			return nil, fmt.Errorf("prompt cancelled: %w", err)
		}

		if callbacks.Signal != nil {
			select {
			case <-callbacks.Signal.Done():
				return nil, fmt.Errorf("login cancelled")
			default:
			}
		}

		normalized, ok := normalizeDomain(input)
		if !ok {
			return nil, fmt.Errorf("invalid GitHub Enterprise domain: %s", strings.TrimSpace(input))
		}
		if normalized != "" {
			domain = normalized
		}
	}

	callbacks.OnProgress("Initiating device authorization...")

	deviceURL, tokenURL, _ := g.getURLs(domain)

	deviceResp, err := g.startDeviceFlow(deviceURL)
	if err != nil {
		return nil, fmt.Errorf("starting device flow: %w", err)
	}

	callbacks.OnAuth(deviceResp.VerificationURI, fmt.Sprintf("Enter code: %s", deviceResp.UserCode))
	callbacks.OnProgress("Waiting for browser authorization...")

	githubToken, err := g.pollForAccessToken(tokenURL, deviceResp.DeviceCode, deviceResp.Interval, deviceResp.ExpiresIn, callbacks)
	if err != nil {
		return nil, fmt.Errorf("polling for access token: %w", err)
	}

	creds, err := g.refreshCopilotToken(githubToken, domain)
	if err != nil {
		return nil, fmt.Errorf("exchanging for Copilot token: %w", err)
	}

	callbacks.OnProgress("Enabling models...")
	g.enableAllModels(creds.Access, domain)

	extraData, _ := json.Marshal(&CopilotExtraData{EnterpriseDomain: domain})
	creds.ExtraData = extraData

	callbacks.OnProgress("Login successful!")
	return creds, nil
}

func (g *GitHubCopilotOAuth) startDeviceFlow(deviceURL string) (*deviceCodeResponse, error) {
	req, err := http.NewRequest("POST", deviceURL, strings.NewReader(url.Values{
		"client_id": {clientID},
		"scope":     {"read:user"},
	}.Encode()))
	if err != nil {
		return nil, fmt.Errorf("creating device flow request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", CopilotHeaders["User-Agent"])

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("device flow request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("device flow failed (HTTP %d): %s", resp.StatusCode, string(body))
	}

	result, err := parseDeviceCodeResponse(body)
	if err != nil {
		return nil, fmt.Errorf("parsing device code response: %w", err)
	}
	if result.DeviceCode == "" || result.UserCode == "" || result.VerificationURI == "" || result.Interval <= 0 || result.ExpiresIn <= 0 {
		return nil, fmt.Errorf("invalid device code response")
	}

	return &result, nil
}

func parseDeviceCodeResponse(body []byte) (deviceCodeResponse, error) {
	var result deviceCodeResponse
	if err := json.Unmarshal(body, &result); err == nil {
		return result, nil
	}
	values, err := url.ParseQuery(string(body))
	if err != nil {
		return deviceCodeResponse{}, err
	}
	result.DeviceCode = strings.TrimSpace(values.Get("device_code"))
	result.UserCode = strings.TrimSpace(values.Get("user_code"))
	result.VerificationURI = strings.TrimSpace(values.Get("verification_uri"))
	if result.VerificationURI == "" {
		result.VerificationURI = strings.TrimSpace(values.Get("verification_url"))
	}
	fmt.Sscanf(strings.TrimSpace(values.Get("interval")), "%d", &result.Interval)
	fmt.Sscanf(strings.TrimSpace(values.Get("expires_in")), "%d", &result.ExpiresIn)
	return result, nil
}

func parseTokenResponse(body []byte) (tokenResponse, error) {
	var result tokenResponse
	if err := json.Unmarshal(body, &result); err == nil {
		return result, nil
	}
	values, err := url.ParseQuery(string(body))
	if err != nil {
		return tokenResponse{}, err
	}
	result.AccessToken = strings.TrimSpace(values.Get("access_token"))
	result.TokenType = strings.TrimSpace(values.Get("token_type"))
	result.Scope = strings.TrimSpace(values.Get("scope"))
	result.Error = strings.TrimSpace(values.Get("error"))
	result.ErrorDesc = strings.TrimSpace(values.Get("error_description"))
	fmt.Sscanf(strings.TrimSpace(values.Get("interval")), "%d", &result.Interval)
	return result, nil
}

func sleepWithCancellation(d time.Duration, signal context.Context) error {
	if d <= 0 {
		return nil
	}
	if signal == nil {
		time.Sleep(d)
		return nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-signal.Done():
		return fmt.Errorf("login cancelled")
	}
}

func (g *GitHubCopilotOAuth) pollForAccessToken(tokenURL, deviceCode string, interval, expiresIn int, callbacks llm.OAuthLoginCallbacks) (string, error) {
	deadline := time.Now().Add(time.Duration(expiresIn) * time.Second)
	pollInterval := time.Duration(maxInt(interval, 1)) * time.Second
	intervalMultiplier := 1.2
	slowDownResponses := 0

	for time.Now().Before(deadline) {
		if callbacks.Signal != nil {
			select {
			case <-callbacks.Signal.Done():
				return "", fmt.Errorf("login cancelled")
			default:
			}
		}

		remaining := time.Until(deadline)
		wait := time.Duration(math.Ceil(float64(pollInterval) * intervalMultiplier))
		if wait > remaining {
			wait = remaining
		}
		if err := sleepWithCancellation(wait, callbacks.Signal); err != nil {
			return "", err
		}

		req, err := http.NewRequest("POST", tokenURL, strings.NewReader(url.Values{
			"client_id":   {clientID},
			"device_code": {deviceCode},
			"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
		}.Encode()))
		if err != nil {
			continue
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", CopilotHeaders["User-Agent"])

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			continue
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			continue
		}
		if resp.StatusCode != http.StatusOK {
			continue
		}

		result, err := parseTokenResponse(body)
		if err != nil {
			continue
		}

		if result.AccessToken != "" {
			return result.AccessToken, nil
		}

		switch result.Error {
		case "authorization_pending":
			continue
		case "slow_down":
			slowDownResponses++
			if result.Interval > 0 {
				pollInterval = time.Duration(result.Interval) * time.Second
			} else {
				pollInterval += 5 * time.Second
			}
			intervalMultiplier = 1.4
			continue
		case "":
			return "", fmt.Errorf("no access token in response")
		default:
			if result.ErrorDesc != "" {
				return "", fmt.Errorf("authorization failed: %s - %s", result.Error, result.ErrorDesc)
			}
			return "", fmt.Errorf("authorization failed: %s", result.Error)
		}
	}

	if slowDownResponses > 0 {
		return "", fmt.Errorf("device flow timed out after slow_down responses (check system clock and retry)")
	}
	return "", fmt.Errorf("device flow timed out")
}

func (g *GitHubCopilotOAuth) refreshCopilotToken(githubToken, domain string) (*llm.OAuthCredentials, error) {
	copilotTokenURL := fmt.Sprintf("https://api.%s/copilot_internal/v2/token", domain)

	req, err := http.NewRequest("GET", copilotTokenURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+githubToken)
	req.Header.Set("Accept", "application/json")
	for k, v := range CopilotHeaders {
		req.Header.Set(k, v)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("copilot token request failed (HTTP %d): %s", resp.StatusCode, string(body))
	}

	var result copilotTokenResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parsing copilot token response: %w", err)
	}

	if result.Token == "" {
		return nil, fmt.Errorf("no token in copilot response")
	}

	now := time.Now().Unix()
	expiresMs := result.ExpiresAt * 1000
	if result.ExpiresAt <= 0 {
		expiresMs = time.Now().Add(55 * time.Minute).UnixMilli()
	} else if result.ExpiresAt < now-60 {
		expiresMs = time.Now().Add(time.Duration(result.ExpiresAt) * time.Second).UnixMilli()
	}
	expiresMs -= int64((5 * time.Minute).Milliseconds())

	return &llm.OAuthCredentials{
		Refresh: githubToken,
		Access:  result.Token,
		Expires: expiresMs,
	}, nil
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func (g *GitHubCopilotOAuth) enableModel(copilotToken, modelID, domain string) bool {
	if strings.TrimSpace(copilotToken) == "" || strings.TrimSpace(modelID) == "" {
		return false
	}
	baseURL := GetBaseUrlFromToken(copilotToken)
	if baseURL == "" {
		if strings.TrimSpace(domain) != "" && domain != "github.com" {
			baseURL = fmt.Sprintf("https://copilot-api.%s", domain)
		} else {
			baseURL = "https://api.individual.githubcopilot.com"
		}
	}

	endpoint := fmt.Sprintf("%s/models/%s/policy", strings.TrimRight(baseURL, "/"), modelID)
	req, err := http.NewRequest("POST", endpoint, strings.NewReader(`{"state":"enabled"}`))
	if err != nil {
		return false
	}
	req.Header.Set("Authorization", "Bearer "+copilotToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("openai-intent", "chat-policy")
	req.Header.Set("x-interaction-type", "chat-policy")
	for k, v := range CopilotHeaders {
		req.Header.Set(k, v)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
	return resp.StatusCode >= 200 && resp.StatusCode < 300
}

func (g *GitHubCopilotOAuth) enableAllModels(copilotToken, domain string) {
	for _, model := range llm.GitHubCopilotDefaultModels() {
		_ = g.enableModel(copilotToken, model.ID, domain)
	}
}

func (g *GitHubCopilotOAuth) RefreshToken(credentials *llm.OAuthCredentials) (*llm.OAuthCredentials, error) {
	domain := g.getDomain()

	var extraData CopilotExtraData
	if err := json.Unmarshal(credentials.ExtraData, &extraData); err == nil && extraData.EnterpriseDomain != "" {
		domain = extraData.EnterpriseDomain
	}

	return g.refreshCopilotToken(credentials.Refresh, domain)
}

type CopilotPremiumUsage struct {
	Used  int
	Total int
}

// FetchCopilotPremiumUsage attempts to fetch monthly premium request usage.
// This data is not guaranteed to be available for all accounts/plans.
func FetchCopilotPremiumUsage(githubToken, domain string) (*CopilotPremiumUsage, error) {
	if strings.TrimSpace(githubToken) == "" {
		return nil, fmt.Errorf("missing github token")
	}
	if strings.TrimSpace(domain) == "" {
		domain = "github.com"
	}

	endpoints := []string{fmt.Sprintf("https://api.%s/copilot_internal/user", domain)}
	if domain == "github.com" {
		endpoints = append([]string{"https://api.github.com/copilot_internal/user"}, endpoints...)
	}

	for _, endpoint := range endpoints {
		req, err := http.NewRequest("GET", endpoint, nil)
		if err != nil {
			continue
		}
		req.Header.Set("Authorization", "Bearer "+githubToken)
		req.Header.Set("Accept", "application/json")
		for k, v := range CopilotHeaders {
			req.Header.Set(k, v)
		}

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			continue
		}

		used, total, ok := parsePremiumUsage(body)
		if ok {
			return &CopilotPremiumUsage{Used: used, Total: total}, nil
		}
	}

	return nil, fmt.Errorf("premium usage not available")
}

func parsePremiumUsage(body []byte) (used int, total int, ok bool) {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return 0, 0, false
	}

	usedKeys := []string{
		"monthly_premium_interactions_used",
		"premium_interactions_used",
		"chat_premium_requests_used",
		"premium_requests_used",
		"used_this_month",
		"used",
	}
	totalKeys := []string{
		"monthly_premium_interactions_limit",
		"premium_interactions_limit",
		"chat_premium_requests_total",
		"premium_requests_total",
		"limit",
		"total",
	}

	used, uok := findFirstInt(payload, usedKeys)
	total, tok := findFirstInt(payload, totalKeys)
	if uok && tok && total > 0 {
		return used, total, true
	}
	return 0, 0, false
}

func findFirstInt(v any, keys []string) (int, bool) {
	for _, key := range keys {
		if val, ok := findIntByKey(v, key); ok {
			return val, true
		}
	}
	return 0, false
}

func findIntByKey(v any, target string) (int, bool) {
	switch n := v.(type) {
	case map[string]any:
		for k, child := range n {
			if strings.EqualFold(k, target) {
				switch x := child.(type) {
				case float64:
					return int(x), true
				case int:
					return x, true
				case int64:
					return int(x), true
				}
			}
			if val, ok := findIntByKey(child, target); ok {
				return val, true
			}
		}
	case []any:
		for _, child := range n {
			if val, ok := findIntByKey(child, target); ok {
				return val, true
			}
		}
	}
	return 0, false
}

// GetBaseUrlFromToken extracts the API base URL from a Copilot token.
func GetBaseUrlFromToken(token string) string {
	parts := strings.Split(token, ";")
	for _, part := range parts {
		if strings.HasPrefix(part, "proxy-ep=") {
			proxyHost := strings.TrimPrefix(part, "proxy-ep=")
			apiHost := strings.TrimPrefix(proxyHost, "proxy.")
			apiHost = "api." + apiHost
			return fmt.Sprintf("https://%s", apiHost)
		}
	}
	return ""
}
