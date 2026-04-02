package llm

// DefaultModels returns the built-in model definitions for all providers.
// These are used as fallbacks when providers don't supply their own model list.
func DefaultModels() []*Model {
	return append(
		GitHubCopilotDefaultModels(),
		KiloDefaultModels()...,
	)
}

// GitHubCopilotDefaultModels returns the default models available via GitHub Copilot.
func GitHubCopilotDefaultModels() []*Model {
	return []*Model{
		{
			ID:            "gpt-4o",
			Name:          "GPT-4o",
			Provider:      "github-copilot",
			API:           APIOpenAICompletions,
			Reasoning:     false,
			Input:         []InputModality{InputText, InputImage},
			ContextWindow: 128000,
			MaxTokens:     16384,
		},
		{
			ID:            "gpt-4.1",
			Name:          "GPT-4.1",
			Provider:      "github-copilot",
			API:           APIOpenAICompletions,
			Reasoning:     false,
			Input:         []InputModality{InputText, InputImage},
			ContextWindow: 1047576,
			MaxTokens:     32768,
		},
		{
			ID:            "o3-mini",
			Name:          "o3-mini",
			Provider:      "github-copilot",
			API:           APIOpenAICompletions,
			Reasoning:     true,
			Input:         []InputModality{InputText},
			ContextWindow: 200000,
			MaxTokens:     100000,
			Compat: &CompatConfig{
				MaxTokensField: strPtr("max_completion_tokens"),
			},
		},
		{
			ID:            "claude-sonnet-4-20250514",
			Name:          "Claude Sonnet 4",
			Provider:      "github-copilot",
			API:           APIOpenAICompletions,
			Reasoning:     false,
			Input:         []InputModality{InputText, InputImage},
			ContextWindow: 200000,
			MaxTokens:     16384,
		},
		{
			ID:            "claude-sonnet-4-20250514-thinking",
			Name:          "Claude Sonnet 4 (Thinking)",
			Provider:      "github-copilot",
			API:           APIOpenAICompletions,
			Reasoning:     true,
			Input:         []InputModality{InputText, InputImage},
			ContextWindow: 200000,
			MaxTokens:     16384,
		},
		{
			ID:            "gemini-2.5-pro",
			Name:          "Gemini 2.5 Pro",
			Provider:      "github-copilot",
			API:           APIOpenAICompletions,
			Reasoning:     true,
			Input:         []InputModality{InputText, InputImage},
			ContextWindow: 1048576,
			MaxTokens:     65536,
		},
	}
}

// KiloDefaultModels returns a curated subset of models available via Kilo Gateway.
// Kilo supports 300+ models via OpenRouter, but we list the most popular ones here.
func KiloDefaultModels() []*Model {
	return []*Model{
		{
			ID:            "anthropic/claude-sonnet-4",
			Name:          "Claude Sonnet 4",
			Provider:      "kilo",
			API:           APIOpenAICompletions,
			Reasoning:     false,
			Input:         []InputModality{InputText, InputImage},
			ContextWindow: 200000,
			MaxTokens:     16384,
		},
		{
			ID:            "anthropic/claude-opus-4",
			Name:          "Claude Opus 4",
			Provider:      "kilo",
			API:           APIOpenAICompletions,
			Reasoning:     false,
			Input:         []InputModality{InputText, InputImage},
			ContextWindow: 200000,
			MaxTokens:     32768,
		},
		{
			ID:            "openai/gpt-4o",
			Name:          "GPT-4o",
			Provider:      "kilo",
			API:           APIOpenAICompletions,
			Reasoning:     false,
			Input:         []InputModality{InputText, InputImage},
			ContextWindow: 128000,
			MaxTokens:     16384,
		},
		{
			ID:            "openai/o3-mini",
			Name:          "o3-mini",
			Provider:      "kilo",
			API:           APIOpenAICompletions,
			Reasoning:     true,
			Input:         []InputModality{InputText},
			ContextWindow: 200000,
			MaxTokens:     100000,
		},
		{
			ID:            "google/gemini-2.5-pro-preview",
			Name:          "Gemini 2.5 Pro",
			Provider:      "kilo",
			API:           APIOpenAICompletions,
			Reasoning:     true,
			Input:         []InputModality{InputText, InputImage},
			ContextWindow: 1048576,
			MaxTokens:     65536,
		},
		{
			ID:            "deepseek/deepseek-r1",
			Name:          "DeepSeek R1",
			Provider:      "kilo",
			API:           APIOpenAICompletions,
			Reasoning:     true,
			Input:         []InputModality{InputText},
			ContextWindow: 128000,
			MaxTokens:     65536,
		},
	}
}

func strPtr(s string) *string {
	return &s
}
