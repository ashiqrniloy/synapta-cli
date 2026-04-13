package oauth

import (
	"context"
	"fmt"
	"time"

	"github.com/ashiqrniloy/synapta-cli/internal/llm"
)

// KiloOAuth implements OAuthProvider for Kilo Gateway.
//
// All Kilo API interactions are delegated to llm.KiloGateway,
// which is the single source of truth for Kilo integration.
type KiloOAuth struct {
	gateway *llm.KiloGateway
}

// NewKiloOAuth creates a new Kilo OAuth provider.
func NewKiloOAuth() *KiloOAuth {
	return &KiloOAuth{gateway: llm.NewKiloGateway()}
}

func (k *KiloOAuth) ID() string {
	return "kilo"
}

func (k *KiloOAuth) Name() string {
	return "Kilo Gateway"
}

func (k *KiloOAuth) Login(callbacks llm.OAuthLoginCallbacks) (*llm.OAuthCredentials, error) {
	onAuth := callbacks.OnAuth
	if onAuth == nil {
		onAuth = func(string, string) {}
	}

	onProgress := callbacks.OnProgress
	if onProgress == nil {
		onProgress = func(string) {}
	}

	signal := callbacks.Signal
	if signal == nil {
		signal = context.Background()
	}

	return k.gateway.Login(context.Background(), llm.DeviceAuthCallbacks{
		OnAuth: func(url, code string) {
			onAuth(url, fmt.Sprintf("Enter code: %s", code))
		},
		OnProgress: onProgress,
		Signal:     signal,
	})
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
	if freeOnly {
		return k.gateway.FetchFreeModels()
	}
	return k.gateway.FetchModels(token)
}

// FetchBalance fetches the user's credit balance.
func (k *KiloOAuth) FetchBalance(token string) (float64, error) {
	return k.gateway.FetchBalance(token)
}
