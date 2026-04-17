package llm

import (
	"fmt"
	"sync"
)

// ─── Auth strategy ────────────────────────────────────────────────────────────

// AuthStrategy describes how a provider sources its access token.
type AuthStrategy string

const (
	// AuthStrategyOAuth means the provider uses a stored OAuthCredentials entry
	// that may need to be refreshed before each use.
	AuthStrategyOAuth AuthStrategy = "oauth"

	// AuthStrategyAPIKey means the provider uses a plain API key, sourced either
	// from AuthStorage or from an environment variable.
	AuthStrategyAPIKey AuthStrategy = "api"
)

// ─── Token hooks ──────────────────────────────────────────────────────────────

// TokenResolver resolves the effective bearer token for one request.
// auth is the AuthStorage (may be nil). It returns ("", nil) when no
// credentials are present but that is not an error (caller treats provider as
// unavailable).
type TokenResolver func(auth *AuthStorage) (token string, err error)

// TokenRefresher is called when AuthStrategy == AuthStrategyOAuth and the
// stored credentials are stale. It must persist the refreshed token back into
// auth and return the new access token.
type TokenRefresher func(auth *AuthStorage, creds *OAuthCredentials) (newAccessToken string, err error)

// ─── Provider factory ─────────────────────────────────────────────────────────

// ProviderFactory constructs a live Provider from a resolved access token.
// The factory is called only when the cached instance is absent or stale.
type ProviderFactory func(token string) (Provider, error)

// ModelDiscovery fetches the model list for this provider given a token.
// Returning (nil, nil) is valid and means "use the factory's embedded list".
type ModelDiscovery func(token string) ([]*Model, error)

// ─── ProviderDescriptor ───────────────────────────────────────────────────────

// ProviderDescriptor bundles every piece of information the registry needs to
// instantiate, authenticate, and query a provider.  Adding a new provider means
// building one descriptor; no core code changes are required.
type ProviderDescriptor struct {
	// ID is the canonical lower-case identifier used throughout the app
	// (e.g. "kilo", "github-copilot").
	ID string

	// Name is the human-readable display name.
	Name string

	// Auth describes the credential strategy.
	Auth AuthStrategy

	// EnvVar is the environment variable consulted as a fallback token source.
	// Leave empty when the provider has no env-var fallback.
	EnvVar string

	// ResolveToken resolves the bearer token to use for one session.
	// The registry calls this before constructing or re-using a Provider.
	// When nil the registry falls back to the default resolution logic that
	// reads AuthStorage + EnvVar.
	ResolveToken TokenResolver

	// RefreshToken is called when the OAuth access token is expired or
	// missing.  Required when Auth == AuthStrategyOAuth.
	RefreshToken TokenRefresher

	// Build constructs a ready-to-use Provider from a resolved access token.
	Build ProviderFactory

	// DiscoverModels fetches the live model list.  Optional — when nil the
	// provider's Models() method is used directly after Build.
	DiscoverModels ModelDiscovery
}

// ─── ProviderRegistry ────────────────────────────────────────────────────────

// ProviderRegistry holds every known provider descriptor.  It is safe for
// concurrent reads after all descriptors have been registered at startup.
type ProviderRegistry struct {
	mu          sync.RWMutex
	descriptors map[string]*ProviderDescriptor
	order       []string // insertion order; used for deterministic iteration
}

// NewProviderRegistry creates an empty registry.
func NewProviderRegistry() *ProviderRegistry {
	return &ProviderRegistry{
		descriptors: make(map[string]*ProviderDescriptor),
	}
}

// Register adds a descriptor to the registry.  Registering the same ID twice
// overwrites the earlier entry (last-writer-wins, useful for tests).
func (r *ProviderRegistry) Register(d *ProviderDescriptor) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.descriptors[d.ID]; !exists {
		r.order = append(r.order, d.ID)
	}
	r.descriptors[d.ID] = d
}

// Get looks up a descriptor by provider ID.  Returns an error when the
// provider is unknown.
func (r *ProviderRegistry) Get(id string) (*ProviderDescriptor, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	d, ok := r.descriptors[id]
	if !ok {
		return nil, fmt.Errorf("unknown provider %q: register a ProviderDescriptor before use", id)
	}
	return d, nil
}

// All returns all registered descriptors in registration order.
func (r *ProviderRegistry) All() []*ProviderDescriptor {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make([]*ProviderDescriptor, 0, len(r.order))
	for _, id := range r.order {
		out = append(out, r.descriptors[id])
	}
	return out
}

// IDs returns every registered provider ID in registration order.
func (r *ProviderRegistry) IDs() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make([]string, len(r.order))
	copy(out, r.order)
	return out
}
