package llm

import (
	"bufio"
	"encoding/json"
	"io"
	"strings"
)

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
