package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

func TestNormalizeResponsesItemID_LowercaseAndCharset(t *testing.T) {
	got := normalizeResponsesItemID("fc_sCe_U3AMvL_nt3BuaeX8feDLUHbo8AfVcMG4kOcWfTTJHCSH1Nufcc7Read2i")

	if got != "fc_sce_u3amvl_nt3buaex8fedluhbo8afvcmg4kocwfttjhcsh1nufcc7read2i" {
		t.Fatalf("unexpected normalized id: %q", got)
	}

	if len(got) > 64 {
		t.Fatalf("normalized id exceeds max length: %d", len(got))
	}

	validID := regexp.MustCompile(`^[a-z0-9_-]+$`)
	if !validID.MatchString(got) {
		t.Fatalf("normalized id contains invalid characters: %q", got)
	}
}

func TestNormalizeResponsesItemID_EmptyFallback(t *testing.T) {
	got := normalizeResponsesItemID("@@@")
	if got != "fc_tool_call" {
		t.Fatalf("expected fallback id, got: %q", got)
	}
}

func TestProviderFallbackFromChatCompletionsToResponses(t *testing.T) {
	tests := []struct {
		name          string
		stream        bool
		chatBody      string
		responsesBody string
		wantText      string
	}{
		{
			name:          "non-streaming chat fallback",
			stream:        false,
			chatBody:      `{"error":"unsupported_api_for_model"}`,
			responsesBody: `{"output_text":"from-responses"}`,
			wantText:      "from-responses",
		},
		{
			name:     "streaming chat fallback",
			stream:   true,
			chatBody: `{"error":"use /chat/completions endpoint is unsupported for this model"}`,
			responsesBody: strings.Join([]string{
				"data: {\"type\":\"response.output_text.delta\",\"delta\":\"hel\"}",
				"",
				"data: {\"type\":\"response.output_text.delta\",\"delta\":\"lo\"}",
				"",
				"data: [DONE]",
				"",
			}, "\n"),
			wantText: "hello",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/chat/completions":
					w.WriteHeader(http.StatusBadRequest)
					_, _ = w.Write([]byte(tt.chatBody))
				case "/responses":
					if tt.stream {
						w.Header().Set("Content-Type", "text/event-stream")
					}
					w.WriteHeader(http.StatusOK)
					_, _ = w.Write([]byte(tt.responsesBody))
				default:
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer ts.Close()

			p := NewOpenAIProvider("p", "p", ts.URL, "k", nil, []*Model{{ID: "m", API: APIOpenAICompletions}}, nil)
			if !tt.stream {
				resp, err := p.Chat(context.Background(), ChatRequest{Model: "m", Messages: []Message{{Role: "user", Content: "hi"}}})
				if err != nil {
					t.Fatalf("Chat() error = %v", err)
				}
				if resp == nil || len(resp.Choices) == 0 || resp.Choices[0].Message.Content != tt.wantText {
					t.Fatalf("unexpected response: %#v", resp)
				}
				return
			}

			var got strings.Builder
			err := p.ChatStream(context.Background(), ChatRequest{Model: "m", Stream: true, Messages: []Message{{Role: "user", Content: "hi"}}}, func(c StreamChunk) error {
				for _, ch := range c.Choices {
					got.WriteString(ch.Delta.Content)
				}
				return nil
			})
			if err != nil {
				t.Fatalf("ChatStream() error = %v", err)
			}
			if got.String() != tt.wantText {
				t.Fatalf("expected streamed fallback text %q, got %q", tt.wantText, got.String())
			}
		})
	}
}

func TestReadStream_IgnoresInvalidSSEEvents(t *testing.T) {
	p := NewOpenAIProvider("p", "p", "http://example", "k", nil, nil, nil)
	payload := strings.Join([]string{
		"event: message",
		"data: not-json",
		"",
		"data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}",
		"",
		"data: [DONE]",
		"",
	}, "\n")

	var got strings.Builder
	err := p.readStream(strings.NewReader(payload), func(c StreamChunk) error {
		for _, ch := range c.Choices {
			got.WriteString(ch.Delta.Content)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("readStream() error = %v", err)
	}
	if got.String() != "ok" {
		t.Fatalf("expected only valid chunk to be emitted, got %q", got.String())
	}
}
