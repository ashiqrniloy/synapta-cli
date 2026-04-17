// Package kilo registers the Kilo Gateway provider with the LLM provider
// registry.  Importing this package is the only action required to make the
// Kilo provider available — no changes to core chat logic are needed.
package kilo

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/ashiqrniloy/synapta-cli/internal/llm"
)

const (
	// ProviderID is the canonical provider identifier used throughout the app.
	ProviderID = "kilo"
)

// Descriptor returns the ProviderDescriptor for Kilo Gateway.
//
// Auth strategy: OAuth (device-code flow, long-lived token stored in
// AuthStorage; falls back to the KILO_API_KEY environment variable).
//
// Token refresh: Kilo tokens are valid for one year; we treat them as
// non-expiring for practical purposes — RefreshToken is a no-op that
// simply returns the existing access token.
//
// Model discovery: fetches the live model catalogue from the Kilo Gateway
// /models endpoint; falls back to the built-in curated list on error.
func Descriptor() *llm.ProviderDescriptor {
	return &llm.ProviderDescriptor{
		ID:     ProviderID,
		Name:   "Kilo Gateway",
		Auth:   llm.AuthStrategyOAuth,
		EnvVar: llm.KiloAPIKeyEnv(),

		ResolveToken: resolveToken,
		RefreshToken: refreshToken,
		Build:        build,
		DiscoverModels: func(token string) ([]*llm.Model, error) {
			return discoverModels(token)
		},
	}
}

// ─── token resolution ────────────────────────────────────────────────────────

func resolveToken(auth *llm.AuthStorage) (string, error) {
	if auth != nil {
		if entry := auth.Get(ProviderID); entry != nil {
			switch entry.Type {
			case "oauth":
				if entry.OAuth != nil && entry.OAuth.Access != "" {
					// Kilo tokens are year-long; skip expiry check for now.
					return entry.OAuth.Access, nil
				}
			case "api":
				if entry.API != nil && entry.API.APIKey != "" {
					return entry.API.APIKey, nil
				}
			}
		}
	}

	if ev := strings.TrimSpace(os.Getenv(llm.KiloAPIKeyEnv())); ev != "" {
		return ev, nil
	}

	// No credentials — return empty; caller treats Kilo as unavailable.
	return "", nil
}

// refreshToken is a no-op for Kilo because its device-auth tokens are
// valid for a year and there is no refresh endpoint.
func refreshToken(_ *llm.AuthStorage, creds *llm.OAuthCredentials) (string, error) {
	if creds == nil {
		return "", fmt.Errorf("kilo: cannot refresh nil credentials")
	}
	if time.Now().UnixMilli() >= creds.Expires {
		return "", fmt.Errorf("kilo: token expired; please re-authenticate with /add-provider")
	}
	return creds.Access, nil
}

// ─── provider factory ────────────────────────────────────────────────────────

func build(token string) (llm.Provider, error) {
	models, err := discoverModels(token)
	if err != nil || len(models) == 0 {
		models = llm.KiloDefaultModels()
	}

	headers := map[string]string{
		"X-KILOCODE-EDITORNAME": "Synapta",
		"User-Agent":            "synapta-kilo-provider",
	}
	return &llm.KiloProvider{
		OpenAIProvider: llm.NewOpenAIProvider(
			ProviderID,
			"Kilo Gateway",
			llm.KiloGatewayBase,
			token,
			headers,
			models,
			&llm.CompatConfig{},
		),
	}, nil
}

// ─── model discovery ─────────────────────────────────────────────────────────

func discoverModels(token string) ([]*llm.Model, error) {
	gateway := llm.NewKiloGateway()
	if token != "" {
		return gateway.FetchModels(token)
	}
	return gateway.FetchFreeModels()
}
