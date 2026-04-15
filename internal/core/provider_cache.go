package core

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/ashiqrniloy/synapta-cli/internal/llm"
	"github.com/ashiqrniloy/synapta-cli/internal/oauth"
)

func (s *ChatService) providerFor(providerID string) (llm.Provider, error) {
	var token string

	switch providerID {
	case ProviderKilo:
		token = s.tokenFromAuthOrEnv(ProviderKilo, llm.KiloAPIKeyEnv())
	case ProviderGitHubCopilot:
		token = s.tokenFromAuthOrEnv(ProviderGitHubCopilot, "GITHUB_COPILOT_API_KEY")
		if token == "" {
			s.invalidateProvider(providerID)
			return nil, fmt.Errorf("no credentials for provider %q", ProviderGitHubCopilot)
		}
	default:
		return nil, fmt.Errorf("unsupported provider: %s", providerID)
	}

	s.mu.RLock()
	cached, ok := s.providers[providerID]
	s.mu.RUnlock()
	if ok && cached.provider != nil && cached.token == token {
		return cached.provider, nil
	}

	var provider llm.Provider
	switch providerID {
	case ProviderKilo:
		kiloProvider, err := llm.NewKiloProviderWithAuth(token)
		if err != nil {
			return nil, fmt.Errorf("creating kilo provider: %w", err)
		}
		provider = kiloProvider
	case ProviderGitHubCopilot:
		baseURL := oauth.GetBaseUrlFromToken(token)
		if baseURL == "" {
			baseURL = "https://api.individual.githubcopilot.com"
		}
		provider = llm.NewGitHubCopilotProvider(baseURL, token, llm.GitHubCopilotDefaultModels())
	default:
		return nil, fmt.Errorf("unsupported provider: %s", providerID)
	}

	s.mu.Lock()
	s.providers[providerID] = cachedProvider{provider: provider, token: token}
	s.mu.Unlock()

	return provider, nil
}

func (s *ChatService) invalidateProvider(providerID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.providers, providerID)
}

func (s *ChatService) tokenFromAuthOrEnv(providerID, envVar string) string {
	if s.auth != nil {
		if entry := s.auth.Get(providerID); entry != nil {
			switch entry.Type {
			case "oauth":
				if entry.OAuth != nil {
					if providerID == ProviderGitHubCopilot {
						if token := s.ensureFreshCopilotToken(entry.OAuth); token != "" {
							return token
						}
					}
					if entry.OAuth.Access != "" {
						return entry.OAuth.Access
					}
				}
			case "api":
				if entry.API != nil && entry.API.APIKey != "" {
					return entry.API.APIKey
				}
			}
		}
	}
	if envVar == "" {
		return ""
	}
	return strings.TrimSpace(os.Getenv(envVar))
}

func (s *ChatService) ensureFreshCopilotToken(creds *llm.OAuthCredentials) string {
	if creds == nil {
		return ""
	}
	if creds.Access == "" && creds.Refresh == "" {
		return ""
	}

	refreshNeeded := creds.Access == ""
	if !refreshNeeded && creds.Expires > 0 {
		refreshNeeded = time.Now().UnixMilli() >= creds.Expires-60_000
	}
	if !refreshNeeded {
		return creds.Access
	}
	if strings.TrimSpace(creds.Refresh) == "" {
		return creds.Access
	}

	provider := oauth.NewGitHubCopilotOAuth("")
	refreshed, err := provider.RefreshToken(creds)
	if err != nil || refreshed == nil || strings.TrimSpace(refreshed.Access) == "" {
		return creds.Access
	}
	if len(refreshed.ExtraData) == 0 && len(creds.ExtraData) > 0 {
		refreshed.ExtraData = creds.ExtraData
	}
	if s.auth != nil {
		_ = s.auth.SetOAuthCredentials(ProviderGitHubCopilot, refreshed)
	}

	// Auth credentials changed; ensure cached provider is rebuilt with fresh token.
	s.invalidateProvider(ProviderGitHubCopilot)

	return refreshed.Access
}
