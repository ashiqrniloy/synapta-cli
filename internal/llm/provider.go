package llm

import (
	"net/http"

	"github.com/ashiqrniloy/synapta-cli/internal/httpclient"
)

// HTTPClient returns the shared HTTP client for non-streaming LLM requests.
func HTTPClient() *http.Client {
	return httpclient.LLM
}

// HTTPStreamClient returns the shared HTTP client for SSE/streaming LLM calls.
// It uses a dedicated transport with a generous ResponseHeaderTimeout (120 s)
// to accommodate slow-to-respond models (Codex, o-series) without timing out
// before the first response byte arrives.
func HTTPStreamClient() *http.Client {
	return httpclient.LLMStream
}

// OpenAIProvider implements an OpenAI-compatible LLM provider.
type OpenAIProvider struct {
	id      string
	name    string
	baseURL string
	apiKey  string
	headers map[string]string
	models  []*Model
	compat  *CompatConfig
}

// NewOpenAIProvider creates a new OpenAI-compatible provider.
func NewOpenAIProvider(id, name, baseURL, apiKey string, headers map[string]string, models []*Model, compat *CompatConfig) *OpenAIProvider {
	return &OpenAIProvider{
		id:      id,
		name:    name,
		baseURL: baseURL,
		apiKey:  apiKey,
		headers: headers,
		models:  models,
		compat:  compat,
	}
}

func (p *OpenAIProvider) ID() string       { return p.id }
func (p *OpenAIProvider) Name() string     { return p.name }
func (p *OpenAIProvider) Models() []*Model { return p.models }
func (p *OpenAIProvider) HasAuth() bool    { return p.apiKey != "" }

func (p *OpenAIProvider) SetAPIKey(apiKey string) {
	p.apiKey = apiKey
}

func (p *OpenAIProvider) SetBaseURL(baseURL string) {
	p.baseURL = baseURL
}

// GitHubCopilotProvider extends OpenAIProvider with Copilot-specific headers.
type GitHubCopilotProvider struct {
	*OpenAIProvider
}

// NewGitHubCopilotProvider creates a new GitHub Copilot provider.
func NewGitHubCopilotProvider(baseURL, apiKey string, models []*Model) *GitHubCopilotProvider {
	headers := map[string]string{
		"User-Agent":             "GitHubCopilotChat/0.35.0",
		"Editor-Version":         "vscode/1.107.0",
		"Editor-Plugin-Version":  "copilot-chat/0.35.0",
		"Copilot-Integration-Id": "vscode-chat",
	}

	return &GitHubCopilotProvider{
		OpenAIProvider: NewOpenAIProvider("github-copilot", "GitHub Copilot", baseURL, apiKey, headers, models, &CompatConfig{}),
	}
}

// SetInitiatorHeader sets the X-Initiator header based on message context.
func (p *GitHubCopilotProvider) SetInitiatorHeader(req *http.Request, messages []Message) {
	initiator := "user"
	if len(messages) > 0 {
		lastMsg := messages[len(messages)-1]
		if lastMsg.Role != "user" {
			initiator = "agent"
		}
	}
	req.Header.Set("X-Initiator", initiator)
	req.Header.Set("Openai-Intent", "conversation-edits")
}

// KiloProvider extends OpenAIProvider with Kilo-specific headers.
type KiloProvider struct {
	*OpenAIProvider
}
