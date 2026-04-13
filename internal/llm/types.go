package llm

// API identifies which OpenAI-compatible wire protocol a model uses.
type API string

const (
	APIOpenAICompletions API = "openai-completions"
	APIOpenAIResponses   API = "openai-responses"
)
