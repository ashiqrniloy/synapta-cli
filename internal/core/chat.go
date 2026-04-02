package core

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/synapta/synapta-cli/internal/llm"
	"github.com/synapta/synapta-cli/internal/oauth"
)

const (
	ProviderKilo          = "kilo"
	ProviderGitHubCopilot = "github-copilot"
)

// ChatService provides a generic interface to send/receive chat messages
// with whichever LLM provider/model is currently selected.
type ChatService struct {
	auth *llm.AuthStorage
}

func NewChatService(auth *llm.AuthStorage) *ChatService {
	return &ChatService{auth: auth}
}

// AvailableModels returns models that can be selected in the UI.
// - Kilo returns authenticated models when a token is available, otherwise free models.
// - GitHub Copilot returns models only when authenticated.
func (s *ChatService) AvailableModels(ctx context.Context) ([]*llm.Model, error) {
	_ = ctx

	models := make([]*llm.Model, 0)

	kiloProvider, err := s.providerFor(ProviderKilo)
	if err == nil {
		models = append(models, kiloProvider.Models()...)
	}

	copilotProvider, err := s.providerFor(ProviderGitHubCopilot)
	if err == nil {
		models = append(models, copilotProvider.Models()...)
	}

	if len(models) == 0 {
		return nil, fmt.Errorf("no models available: authenticate with /add-provider or set provider credentials")
	}

	return models, nil
}

// Stream sends messages to the selected provider/model and streams assistant deltas.
func (s *ChatService) Stream(
	ctx context.Context,
	providerID string,
	modelID string,
	messages []llm.Message,
	onDelta func(text string) error,
) error {
	provider, err := s.providerFor(providerID)
	if err != nil {
		return err
	}

	streamReq := llm.ChatRequest{
		Model:    modelID,
		Messages: messages,
		Stream:   true,
	}

	receivedDelta := false
	err = provider.ChatStream(ctx, streamReq, func(chunk llm.StreamChunk) error {
		for _, choice := range chunk.Choices {
			piece := choice.Delta.Content
			if piece == "" {
				piece = choice.Message.Content
			}
			if piece == "" {
				piece = choice.Text
			}
			if piece == "" {
				continue
			}
			receivedDelta = true
			if err := onDelta(piece); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	if receivedDelta {
		return nil
	}

	// Fallback for providers/models that don't emit proper streaming deltas.
	resp, err := provider.Chat(ctx, llm.ChatRequest{
		Model:    modelID,
		Messages: messages,
		Stream:   false,
	})
	if err != nil {
		return fmt.Errorf("stream produced no deltas and fallback chat failed: %w", err)
	}
	if resp == nil || len(resp.Choices) == 0 {
		return fmt.Errorf("empty response from provider %q model %q", providerID, modelID)
	}

	content := strings.TrimSpace(resp.Choices[0].Message.Content)
	if content == "" {
		return fmt.Errorf("provider %q model %q returned empty content", providerID, modelID)
	}
	return onDelta(content)
}

func (s *ChatService) providerFor(providerID string) (llm.Provider, error) {
	switch providerID {
	case ProviderKilo:
		token := s.tokenFromAuthOrEnv(ProviderKilo, llm.KiloAPIKeyEnv())
		provider, err := llm.NewKiloProviderWithAuth(token)
		if err != nil {
			return nil, fmt.Errorf("creating kilo provider: %w", err)
		}
		return provider, nil

	case ProviderGitHubCopilot:
		token := s.tokenFromAuthOrEnv(ProviderGitHubCopilot, "GITHUB_COPILOT_API_KEY")
		if token == "" {
			return nil, fmt.Errorf("no credentials for provider %q", ProviderGitHubCopilot)
		}
		baseURL := oauth.GetBaseUrlFromToken(token)
		if baseURL == "" {
			baseURL = "https://api.individual.githubcopilot.com"
		}
		provider := llm.NewGitHubCopilotProvider(baseURL, token, llm.GitHubCopilotDefaultModels())
		return provider, nil

	default:
		return nil, fmt.Errorf("unsupported provider: %s", providerID)
	}
}

func (s *ChatService) tokenFromAuthOrEnv(providerID, envVar string) string {
	if s.auth != nil {
		if entry := s.auth.Get(providerID); entry != nil {
			switch entry.Type {
			case "oauth":
				if entry.OAuth != nil && entry.OAuth.Access != "" {
					return entry.OAuth.Access
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
