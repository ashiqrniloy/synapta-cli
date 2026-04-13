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
//
// This mirrors pi-ai's bundled Copilot catalog (including Codex family models)
// so users see the full expected set in the picker.
func GitHubCopilotDefaultModels() []*Model {
	return []*Model{
		{ID: "claude-haiku-4.5", Name: "Claude Haiku 4.5", Provider: "github-copilot", API: APIOpenAICompletions, Reasoning: true, Input: []InputModality{InputText, InputImage}, ContextWindow: 144000, MaxTokens: 32000},
		{ID: "claude-opus-4.5", Name: "Claude Opus 4.5", Provider: "github-copilot", API: APIOpenAICompletions, Reasoning: true, Input: []InputModality{InputText, InputImage}, ContextWindow: 160000, MaxTokens: 32000},
		{ID: "claude-opus-4.6", Name: "Claude Opus 4.6", Provider: "github-copilot", API: APIOpenAICompletions, Reasoning: true, Input: []InputModality{InputText, InputImage}, ContextWindow: 1000000, MaxTokens: 64000},
		{ID: "claude-sonnet-4", Name: "Claude Sonnet 4", Provider: "github-copilot", API: APIOpenAICompletions, Reasoning: true, Input: []InputModality{InputText, InputImage}, ContextWindow: 216000, MaxTokens: 16000},
		{ID: "claude-sonnet-4.5", Name: "Claude Sonnet 4.5", Provider: "github-copilot", API: APIOpenAICompletions, Reasoning: true, Input: []InputModality{InputText, InputImage}, ContextWindow: 144000, MaxTokens: 32000},
		{ID: "claude-sonnet-4.6", Name: "Claude Sonnet 4.6", Provider: "github-copilot", API: APIOpenAICompletions, Reasoning: true, Input: []InputModality{InputText, InputImage}, ContextWindow: 1000000, MaxTokens: 32000},
		{ID: "gemini-2.5-pro", Name: "Gemini 2.5 Pro", Provider: "github-copilot", API: APIOpenAICompletions, Reasoning: true, Input: []InputModality{InputText, InputImage}, ContextWindow: 128000, MaxTokens: 64000},
		{ID: "gemini-3-flash-preview", Name: "Gemini 3 Flash", Provider: "github-copilot", API: APIOpenAICompletions, Reasoning: true, Input: []InputModality{InputText, InputImage}, ContextWindow: 128000, MaxTokens: 64000},
		{ID: "gemini-3-pro-preview", Name: "Gemini 3 Pro Preview", Provider: "github-copilot", API: APIOpenAICompletions, Reasoning: true, Input: []InputModality{InputText, InputImage}, ContextWindow: 128000, MaxTokens: 64000},
		{ID: "gemini-3.1-pro-preview", Name: "Gemini 3.1 Pro Preview", Provider: "github-copilot", API: APIOpenAICompletions, Reasoning: true, Input: []InputModality{InputText, InputImage}, ContextWindow: 128000, MaxTokens: 64000},
		{ID: "gpt-4.1", Name: "GPT-4.1", Provider: "github-copilot", API: APIOpenAICompletions, Reasoning: false, Input: []InputModality{InputText, InputImage}, ContextWindow: 128000, MaxTokens: 16384},
		{ID: "gpt-4o", Name: "GPT-4o", Provider: "github-copilot", API: APIOpenAICompletions, Reasoning: false, Input: []InputModality{InputText, InputImage}, ContextWindow: 128000, MaxTokens: 4096},
		{ID: "gpt-5", Name: "GPT-5", Provider: "github-copilot", API: APIOpenAIResponses, Reasoning: true, Input: []InputModality{InputText, InputImage}, ContextWindow: 128000, MaxTokens: 128000},
		{ID: "gpt-5-mini", Name: "GPT-5-mini", Provider: "github-copilot", API: APIOpenAIResponses, Reasoning: true, Input: []InputModality{InputText, InputImage}, ContextWindow: 264000, MaxTokens: 64000},
		{ID: "gpt-5.1", Name: "GPT-5.1", Provider: "github-copilot", API: APIOpenAIResponses, Reasoning: true, Input: []InputModality{InputText, InputImage}, ContextWindow: 264000, MaxTokens: 64000},
		{ID: "gpt-5.1-codex", Name: "GPT-5.1-Codex", Provider: "github-copilot", API: APIOpenAIResponses, Reasoning: true, Input: []InputModality{InputText, InputImage}, ContextWindow: 400000, MaxTokens: 128000},
		{ID: "gpt-5.1-codex-max", Name: "GPT-5.1-Codex-max", Provider: "github-copilot", API: APIOpenAIResponses, Reasoning: true, Input: []InputModality{InputText, InputImage}, ContextWindow: 400000, MaxTokens: 128000},
		{ID: "gpt-5.1-codex-mini", Name: "GPT-5.1-Codex-mini", Provider: "github-copilot", API: APIOpenAIResponses, Reasoning: true, Input: []InputModality{InputText, InputImage}, ContextWindow: 400000, MaxTokens: 128000},
		{ID: "gpt-5.2", Name: "GPT-5.2", Provider: "github-copilot", API: APIOpenAIResponses, Reasoning: true, Input: []InputModality{InputText, InputImage}, ContextWindow: 264000, MaxTokens: 64000},
		{ID: "gpt-5.2-codex", Name: "GPT-5.2-Codex", Provider: "github-copilot", API: APIOpenAIResponses, Reasoning: true, Input: []InputModality{InputText, InputImage}, ContextWindow: 400000, MaxTokens: 128000},
		{ID: "gpt-5.3-codex", Name: "GPT-5.3-Codex", Provider: "github-copilot", API: APIOpenAIResponses, Reasoning: true, Input: []InputModality{InputText, InputImage}, ContextWindow: 400000, MaxTokens: 128000},
		{ID: "gpt-5.4", Name: "GPT-5.4", Provider: "github-copilot", API: APIOpenAIResponses, Reasoning: true, Input: []InputModality{InputText, InputImage}, ContextWindow: 400000, MaxTokens: 128000},
		{ID: "gpt-5.4-mini", Name: "GPT-5.4 mini", Provider: "github-copilot", API: APIOpenAIResponses, Reasoning: true, Input: []InputModality{InputText, InputImage}, ContextWindow: 400000, MaxTokens: 128000},
		{ID: "grok-code-fast-1", Name: "Grok Code Fast 1", Provider: "github-copilot", API: APIOpenAICompletions, Reasoning: true, Input: []InputModality{InputText, InputImage}, ContextWindow: 256000, MaxTokens: 32000},
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
