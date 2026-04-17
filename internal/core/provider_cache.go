package core

// provider_cache.go implements the per-ChatService provider cache.
//
// All provider-specific logic (token resolution, refresh, construction) has
// been moved into ProviderDescriptor implementations under
// internal/llm/providers/.  This file contains only the generic caching
// plumbing that works for any registered provider.

import (
	"fmt"

	"github.com/ashiqrniloy/synapta-cli/internal/llm"
)

// providerFor returns a ready-to-use Provider for the given provider ID.
//
// Lookup order:
//  1. Return the cached instance if the resolved token hasn't changed.
//  2. Ask the descriptor's ResolveToken hook for the current bearer token.
//  3. Construct a fresh Provider via the descriptor's Build hook.
//  4. Cache the result keyed by provider ID + token.
//
// An error is returned when:
//   - the provider ID is not registered, or
//   - ResolveToken returns an error, or
//   - the descriptor requires a token and none could be resolved, or
//   - Build returns an error.
func (s *ChatService) providerFor(providerID string) (llm.Provider, error) {
	desc, err := s.providerReg.Get(providerID)
	if err != nil {
		return nil, err
	}

	// Resolve the current access token via the descriptor's hook.
	token, err := desc.ResolveToken(s.auth)
	if err != nil {
		return nil, fmt.Errorf("provider %q: resolving token: %w", providerID, err)
	}

	// When the token is empty the provider has no credentials — treat it as
	// unavailable rather than an error so that AvailableModels can silently
	// skip providers that haven't been authenticated yet.
	if token == "" {
		s.invalidateProvider(providerID)
		return nil, fmt.Errorf("no credentials for provider %q", providerID)
	}

	// Return the cached instance when the token is unchanged.
	s.mu.RLock()
	cached, ok := s.providers[providerID]
	s.mu.RUnlock()
	if ok && cached.provider != nil && cached.token == token {
		return cached.provider, nil
	}

	// Build a new provider instance.
	provider, err := desc.Build(token)
	if err != nil {
		return nil, fmt.Errorf("provider %q: build failed: %w", providerID, err)
	}

	s.mu.Lock()
	s.providers[providerID] = cachedProvider{provider: provider, token: token}
	s.mu.Unlock()

	return provider, nil
}

// invalidateProvider evicts the cached provider instance for providerID.
func (s *ChatService) invalidateProvider(providerID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.providers, providerID)
}
