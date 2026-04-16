package httpclient

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestParseTraceEnabled_Table(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		{in: "1", want: true},
		{in: "true", want: true},
		{in: "TRACE", want: true},
		{in: "on", want: true},
		{in: "0", want: false},
		{in: "", want: false},
		{in: "nope", want: false},
	}
	for _, tt := range tests {
		if got := parseTraceEnabled(tt.in); got != tt.want {
			t.Fatalf("parseTraceEnabled(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestWrapErrorAddsResponseHeaderTimeoutHint(t *testing.T) {
	rec := &httpTraceRecord{
		id:       "id",
		client:   "llm",
		method:   "POST",
		target:   "https://example.invalid/chat/completions",
		started:  time.Now().Add(-2 * time.Second),
		wroteReq: time.Now().Add(-1500 * time.Millisecond),
	}
	err := rec.wrapError(errors.New("http2: timeout awaiting response headers"))
	if err == nil {
		t.Fatal("expected wrapped error")
	}
	s := err.Error()
	if !strings.Contains(s, "phase=awaiting response headers") {
		t.Fatalf("expected phase hint, got: %s", s)
	}
	if !strings.Contains(s, "hint=response_header_timeout") {
		t.Fatalf("expected response_header_timeout hint, got: %s", s)
	}
}
