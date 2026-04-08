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
	"time"
)

// ─── HTTP Client Constants ────────────────────────────────────────────

const (
	// Default timeout for API requests - generous for streaming responses
	defaultHTTPTimeout = 300 * time.Second
	// Timeout for reading response headers
	headerTimeout = 30 * time.Second
	// Buffer size for reading response bodies
	bodyBufferSize = 32 * 1024 // 32KB
)

// defaultHTTPClient is a shared HTTP client with appropriate timeouts
// for LLM API requests. It uses a Transport with connection pooling
// for better performance.
var defaultHTTPClient *http.Client
var streamingHTTPClient *http.Client

func init() {
	// Initialize the default HTTP client with timeouts
	transport := &http.Transport{
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   10,
		IdleConnTimeout:       90 * time.Second,
		ResponseHeaderTimeout: headerTimeout,
	}

	defaultHTTPClient = &http.Client{
		Transport: transport,
		Timeout:   defaultHTTPTimeout,
	}

	// Streaming responses can remain open for long periods while tokens
	// are emitted, so avoid client-level timeouts for SSE streams.
	streamingHTTPClient = &http.Client{
		Transport: transport,
	}
}

// HTTPClient returns the default HTTP client for LLM requests.
// It is configured with reasonable timeouts for API interactions.
func HTTPClient() *http.Client {
	return defaultHTTPClient
}

// HTTPStreamClient returns an HTTP client intended for SSE/streaming calls.
func HTTPStreamClient() *http.Client {
	return streamingHTTPClient
}

// ─── OpenAIProvider ──────────────────────────────────────────────────

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

func (p *OpenAIProvider) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	api := p.apiForModel(req.Model)
	if api == APIOpenAIResponses {
		return p.chatResponses(ctx, req)
	}

	resp, body, err := p.chatCompletions(ctx, req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		if isUnsupportedChatEndpoint(body) {
			return p.chatResponses(ctx, req)
		}
		return nil, fmt.Errorf("API error (HTTP %d): %s", resp.StatusCode, string(body))
	}

	var chatResp ChatResponse
	if err := json.Unmarshal(body, &chatResp); err != nil {
		return nil, fmt.Errorf("parsing response: %w", err)
	}

	return &chatResp, nil
}

func (p *OpenAIProvider) ChatStream(ctx context.Context, req ChatRequest, callback StreamCallback) error {
	api := p.apiForModel(req.Model)
	if api == APIOpenAIResponses {
		return p.chatStreamResponses(ctx, req, callback)
	}

	resp, body, err := p.chatCompletions(ctx, req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		if isUnsupportedChatEndpoint(body) {
			return p.chatStreamResponses(ctx, req, callback)
		}
		return fmt.Errorf("API error (HTTP %d): %s", resp.StatusCode, string(body))
	}

	return p.readStream(resp.Body, callback)
}

