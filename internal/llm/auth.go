package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// OAuthCredentials stores OAuth authentication credentials.
type OAuthCredentials struct {
	Refresh   string          `json:"refresh"`
	Access    string          `json:"access"`
	Expires   int64           `json:"expires"`
	ExtraData json.RawMessage `json:"extraData,omitempty"`
}

// APICredentials stores API key credentials.
type APICredentials struct {
	APIKey string `json:"apiKey"`
}

// OAuthLoginCallbacks provides callbacks for the OAuth login flow.
type OAuthLoginCallbacks struct {
	OnAuth     func(url string, instructions string)
	OnProgress func(message string)
	OnPrompt   func(message string, placeholder string, allowEmpty bool) (string, error)
	Signal     context.Context
}

// AuthEntry represents stored credentials for a provider.
type AuthEntry struct {
	Type  string            `json:"type"`
	OAuth *OAuthCredentials `json:"oauth,omitempty"`
	API   *APICredentials   `json:"api,omitempty"`
}

// AuthStorage manages credential storage.
type AuthStorage struct {
	mu       sync.RWMutex
	data     map[string]*AuthEntry
	filePath string
}

// NewAuthStorage creates a new auth storage.
func NewAuthStorage(agentDir string) (*AuthStorage, error) {
	if err := os.MkdirAll(agentDir, 0700); err != nil {
		return nil, fmt.Errorf("creating agent dir: %w", err)
	}

	filePath := filepath.Join(agentDir, "auth.json")
	s := &AuthStorage{
		data:     make(map[string]*AuthEntry),
		filePath: filePath,
	}

	if err := s.load(); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("loading auth: %w", err)
	}

	return s, nil
}

func (s *AuthStorage) load() error {
	data, err := os.ReadFile(s.filePath)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, &s.data)
}

func (s *AuthStorage) save() error {
	data, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling auth: %w", err)
	}
	return os.WriteFile(s.filePath, data, 0600)
}

func (s *AuthStorage) Get(provider string) *AuthEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.data[provider]
}

func (s *AuthStorage) HasAuth(provider string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entry, ok := s.data[provider]
	if !ok {
		return false
	}

	switch entry.Type {
	case "oauth":
		return entry.OAuth != nil && (entry.OAuth.Access != "" || entry.OAuth.Refresh != "")
	case "api":
		return entry.API != nil && entry.API.APIKey != ""
	}
	return false
}

func (s *AuthStorage) GetAPIKey(provider string) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entry, ok := s.data[provider]
	if !ok {
		return "", fmt.Errorf("no credentials for provider: %s", provider)
	}

	switch entry.Type {
	case "oauth":
		if entry.OAuth == nil {
			return "", fmt.Errorf("no OAuth credentials for provider: %s", provider)
		}
		return entry.OAuth.Access, nil
	case "api":
		if entry.API == nil {
			return "", fmt.Errorf("no API credentials for provider: %s", provider)
		}
		return entry.API.APIKey, nil
	}
	return "", fmt.Errorf("unknown credential type: %s", entry.Type)
}

func (s *AuthStorage) SetOAuthCredentials(provider string, creds *OAuthCredentials) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.data[provider] = &AuthEntry{
		Type:  "oauth",
		OAuth: creds,
	}
	return s.save()
}

func (s *AuthStorage) SetAPIKey(provider string, apiKey string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.data[provider] = &AuthEntry{
		Type: "api",
		API:  &APICredentials{APIKey: apiKey},
	}
	return s.save()
}

func (s *AuthStorage) Remove(provider string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.data, provider)
	return s.save()
}

func (s *AuthStorage) GetOAuthCredentials(provider string) (*OAuthCredentials, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entry, ok := s.data[provider]
	if !ok {
		return nil, fmt.Errorf("no credentials for provider: %s", provider)
	}

	if entry.Type != "oauth" || entry.OAuth == nil {
		return nil, fmt.Errorf("provider %s is not using OAuth", provider)
	}

	return entry.OAuth, nil
}
