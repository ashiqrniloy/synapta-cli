package oauth

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/synapta/synapta-cli/internal/llm"
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

func (g *GitHubCopilotOAuth) ID() string {
	return "github-copilot"
}

func (g *GitHubCopilotOAuth) Name() string {
	return "GitHub Copilot"
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

		trimmed := strings.TrimSpace(input)
		if trimmed != "" {
			if !strings.Contains(trimmed, ".") {
				return nil, fmt.Errorf("invalid GitHub Enterprise domain: %s", trimmed)
			}
			domain = strings.TrimPrefix(trimmed, "https://")
			domain = strings.TrimPrefix(domain, "http://")
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

	extraData, _ := json.Marshal(&CopilotExtraData{EnterpriseDomain: domain})
	creds.ExtraData = extraData

	callbacks.OnProgress("Login successful!")
	return creds, nil
}

func (g *GitHubCopilotOAuth) startDeviceFlow(deviceURL string) (*deviceCodeResponse, error) {
	resp, err := http.Post(deviceURL, "application/x-www-form-urlencoded", strings.NewReader(
		url.Values{
			"client_id": {clientID},
			"scope":     {"read:user"},
		}.Encode(),
	))
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

	var result deviceCodeResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parsing device code response: %w", err)
	}

	if result.DeviceCode == "" || result.UserCode == "" {
		return nil, fmt.Errorf("invalid device code response")
	}

	return &result, nil
}

func (g *GitHubCopilotOAuth) pollForAccessToken(tokenURL, deviceCode string, interval, expiresIn int, callbacks llm.OAuthLoginCallbacks) (string, error) {
	deadline := time.Now().Add(time.Duration(expiresIn) * time.Second)
	pollInterval := time.Duration(interval) * time.Second

	for time.Now().Before(deadline) {
		if callbacks.Signal != nil {
			select {
			case <-callbacks.Signal.Done():
				return "", fmt.Errorf("login cancelled")
			default:
			}
		}

		time.Sleep(pollInterval)

		resp, err := http.Post(tokenURL, "application/x-www-form-urlencoded", strings.NewReader(
			url.Values{
				"client_id":  {clientID},
				"device_code": {deviceCode},
				"grant_type": {"urn:ietf:params:oauth:grant-type:device_code"},
			}.Encode(),
		))
		if err != nil {
			continue
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			continue
		}

		var result tokenResponse
		if err := json.Unmarshal(body, &result); err != nil {
			continue
		}

		if result.Error == "authorization_pending" {
			remaining := time.Until(deadline).Seconds()
			callbacks.OnProgress(fmt.Sprintf("Waiting for browser authorization... (%.0fs remaining)", remaining))
			continue
		}

		if result.Error == "slow_down" {
			pollInterval += 5 * time.Second
			continue
		}

		if result.Error != "" {
			return "", fmt.Errorf("authorization failed: %s - %s", result.Error, result.ErrorDesc)
		}

		if result.AccessToken == "" {
			return "", fmt.Errorf("no access token in response")
		}

		return result.AccessToken, nil
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

	expiresAt := time.Now().Add(time.Duration(result.ExpiresAt)*time.Second - 5*time.Minute).UnixMilli()

	return &llm.OAuthCredentials{
		Refresh: githubToken,
		Access:  result.Token,
		Expires: expiresAt,
	}, nil
}

func (g *GitHubCopilotOAuth) RefreshToken(credentials *llm.OAuthCredentials) (*llm.OAuthCredentials, error) {
	domain := g.getDomain()

	var extraData CopilotExtraData
	if err := json.Unmarshal(credentials.ExtraData, &extraData); err == nil && extraData.EnterpriseDomain != "" {
		domain = extraData.EnterpriseDomain
	}

	return g.refreshCopilotToken(credentials.Refresh, domain)
}

func (g *GitHubCopilotOAuth) GetAPIKey(credentials *llm.OAuthCredentials) string {
	return credentials.Access
}

func (g *GitHubCopilotOAuth) ModifyModels(models []*llm.Model, credentials *llm.OAuthCredentials) []*llm.Model {
	baseURL := GetBaseUrlFromToken(credentials.Access)
	if baseURL == "" {
		domain := g.getDomain()
		if domain != "github.com" {
			baseURL = fmt.Sprintf("https://copilot-api.%s", domain)
		} else {
			baseURL = "https://api.individual.githubcopilot.com"
		}
	}

	result := make([]*llm.Model, len(models))
	for i, m := range models {
		if m.Provider == "github-copilot" {
			modified := *m
			modified.BaseURL = baseURL
			result[i] = &modified
		} else {
			result[i] = m
		}
	}
	return result
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
