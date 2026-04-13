package llm

import "context"

// Provider defines the interface for an LLM provider.
type Provider interface {
	ID() string
	Name() string
	Models() []*Model
	Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error)
	ChatStream(ctx context.Context, req ChatRequest, callback StreamCallback) error
	HasAuth() bool
}

// OAuthProvider defines the interface for a provider that supports OAuth login.
type OAuthProvider interface {
	Login(callbacks OAuthLoginCallbacks) (*OAuthCredentials, error)
	RefreshToken(credentials *OAuthCredentials) (*OAuthCredentials, error)
}
