package core

import (
	"context"
	"errors"
	"strings"
	"testing"

	coretools "github.com/ashiqrniloy/synapta-cli/internal/core/tools"
	"github.com/ashiqrniloy/synapta-cli/internal/llm"
)

type fakeProvider struct {
	chatFn       func(ctx context.Context, req llm.ChatRequest) (*llm.ChatResponse, error)
	chatStreamFn func(ctx context.Context, req llm.ChatRequest, callback llm.StreamCallback) error
}

func (f *fakeProvider) ID() string           { return "fake" }
func (f *fakeProvider) Name() string         { return "fake" }
func (f *fakeProvider) Models() []*llm.Model { return nil }
func (f *fakeProvider) HasAuth() bool        { return true }
func (f *fakeProvider) Chat(ctx context.Context, req llm.ChatRequest) (*llm.ChatResponse, error) {
	if f.chatFn == nil {
		return nil, errors.New("chat not configured")
	}
	return f.chatFn(ctx, req)
}
func (f *fakeProvider) ChatStream(ctx context.Context, req llm.ChatRequest, callback llm.StreamCallback) error {
	if f.chatStreamFn == nil {
		return errors.New("chat stream not configured")
	}
	return f.chatStreamFn(ctx, req, callback)
}

func TestStreamAssistantTurn_AssemblesPartialChunks(t *testing.T) {
	svc := NewChatService(nil, coretools.NewToolSet(t.TempDir()))
	provider := &fakeProvider{
		chatFn: func(context.Context, llm.ChatRequest) (*llm.ChatResponse, error) {
			t.Fatal("fallback Chat must not be called when stream has output")
			return nil, nil
		},
		chatStreamFn: func(_ context.Context, _ llm.ChatRequest, callback llm.StreamCallback) error {
			chunks := []llm.StreamChunk{
				{Choices: []llm.StreamChoice{{Delta: llm.Message{Content: "hel"}}}},
				{Choices: []llm.StreamChoice{{Delta: llm.Message{Content: "lo", ToolCalls: []llm.ToolCall{{
					Index:    0,
					Function: llm.ToolFunctionCall{Name: "read", Arguments: "{\"path\":"},
				}}}}}},
				{Choices: []llm.StreamChoice{{Delta: llm.Message{ToolCalls: []llm.ToolCall{{
					Index:    0,
					ID:       "tc-1",
					Type:     "function",
					Function: llm.ToolFunctionCall{Arguments: "\"a.txt\"}"},
				}}}}}},
			}
			for _, c := range chunks {
				if err := callback(c); err != nil {
					return err
				}
			}
			return nil
		},
	}

	var deltas []string
	text, toolCalls, err := svc.streamAssistantTurn(context.Background(), provider, "m", nil, func(s string) error {
		deltas = append(deltas, s)
		return nil
	})
	if err != nil {
		t.Fatalf("streamAssistantTurn() error = %v", err)
	}
	if text != "hello" {
		t.Fatalf("expected text %q, got %q", "hello", text)
	}
	if strings.Join(deltas, "") != "hello" {
		t.Fatalf("expected deltas to form hello, got %q", strings.Join(deltas, ""))
	}
	if len(toolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(toolCalls))
	}
	if toolCalls[0].Function.Name != "read" || toolCalls[0].Function.Arguments != "{\"path\":\"a.txt\"}" {
		t.Fatalf("unexpected assembled tool call: %#v", toolCalls[0])
	}
}

func TestStreamAssistantTurn_FallbackPaths(t *testing.T) {
	tests := []struct {
		name            string
		chatStreamFn    func(context.Context, llm.ChatRequest, llm.StreamCallback) error
		chatFn          func(context.Context, llm.ChatRequest) (*llm.ChatResponse, error)
		wantText        string
		wantDelta       string
		wantToolCallID  string
		wantErrContains string
	}{
		{
			name: "fallback succeeds when stream empty",
			chatStreamFn: func(_ context.Context, _ llm.ChatRequest, callback llm.StreamCallback) error {
				return callback(llm.StreamChunk{Choices: []llm.StreamChoice{{Delta: llm.Message{}}}})
			},
			chatFn: func(_ context.Context, req llm.ChatRequest) (*llm.ChatResponse, error) {
				if req.Stream {
					return nil, errors.New("expected fallback Chat to be non-stream")
				}
				return &llm.ChatResponse{Choices: []llm.Choice{{
					Index: 0,
					Message: llm.Message{
						Content: "fallback text",
						ToolCalls: []llm.ToolCall{{
							ID:       "tc-fb",
							Type:     "function",
							Function: llm.ToolFunctionCall{Name: "read", Arguments: "{}"},
						}},
					},
				}}}, nil
			},
			wantText:       "fallback text",
			wantDelta:      "fallback text",
			wantToolCallID: "tc-fb",
		},
		{
			name: "fallback error is wrapped",
			chatStreamFn: func(_ context.Context, _ llm.ChatRequest, _ llm.StreamCallback) error {
				return nil
			},
			chatFn: func(_ context.Context, _ llm.ChatRequest) (*llm.ChatResponse, error) {
				return nil, errors.New("boom")
			},
			wantErrContains: "stream produced no output and fallback chat failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := NewChatService(nil, coretools.NewToolSet(t.TempDir()))
			provider := &fakeProvider{chatStreamFn: tt.chatStreamFn, chatFn: tt.chatFn}

			var gotDelta string
			text, toolCalls, err := svc.streamAssistantTurn(context.Background(), provider, "m", nil, func(s string) error {
				gotDelta += s
				return nil
			})

			if tt.wantErrContains != "" {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if !strings.Contains(err.Error(), tt.wantErrContains) {
					t.Fatalf("expected error containing %q, got %v", tt.wantErrContains, err)
				}
				return
			}

			if err != nil {
				t.Fatalf("streamAssistantTurn() error = %v", err)
			}
			if text != tt.wantText || gotDelta != tt.wantDelta {
				t.Fatalf("unexpected fallback text/delta: text=%q delta=%q", text, gotDelta)
			}
			if len(toolCalls) != 1 || toolCalls[0].ID != tt.wantToolCallID {
				t.Fatalf("unexpected fallback tool calls: %#v", toolCalls)
			}
		})
	}
}
