package llm

// ToolFunctionDefinition describes a callable tool function.
type ToolFunctionDefinition struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Parameters  any    `json:"parameters,omitempty"`
}

// ToolDefinition declares a tool for the model.
type ToolDefinition struct {
	Type     string                 `json:"type"`
	Function ToolFunctionDefinition `json:"function"`
}

// ToolFunctionCall is the model-produced tool call payload.
type ToolFunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// ToolCall represents a single function tool invocation.
type ToolCall struct {
	Index    int              `json:"index,omitempty"`
	ID       string           `json:"id"`
	Type     string           `json:"type"`
	Function ToolFunctionCall `json:"function"`
}
