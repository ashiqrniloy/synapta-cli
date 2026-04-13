package llm

// Message represents a chat message.
type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	Name       string     `json:"name,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
}

// ChatRequest represents a request to the LLM.
type ChatRequest struct {
	Model      string           `json:"model"`
	Messages   []Message        `json:"messages"`
	Stream     bool             `json:"stream"`
	Tools      []ToolDefinition `json:"tools,omitempty"`
	ToolChoice any              `json:"tool_choice,omitempty"`
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
