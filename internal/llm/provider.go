package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

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

func (p *OpenAIProvider) ID() string   { return p.id }
func (p *OpenAIProvider) Name() string { return p.name }
func (p *OpenAIProvider) Models() []*Model { return p.models }
func (p *OpenAIProvider) HasAuth() bool { return p.apiKey != "" }

func (p *OpenAIProvider) SetAPIKey(apiKey string) {
	p.apiKey = apiKey
}

func (p *OpenAIProvider) SetBaseURL(baseURL string) {
	p.baseURL = baseURL
}

func (p *OpenAIProvider) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	url := fmt.Sprintf("%s/chat/completions", strings.TrimRight(p.baseURL, "/"))

	reqBody := p.buildRequestBody(req)

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	p.setHeaders(httpReq)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("sending request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API error (HTTP %d): %s", resp.StatusCode, string(body))
	}

	var chatResp ChatResponse
	if err := json.Unmarshal(body, &chatResp); err != nil {
		return nil, fmt.Errorf("parsing response: %w", err)
	}

	return &chatResp, nil
}

func (p *OpenAIProvider) ChatStream(ctx context.Context, req ChatRequest, callback StreamCallback) error {
	url := fmt.Sprintf("%s/chat/completions", strings.TrimRight(p.baseURL, "/"))

	reqBody := p.buildRequestBody(req)
	reqBody["stream"] = true

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("marshaling request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}

	p.setHeaders(httpReq)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("sending request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("API error (HTTP %d): %s", resp.StatusCode, string(body))
	}

	return p.readStream(resp.Body, callback)
}

func (p *OpenAIProvider) buildRequestBody(req ChatRequest) map[string]any {
	messages := make([]map[string]any, len(req.Messages))
	for i, msg := range req.Messages {
		messages[i] = map[string]any{
			"role":    msg.Role,
			"content": msg.Content,
		}
	}

	body := map[string]any{
		"model":    req.Model,
		"messages": messages,
	}

	if p.compat != nil && p.compat.MaxTokensField != nil && *p.compat.MaxTokensField == "max_completion_tokens" {
		body["max_completion_tokens"] = 16384
	} else {
		body["max_tokens"] = 16384
	}

	return body
}

func (p *OpenAIProvider) setHeaders(req *http.Request) {
	if p.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.apiKey)
	}

	for k, v := range p.headers {
		req.Header.Set(k, v)
	}
}

func (p *OpenAIProvider) readStream(reader io.Reader, callback StreamCallback) error {
	scanner := bufio.NewScanner(reader)
	var buffer strings.Builder

	for scanner.Scan() {
		line := scanner.Text()

		if line == "" {
			if buffer.Len() > 0 {
				if err := p.processSSELine(buffer.String(), callback); err != nil {
					return err
				}
				buffer.Reset()
			}
			continue
		}

		if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")
			if data == "[DONE]" {
				return nil
			}
			buffer.WriteString(data)
		}
	}

	if buffer.Len() > 0 {
		if err := p.processSSELine(buffer.String(), callback); err != nil {
			return err
		}
	}

	return scanner.Err()
}

func (p *OpenAIProvider) processSSELine(data string, callback StreamCallback) error {
	var chunk StreamChunk
	if err := json.Unmarshal([]byte(data), &chunk); err != nil {
		return nil
	}
	return callback(chunk)
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

// NewKiloProvider creates a new Kilo Gateway provider.
func NewKiloProvider(baseURL, apiKey string, models []*Model) *KiloProvider {
	headers := map[string]string{
		"X-KILOCODE-EDITORNAME": "Synapta",
		"User-Agent":            "synapta-kilo-provider",
	}

	return &KiloProvider{
		OpenAIProvider: NewOpenAIProvider("kilo", "Kilo Gateway", baseURL, apiKey, headers, models, &CompatConfig{}),
	}
}