func (p *OpenAIProvider) chatCompletions(ctx context.Context, req ChatRequest) (*http.Response, []byte, error) {
	url := fmt.Sprintf("%s/chat/completions", strings.TrimRight(p.baseURL, "/"))

	reqBody := p.buildRequestBody(req)
	if req.Stream {
		reqBody["stream"] = true
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, nil, fmt.Errorf("marshaling request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, nil, fmt.Errorf("creating request: %w", err)
	}

	p.setHeaders(httpReq)
	p.setDynamicHeaders(httpReq, req.Messages)
	httpReq.Header.Set("Content-Type", "application/json")
	if req.Stream {
		httpReq.Header.Set("Accept", "text/event-stream")
	}

	httpClient := HTTPClient()
	if req.Stream {
		httpClient = HTTPStreamClient()
	}

	resp, err := httpClient.Do(httpReq)
	if err != nil {
		return nil, nil, fmt.Errorf("sending request: %w", err)
	}

	if req.Stream && resp.StatusCode == http.StatusOK {
		return resp, nil, nil
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		resp.Body.Close()
		return nil, nil, fmt.Errorf("reading response: %w", err)
	}
	if !req.Stream {
		resp.Body.Close()
	}

	return resp, body, nil
}

func (p *OpenAIProvider) chatResponses(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	url := fmt.Sprintf("%s/responses", strings.TrimRight(p.baseURL, "/"))
	reqBody := p.buildResponsesRequestBody(req, false)

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	p.setHeaders(httpReq)
	p.setDynamicHeaders(httpReq, req.Messages)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := HTTPClient().Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("sending request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		errMsg := fmt.Sprintf("API error (HTTP %d): %s", resp.StatusCode, string(body))
		if strings.Contains(strings.ToLower(string(body)), "no tool call found for function call output") {
			errMsg += " " + responsesCallDebug(reqBody)
		}
		return nil, fmt.Errorf("%s", errMsg)
	}

	return parseResponsesChat(body)
}

func (p *OpenAIProvider) chatStreamResponses(ctx context.Context, req ChatRequest, callback StreamCallback) error {
	url := fmt.Sprintf("%s/responses", strings.TrimRight(p.baseURL, "/"))
	reqBody := p.buildResponsesRequestBody(req, true)

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("marshaling request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}

	p.setHeaders(httpReq)
	p.setDynamicHeaders(httpReq, req.Messages)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := HTTPStreamClient().Do(httpReq)
	if err != nil {
		return fmt.Errorf("sending request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		errMsg := fmt.Sprintf("API error (HTTP %d): %s", resp.StatusCode, string(body))
		if strings.Contains(strings.ToLower(string(body)), "no tool call found for function call output") {
			errMsg += " " + responsesCallDebug(reqBody)
		}
		return fmt.Errorf("%s", errMsg)
	}

	return p.readResponsesStream(resp.Body, callback)
}

func (p *OpenAIProvider) apiForModel(modelID string) API {
	for _, m := range p.models {
		if m.ID == modelID {
			if m.API != "" {
				return m.API
			}
			break
		}
	}
	if strings.Contains(strings.ToLower(modelID), "gpt-5") {
		return APIOpenAIResponses
	}
	return APIOpenAICompletions
}

func isUnsupportedChatEndpoint(body []byte) bool {
	s := strings.ToLower(string(body))
	return strings.Contains(s, "unsupported_api_for_model") || strings.Contains(s, "/chat/completions endpoint")
}

func (p *OpenAIProvider) buildResponsesRequestBody(req ChatRequest, stream bool) map[string]any {
	input := make([]any, 0, len(req.Messages))
	seenFunctionCalls := make(map[string]bool)
	for _, msg := range req.Messages {
		switch msg.Role {
		case "tool":
			callID, _ := splitToolCallID(msg.ToolCallID)
			if callID == "" {
				continue
			}
			if !seenFunctionCalls[callID] {
				name := strings.TrimSpace(msg.Name)
				if name == "" {
					name = "unknown_tool"
				}
				input = append(input, map[string]any{
					"type":      "function_call",
					"call_id":   callID,
					"name":      name,
					"arguments": "{}",
				})
				seenFunctionCalls[callID] = true
			}
			output := strings.TrimSpace(msg.Content)
			if output == "" {
				output = "{}"
			}
			input = append(input, map[string]any{
				"type":    "function_call_output",
				"call_id": callID,
				"output":  output,
			})
		case "assistant":
			if msg.Content != "" {
				input = append(input, map[string]any{
					"type":   "message",
					"role":   "assistant",
					"status": "completed",
					"content": []any{
						map[string]any{"type": "output_text", "text": msg.Content},
					},
				})
			}
			for _, tc := range msg.ToolCalls {
				callID, _ := splitToolCallID(tc.ID)
				if callID == "" {
					continue
				}
				arguments := strings.TrimSpace(tc.Function.Arguments)
				if arguments == "" {
					arguments = "{}"
				}
				item := map[string]any{
					"type":      "function_call",
					"call_id":   callID,
					"name":      tc.Function.Name,
					"arguments": arguments,
				}
				input = append(input, item)
				seenFunctionCalls[callID] = true
			}
		case "user":
			if msg.Content != "" {
				input = append(input, map[string]any{
					"role": "user",
					"content": []any{
						map[string]any{"type": "input_text", "text": msg.Content},
					},
				})
			}
		case "system", "developer":
			if msg.Content != "" {
				input = append(input, map[string]any{
					"role":    msg.Role,
					"content": msg.Content,
				})
			}
		default:
			if msg.Content != "" {
				input = append(input, map[string]any{
					"role":    msg.Role,
					"content": msg.Content,
				})
			}
		}
	}

	body := map[string]any{
		"model":  req.Model,
		"input":  input,
		"stream": stream,
	}

	if len(req.Tools) > 0 {
		tools := make([]any, 0, len(req.Tools))
		for _, t := range req.Tools {
			tools = append(tools, map[string]any{
				"type":        "function",
				"name":        t.Function.Name,
				"description": t.Function.Description,
				"parameters":  t.Function.Parameters,
			})
		}
		body["tools"] = tools
		if req.ToolChoice != nil {
			body["tool_choice"] = req.ToolChoice
		} else {
			body["tool_choice"] = "auto"
		}
	}

	body["max_output_tokens"] = 16384
	return body
}

func splitToolCallID(id string) (callID, itemID string) {
	trimmed := strings.TrimSpace(id)
	if trimmed == "" {
		return "", ""
	}
	parts := strings.SplitN(trimmed, "|", 2)
	if len(parts) == 2 {
		return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
	}
	return trimmed, ""
}

func normalizeResponsesItemID(id string) string {
	id = strings.TrimSpace(strings.ToLower(id))
	if id == "" {
		return ""
	}
	id = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			return r
		}
		return '_'
	}, id)
	if !strings.HasPrefix(id, "fc_") {
		id = "fc_" + id
	}
	if len(id) > 64 {
		id = id[:64]
	}
	id = strings.TrimRight(id, "_-")
	if id == "" || id == "fc" || id == "fc_" {
		return "fc_tool_call"
	}
	return id
}

