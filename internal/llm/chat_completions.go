package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

func (p *OpenAIProvider) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

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
	if err := req.Validate(); err != nil {
		return err
	}

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
