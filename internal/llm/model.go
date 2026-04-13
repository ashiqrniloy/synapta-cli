package llm

// InputModality represents the type of input a model accepts.
type InputModality string

const (
	InputText  InputModality = "text"
	InputImage InputModality = "image"
)

// Cost represents the pricing for a model (per million tokens).
type Cost struct {
	Input      float64 `json:"input"`
	Output     float64 `json:"output"`
	CacheRead  float64 `json:"cacheRead"`
	CacheWrite float64 `json:"cacheWrite"`
}

// Model represents an LLM model with its capabilities and configuration.
type Model struct {
	ID            string          `json:"id"`
	Name          string          `json:"name"`
	Provider      string          `json:"provider"`
	API           API             `json:"api"`
	BaseURL       string          `json:"baseUrl"`
	Reasoning     bool            `json:"reasoning"`
	Input         []InputModality `json:"input"`
	Cost          Cost            `json:"cost"`
	ContextWindow int             `json:"contextWindow"`
	MaxTokens     int             `json:"maxTokens"`
	Compat        *CompatConfig   `json:"compat,omitempty"`
}

// CompatConfig defines OpenAI compatibility settings.
type CompatConfig struct {
	SupportsStore            *bool   `json:"supportsStore,omitempty"`
	SupportsDeveloperRole    *bool   `json:"supportsDeveloperRole,omitempty"`
	SupportsReasoningEffort  *bool   `json:"supportsReasoningEffort,omitempty"`
	SupportsUsageInStreaming *bool   `json:"supportsUsageInStreaming,omitempty"`
	MaxTokensField           *string `json:"maxTokensField,omitempty"`
}