func parseResponsesChat(body []byte) (*ChatResponse, error) {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("parsing response: %w", err)
	}

	message := Message{Role: "assistant"}
	if outText, ok := payload["output_text"].(string); ok && outText != "" {
		message.Content += outText
	}
	if output, ok := payload["output"].([]any); ok {
		for _, raw := range output {
			item, _ := raw.(map[string]any)
			itemType, _ := item["type"].(string)
			switch itemType {
			case "message":
				if content, ok := item["content"].([]any); ok {
					for _, c := range content {
						part, _ := c.(map[string]any)
						partType, _ := part["type"].(string)
						if partType == "output_text" || partType == "text" {
							if text, _ := part["text"].(string); text != "" {
								message.Content += text
							}
						}
					}
				}
			case "function_call":
				callID, _ := item["call_id"].(string)
				itemID, _ := item["id"].(string)
				name, _ := item["name"].(string)
				arguments, _ := item["arguments"].(string)
				id := callID
				if itemID != "" {
					id = callID + "|" + itemID
				}
				message.ToolCalls = append(message.ToolCalls, ToolCall{
					ID:   id,
					Type: "function",
					Function: ToolFunctionCall{
						Name:      name,
						Arguments: arguments,
					},
				})
			}
		}
	}

	return &ChatResponse{Choices: []Choice{{Index: 0, Message: message, FinishReason: "stop"}}}, nil
}

