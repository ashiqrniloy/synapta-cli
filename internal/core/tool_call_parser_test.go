package core

import (
	"strings"
	"testing"

	"github.com/ashiqrniloy/synapta-cli/internal/llm"
)

func TestParseToolCall_MalformedArguments(t *testing.T) {
	tests := []struct {
		name      string
		toolName  string
		arguments string
		errSubstr string
	}{
		{name: "read invalid json", toolName: "read", arguments: "{", errSubstr: "invalid read arguments"},
		{name: "write invalid json", toolName: "write", arguments: "not-json", errSubstr: "invalid write arguments"},
		{name: "bash invalid json", toolName: "bash", arguments: "[1,2]", errSubstr: "invalid bash arguments"},
		{name: "unknown tool", toolName: "wat", arguments: "{}", errSubstr: "unknown tool"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseToolCall(llm.ToolCall{Function: llm.ToolFunctionCall{Name: tt.toolName, Arguments: tt.arguments}})
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.errSubstr) {
				t.Fatalf("expected error containing %q, got %v", tt.errSubstr, err)
			}
		})
	}
}
