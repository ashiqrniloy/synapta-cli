package application

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/ashiqrniloy/synapta-cli/internal/llm"
	"github.com/ashiqrniloy/synapta-cli/internal/oauth"
)

type ModelInfo struct {
	Provider string
	ID       string
	Name     string
}

type ProviderService struct {
	authStorage    *llm.AuthStorage
	chatController *ChatController
}

func NewProviderService(authStorage *llm.AuthStorage, chatController *ChatController) *ProviderService {
	return &ProviderService{authStorage: authStorage, chatController: chatController}
}

func (s *ProviderService) SetChatController(chatController *ChatController) {
	s.chatController = chatController
}

func (s *ProviderService) FetchBalance(ctx context.Context, providerID string) (string, error) {
	if s.authStorage == nil {
		return "", nil
	}

	switch providerID {
	case "kilo":
		creds, err := s.authStorage.GetOAuthCredentials("kilo")
		if err != nil || creds == nil || strings.TrimSpace(creds.Access) == "" {
			return "", nil
		}
		gateway := llm.NewKiloGateway()
		balance, err := gateway.FetchBalance(ctx, creds.Access)
		if err != nil {
			return "", err
		}
		return llm.FormatBalance(balance), nil
	case "github-copilot":
		creds, err := s.authStorage.GetOAuthCredentials("github-copilot")
		if err != nil || creds == nil || strings.TrimSpace(creds.Refresh) == "" {
			return "", nil
		}
		domain := "github.com"
		if len(creds.ExtraData) > 0 {
			var extra oauth.CopilotExtraData
			if err := json.Unmarshal(creds.ExtraData, &extra); err == nil && strings.TrimSpace(extra.EnterpriseDomain) != "" {
				domain = strings.TrimSpace(extra.EnterpriseDomain)
			}
		}
		usage, err := oauth.FetchCopilotPremiumUsage(ctx, creds.Refresh, domain)
		if err != nil || usage == nil || usage.Total <= 0 {
			return "", nil
		}
		return fmt.Sprintf("Premium %d/%d", usage.Used, usage.Total), nil
	default:
		return "", nil
	}
}

func (s *ProviderService) AvailableModels(ctx context.Context, rankProvider func(string) int, modelTier func(string, string) string) ([]ModelInfo, error) {
	if s.chatController == nil {
		return nil, nil
	}

	available, err := s.chatController.AvailableModels(ctx)
	if err != nil {
		return nil, err
	}

	models := make([]ModelInfo, 0, len(available))
	for _, model := range available {
		name := model.Name
		if modelTier != nil {
			if tier := modelTier(model.Provider, model.ID); tier != "" {
				name = fmt.Sprintf("%s [%s]", name, strings.ToUpper(tier))
			}
		}
		models = append(models, ModelInfo{Provider: model.Provider, ID: model.ID, Name: name})
	}

	sort.SliceStable(models, func(i, j int) bool {
		ri, rj := 0, 0
		if rankProvider != nil {
			ri, rj = rankProvider(models[i].Provider), rankProvider(models[j].Provider)
		}
		if ri != rj {
			return ri < rj
		}
		if models[i].Provider != models[j].Provider {
			return models[i].Provider < models[j].Provider
		}
		return strings.ToLower(models[i].Name) < strings.ToLower(models[j].Name)
	})

	return models, nil
}

func (s *ProviderService) AuthenticateKilo(ctx context.Context, onAuthURL func(string)) (int, error) {
	gateway := llm.NewKiloGateway()
	var verificationURL string
	creds, err := gateway.Login(ctx, llm.DeviceAuthCallbacks{
		OnAuth: func(url, code string) {
			verificationURL = url
			if onAuthURL != nil {
				onAuthURL(url)
			}
		},
		OnProgress: func(msg string) {},
		Signal:     ctx,
	})
	if err != nil {
		if verificationURL != "" {
			return 0, fmt.Errorf("%w\nOpen this URL: %s", err, verificationURL)
		}
		return 0, err
	}
	if s.authStorage != nil {
		if err := s.authStorage.SetOAuthCredentials("kilo", creds); err != nil {
			return 0, fmt.Errorf("authenticated but failed to store credentials: %w", err)
		}
	}
	models, err := gateway.FetchModels(creds.Access)
	if err != nil {
		return 0, fmt.Errorf("authenticated but failed to fetch models: %w", err)
	}
	if s.chatController != nil {
		s.chatController.InvalidateProviderCache()
	}
	return len(models), nil
}

type CopilotAuthEvent struct {
	Progress string
	Prompt   *CopilotAuthPrompt
	Complete *CopilotAuthComplete
}

type CopilotAuthPrompt struct {
	VerificationURL string
	UserCode        string
}

type CopilotAuthComplete struct {
	ModelCount int
	Err        error
}

func (s *ProviderService) AuthenticateCopilot(ctx context.Context, emit func(CopilotAuthEvent), onAuthURL func(string)) {
	provider := oauth.NewGitHubCopilotOAuth("")
	if emit != nil {
		emit(CopilotAuthEvent{Progress: "Initiating device authorization..."})
	}

	creds, err := provider.Login(llm.OAuthLoginCallbacks{
		OnAuth: func(url string, instructions string) {
			code := strings.TrimSpace(strings.TrimPrefix(instructions, "Enter code:"))
			if emit != nil {
				emit(CopilotAuthEvent{Prompt: &CopilotAuthPrompt{VerificationURL: url, UserCode: code}})
			}
			if onAuthURL != nil {
				onAuthURL(url)
			}
		},
		OnProgress: func(message string) {
			if emit != nil {
				emit(CopilotAuthEvent{Progress: message})
			}
		},
		OnPrompt: func(message string, placeholder string, allowEmpty bool) (string, error) {
			return "", nil
		},
		Signal: ctx,
	})
	if err != nil {
		if emit != nil {
			emit(CopilotAuthEvent{Complete: &CopilotAuthComplete{Err: err}})
		}
		return
	}

	if s.authStorage != nil {
		if err := s.authStorage.SetOAuthCredentials("github-copilot", creds); err != nil {
			if emit != nil {
				emit(CopilotAuthEvent{Complete: &CopilotAuthComplete{Err: fmt.Errorf("authenticated but failed to store credentials: %w", err)}})
			}
			return
		}
	}

	if s.chatController != nil {
		s.chatController.InvalidateProviderCache()
	}
	if emit != nil {
		emit(CopilotAuthEvent{Complete: &CopilotAuthComplete{ModelCount: len(llm.GitHubCopilotDefaultModels())}})
	}
}
