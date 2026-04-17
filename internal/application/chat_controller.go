package application

import (
	"context"
	"fmt"

	"github.com/ashiqrniloy/synapta-cli/internal/core"
	"github.com/ashiqrniloy/synapta-cli/internal/llm"
)

// ChatController orchestrates chat runtime use-cases.
type ChatController struct {
	chatService *core.ChatService
}

func NewChatController(chatService *core.ChatService) *ChatController {
	return &ChatController{chatService: chatService}
}

func (c *ChatController) SetChatService(chatService *core.ChatService) {
	c.chatService = chatService
}

func (c *ChatController) Stream(
	ctx context.Context,
	providerID string,
	modelID string,
	history []llm.Message,
	onChunk func(text string) error,
	onAssistantToolCalls func(message llm.Message) error,
	onToolEvent func(event core.ToolEvent) error,
) error {
	if c == nil || c.chatService == nil {
		return fmt.Errorf("chat service not available")
	}
	return c.chatService.Stream(ctx, providerID, modelID, history, onChunk, onAssistantToolCalls, onToolEvent)
}

func (c *ChatController) AvailableModels(ctx context.Context) ([]*llm.Model, error) {
	if c == nil || c.chatService == nil {
		return nil, nil
	}
	return c.chatService.AvailableModels(ctx)
}

func (c *ChatController) ModelContextWindow(ctx context.Context, providerID, modelID string) (int, error) {
	if c == nil || c.chatService == nil {
		return 0, fmt.Errorf("chat service not available")
	}
	return c.chatService.ModelContextWindow(ctx, providerID, modelID)
}

func (c *ChatController) SummarizeCompaction(ctx context.Context, providerID, modelID string, messages []llm.Message, previousSummary string) (string, error) {
	if c == nil || c.chatService == nil {
		return "", nil
	}
	return c.chatService.SummarizeCompaction(ctx, providerID, modelID, messages, previousSummary)
}

func (c *ChatController) InvalidateProviderCache() {
	if c == nil || c.chatService == nil {
		return
	}
	c.chatService.InvalidateProviderCache()
}
