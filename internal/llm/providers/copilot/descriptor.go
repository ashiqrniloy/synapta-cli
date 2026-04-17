// Package copilot registers the GitHub Copilot provider with the LLM provider
// registry.  Importing this package is the only action required to make the
// GitHub Copilot provider available — no changes to core chat logic are needed.
package copilot

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/ashiqrniloy/synapta-cli/internal/llm"
	"github.com/ashiqrniloy/synapta-cli/internal/oauth"
)

const (
	// ProviderID is the canonical provider identifier used throughout the app.
	ProviderID = "github-copilot"

	// envVar is the optional environment variable for a pre-issued Copilot token.
	envVar = "GITHUB_COPILOT_API_KEY"

	// tokenExpiryGracePeriodMs is how far before the expiry timestamp we treat
	// a token as stale and trigger a refresh (60 seconds).
	tokenExpiryGracePeriodMs = 60_000
)

// Descriptor returns the ProviderDescriptor for GitHub Copilot.
//
// Auth strategy: OAuth (GitHub device-code flow producing a short-lived
// Copilot access token that must be refreshed via the GitHub API).
//
// Token refresh: the stored Refresh field holds the GitHub OAuth token;
// RefreshToken exchanges it for a new short-lived Copilot bearer token and
// persists the result back into AuthStorage.
//
// Model discovery: static list (GitHubCopilotDefaultModels) — Copilot does
// not expose a public model catalogue endpoint.
func Descriptor() *llm.ProviderDescriptor {
	return &llm.ProviderDescriptor{
		ID:     ProviderID,
		Name:   "GitHub Copilot",
		Auth:   llm.AuthStrategyOAuth,
		EnvVar: envVar,

		ResolveToken:   resolveToken,
		RefreshToken:   refreshToken,
		Build:          build,
		DiscoverModels: nil, // uses static default list embedded in the provider
	}
}

// ─── token resolution ────────────────────────────────────────────────────────

// resolveToken returns the current Copilot access token.
// If the stored token is expired (or missing), it triggers a refresh.
// Returns ("", nil) only when no credentials at all are available so that
// the caller can silently skip this provider without treating it as an error.
func resolveToken(auth *llm.AuthStorage) (string, error) {
	// 1. Check AuthStorage for an OAuth entry.
	if auth != nil {
		if entry := auth.Get(ProviderID); entry != nil {
			if entry.Type == "oauth" && entry.OAuth != nil {
				creds := entry.OAuth

				refreshNeeded := creds.Access == ""
				if !refreshNeeded && creds.Expires > 0 {
					refreshNeeded = time.Now().UnixMilli() >= creds.Expires-tokenExpiryGracePeriodMs
				}

				if !refreshNeeded {
					return creds.Access, nil
				}

				// Refresh only when we have a GitHub refresh token.
				if strings.TrimSpace(creds.Refresh) != "" {
					newToken, err := refreshToken(auth, creds)
					if err != nil {
						// Degraded: fall through to env-var fallback.
					} else if newToken != "" {
						return newToken, nil
					}
				}

				// Even without a refresh we can return what we have.
				if creds.Access != "" {
					return creds.Access, nil
				}
			}
		}
	}

	// 2. Environment-variable fallback.
	if ev := strings.TrimSpace(os.Getenv(envVar)); ev != "" {
		return ev, nil
	}

	// 3. No credentials available — provider is considered absent.
	return "", nil
}

// ─── token refresh ───────────────────────────────────────────────────────────

// refreshToken exchanges a stored GitHub OAuth refresh token for a new
// short-lived Copilot access token, persists it in auth, and returns the new
// access token.
func refreshToken(auth *llm.AuthStorage, creds *llm.OAuthCredentials) (string, error) {
	if creds == nil {
		return "", fmt.Errorf("copilot: cannot refresh nil credentials")
	}
	if strings.TrimSpace(creds.Refresh) == "" {
		return creds.Access, nil // nothing to refresh with
	}

	// Extract enterprise domain if previously stored.
	domain := ""
	if len(creds.ExtraData) > 0 {
		var extra oauth.CopilotExtraData
		if err := json.Unmarshal(creds.ExtraData, &extra); err == nil {
			domain = extra.EnterpriseDomain
		}
	}

	oauthProvider := oauth.NewGitHubCopilotOAuth(domain)
	refreshed, err := oauthProvider.RefreshToken(creds)
	if err != nil || refreshed == nil || strings.TrimSpace(refreshed.Access) == "" {
		return creds.Access, err // return stale token on soft failure
	}

	// Preserve extra data from the original credentials.
	if len(refreshed.ExtraData) == 0 && len(creds.ExtraData) > 0 {
		refreshed.ExtraData = creds.ExtraData
	}

	if auth != nil {
		_ = auth.SetOAuthCredentials(ProviderID, refreshed)
	}

	return refreshed.Access, nil
}

// ─── provider factory ────────────────────────────────────────────────────────

func build(token string) (llm.Provider, error) {
	if token == "" {
		return nil, fmt.Errorf("copilot: cannot build provider without an access token")
	}

	baseURL := oauth.GetBaseUrlFromToken(token)
	if baseURL == "" {
		baseURL = "https://api.individual.githubcopilot.com"
	}

	return llm.NewGitHubCopilotProvider(baseURL, token, llm.GitHubCopilotDefaultModels()), nil
}
