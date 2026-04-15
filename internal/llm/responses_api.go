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
