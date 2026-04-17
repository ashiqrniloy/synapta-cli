package llm

import (
	"fmt"
	"strings"
)

// MessageRole identifies the role of a chat message.
type MessageRole string

const (
	RoleSystem    MessageRole = "system"
	RoleDeveloper MessageRole = "developer"
	RoleUser      MessageRole = "user"
	RoleAssistant MessageRole = "assistant"
	RoleTool      MessageRole = "tool"
)

func (r MessageRole) IsValid() bool {
	switch r {
	case RoleSystem, RoleDeveloper, RoleUser, RoleAssistant, RoleTool:
		return true
	default:
		return false
	}
}

func ParseMessageRole(v string) (MessageRole, error) {
	role := MessageRole(strings.TrimSpace(v))
	if !role.IsValid() {
		return "", fmt.Errorf("invalid message role: %q", v)
	}
	return role, nil
}

// Message represents a chat message.
type Message struct {
	Role       MessageRole `json:"role"`
	Content    string      `json:"content"`
	ToolCallID string      `json:"tool_call_id,omitempty"`
	Name       string      `json:"name,omitempty"`
	ToolCalls  []ToolCall  `json:"tool_calls,omitempty"`
}

func (m Message) Validate() error {
	if !m.Role.IsValid() {
		return fmt.Errorf("invalid message role: %q", m.Role)
	}
	return nil
}

// ChatRequest represents a request to the LLM.
type ChatRequest struct {
	Model      string           `json:"model"`
	Messages   []Message        `json:"messages"`
	Stream     bool             `json:"stream"`
	Tools      []ToolDefinition `json:"tools,omitempty"`
	ToolChoice any              `json:"tool_choice,omitempty"`
}

func (r ChatRequest) Validate() error {
	if strings.TrimSpace(r.Model) == "" {
		return fmt.Errorf("model is required")
	}
	for i, msg := range r.Messages {
		if err := msg.Validate(); err != nil {
			return fmt.Errorf("messages[%d]: %w", i, err)
		}
	}
	return nil
}

// ChatResponse represents a response from the LLM.
type ChatResponse struct {
	ID      string   `json:"id,omitempty"`
	Choices []Choice `json:"choices"`
	Usage   *Usage   `json:"usage,omitempty"`
}

// Choice represents a single completion choice.
type Choice struct {
	Index        int     `json:"index"`
	Message      Message `json:"message"`
	FinishReason string  `json:"finish_reason"`
}

// Usage represents token usage statistics.
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// StreamChunk represents a chunk in a streaming response.
type StreamChunk struct {
	ID      string         `json:"id,omitempty"`
	Choices []StreamChoice `json:"choices"`
	Usage   *Usage         `json:"usage,omitempty"`
}

// StreamChoice represents a choice in a streaming response.
type StreamChoice struct {
	Index        int     `json:"index"`
	Delta        Message `json:"delta"`
	Message      Message `json:"message,omitempty"`
	Text         string  `json:"text,omitempty"`
	FinishReason string  `json:"finish_reason,omitempty"`
}

// StreamCallback is called for each chunk in a streaming response.
type StreamCallback func(chunk StreamChunk) error
