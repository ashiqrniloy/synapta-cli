package llm

import (
	"net/http"
	"strings"
)

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
		case RoleTool:
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
		case RoleAssistant:
			if msg.Content != "" {
				input = append(input, map[string]any{
					"type":   "message",
					"role":   string(RoleAssistant),
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
		case RoleUser:
			if msg.Content != "" {
				input = append(input, map[string]any{
					"role": string(RoleUser),
					"content": []any{
						map[string]any{"type": "input_text", "text": msg.Content},
					},
				})
			}
		case RoleSystem, RoleDeveloper:
			if msg.Content != "" {
				input = append(input, map[string]any{
					"role":    string(msg.Role),
					"content": msg.Content,
				})
			}
		default:
			if msg.Content != "" {
				input = append(input, map[string]any{
					"role":    string(msg.Role),
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

func (p *OpenAIProvider) buildRequestBody(req ChatRequest) map[string]any {
	messages := make([]map[string]any, len(req.Messages))
	for i, msg := range req.Messages {
		m := map[string]any{
			"role":    string(msg.Role),
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
		if lastMsg.Role != RoleUser {
			initiator = "agent"
		}
	}
	req.Header.Set("X-Initiator", initiator)
	req.Header.Set("Openai-Intent", "conversation-edits")

	for _, msg := range messages {
		if msg.Role == RoleUser && strings.Contains(msg.Content, "data:image/") {
			req.Header.Set("Copilot-Vision-Request", "true")
			return
		}
	}
}