func responsesCallDebug(reqBody map[string]any) string {
	input, _ := reqBody["input"].([]any)
	calls := make([]string, 0)
	outs := make([]string, 0)
	for _, raw := range input {
		item, _ := raw.(map[string]any)
		t, _ := item["type"].(string)
		switch t {
		case "function_call":
			id, _ := item["call_id"].(string)
			if id != "" {
				calls = append(calls, id)
			}
		case "function_call_output":
			id, _ := item["call_id"].(string)
			if id != "" {
				outs = append(outs, id)
			}
		}
	}
	return fmt.Sprintf("[responses-call-debug calls=%v outputs=%v]", calls, outs)
}

func (p *OpenAIProvider) readResponsesStream(reader io.Reader, callback StreamCallback) error {
	scanner := bufio.NewScanner(reader)
	toolCallIndex := 0
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" || data == "[DONE]" {
			continue
		}

		var event map[string]any
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			continue
		}
		eventType, _ := event["type"].(string)
		switch eventType {
		case "response.output_text.delta":
			delta, _ := event["delta"].(string)
			if delta != "" {
				if err := callback(StreamChunk{Choices: []StreamChoice{{Index: 0, Delta: Message{Content: delta}}}}); err != nil {
					return err
				}
			}
		case "response.output_item.done":
			item, _ := event["item"].(map[string]any)
			itemType, _ := item["type"].(string)
			if itemType != "function_call" {
				continue
			}
			callID, _ := item["call_id"].(string)
			if callID == "" {
				continue
			}
			itemID, _ := item["id"].(string)
			fullID := callID
			if itemID != "" {
				fullID = callID + "|" + itemID
			}
			name, _ := item["name"].(string)
			arguments, _ := item["arguments"].(string)
			tc := ToolCall{
				Index: toolCallIndex,
				ID:    fullID,
				Type:  "function",
				Function: ToolFunctionCall{
					Name:      name,
					Arguments: arguments,
				},
			}
			toolCallIndex++
			if err := callback(StreamChunk{Choices: []StreamChoice{{Index: 0, Delta: Message{ToolCalls: []ToolCall{tc}}}}}); err != nil {
				return err
			}
		}
	}
	return scanner.Err()
}

func (p *OpenAIProvider) buildRequestBody(req ChatRequest) map[string]any {
	messages := make([]map[string]any, len(req.Messages))
	for i, msg := range req.Messages {
		m := map[string]any{
			"role":    msg.Role,
			"content": msg.Content,
		}
		if msg.ToolCallID != "" {
			m["tool_call_id"] = msg.ToolCallID
		}
		if msg.Name != "" {
			m["name"] = msg.Name
		}
		if len(msg.ToolCalls) > 0 {
			m["tool_calls"] = msg.ToolCalls
		}
		messages[i] = m
	}

	body := map[string]any{
		"model":    req.Model,
		"messages": messages,
	}

	if req.Stream {
		body["stream"] = true
	}
	if len(req.Tools) > 0 {
		body["tools"] = req.Tools
		if req.ToolChoice != nil {
			body["tool_choice"] = req.ToolChoice
		} else {
			body["tool_choice"] = "auto"
		}
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

func (p *OpenAIProvider) setDynamicHeaders(req *http.Request, messages []Message) {
	if p.id != "github-copilot" {
		return
	}
	initiator := "user"
	if len(messages) > 0 {
		lastMsg := messages[len(messages)-1]
		if lastMsg.Role != "user" {
			initiator = "agent"
		}
	}
	req.Header.Set("X-Initiator", initiator)
	req.Header.Set("Openai-Intent", "conversation-edits")

	for _, msg := range messages {
		if msg.Role == "user" && strings.Contains(msg.Content, "data:image/") {
			req.Header.Set("Copilot-Vision-Request", "true")
			return
		}
	}
}

func (p *OpenAIProvider) readStream(reader io.Reader, callback StreamCallback) error {
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || !strings.HasPrefix(line, "data:") {
			continue
		}

		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" {
			continue
		}
		if data == "[DONE]" {
			return nil
		}

		if err := p.processSSELine(data, callback); err != nil {
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
