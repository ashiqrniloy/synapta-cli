package tools

import (
	"context"
	"strings"
	"testing"
)

func TestWriteOverwriteMode(t *testing.T) {
	dir := t.TempDir()
	tool := NewWriteTool(dir)

	res, err := tool.Execute(context.Background(), WriteInput{
		Path:    "a.txt",
		Content: "hello\nworld\n",
		Mode:    WriteModeOverwrite,
	})
	if err != nil {
		t.Fatalf("overwrite failed: %v", err)
	}
	if len(res.Content) == 0 || !strings.Contains(res.Content[0].Text, "mode=overwrite") {
		t.Fatalf("expected overwrite mode in output, got: %#v", res.Content)
	}
}

func TestWriteReplaceMode(t *testing.T) {
	dir := t.TempDir()
	tool := NewWriteTool(dir)

	_, err := tool.Execute(context.Background(), WriteInput{Path: "b.txt", Content: "alpha beta beta", Mode: WriteModeOverwrite})
	if err != nil {
		t.Fatalf("setup write failed: %v", err)
	}

	expected := 2
	_, err = tool.Execute(context.Background(), WriteInput{
		Path:            "b.txt",
		Mode:            WriteModeReplace,
		Find:            "beta",
		Replace:         "BETA",
		ExpectedMatches: &expected,
	})
	if err != nil {
		t.Fatalf("replace failed: %v", err)
	}

	out, err := NewReadTool(dir).Execute(context.Background(), ReadInput{Path: "b.txt"})
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	text := out.Content[0].Text
	if !strings.Contains(text, "alpha BETA BETA") {
		t.Fatalf("unexpected content after replace: %s", text)
	}
}

func TestWriteLineEditMode(t *testing.T) {
	dir := t.TempDir()
	tool := NewWriteTool(dir)

	_, err := tool.Execute(context.Background(), WriteInput{
		Path:    "c.txt",
		Content: "1\n2\n3\n4\n",
		Mode:    WriteModeOverwrite,
	})
	if err != nil {
		t.Fatalf("setup write failed: %v", err)
	}

	s, e := 2, 3
	_, err = tool.Execute(context.Background(), WriteInput{
		Path:      "c.txt",
		Mode:      WriteModeLineEdit,
		StartLine: &s,
		EndLine:   &e,
		Content:   "X\nY",
	})
	if err != nil {
		t.Fatalf("line edit failed: %v", err)
	}

	out, err := NewReadTool(dir).Execute(context.Background(), ReadInput{Path: "c.txt"})
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	text := out.Content[0].Text
	if !strings.Contains(text, "1\nX\nY\n4\n") {
		t.Fatalf("unexpected content after line edit: %s", text)
	}
}

func TestWritePatchMode(t *testing.T) {
	dir := t.TempDir()
	tool := NewWriteTool(dir)

	_, err := tool.Execute(context.Background(), WriteInput{Path: "d.txt", Content: "a\nb\nc\n", Mode: WriteModeOverwrite})
	if err != nil {
		t.Fatalf("setup write failed: %v", err)
	}

	patch := strings.Join([]string{
		"--- a/d.txt",
		"+++ b/d.txt",
		"@@ -1,3 +1,3 @@",
		" a",
		"-b",
		"+B",
		" c",
	}, "\n")
	_, err = tool.Execute(context.Background(), WriteInput{Path: "d.txt", Mode: WriteModePatch, UnifiedDiff: patch})
	if err != nil {
		t.Fatalf("patch mode failed: %v", err)
	}

	out, err := NewReadTool(dir).Execute(context.Background(), ReadInput{Path: "d.txt"})
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	if !strings.Contains(out.Content[0].Text, "a\nB\nc\n") {
		t.Fatalf("unexpected content after patch: %s", out.Content[0].Text)
	}
}

func TestWriteDryRunDoesNotMutate(t *testing.T) {
	dir := t.TempDir()
	tool := NewWriteTool(dir)

	_, err := tool.Execute(context.Background(), WriteInput{Path: "e.txt", Content: "old", Mode: WriteModeOverwrite})
	if err != nil {
		t.Fatalf("setup write failed: %v", err)
	}
	dry := true
	_, err = tool.Execute(context.Background(), WriteInput{Path: "e.txt", Content: "new", Mode: WriteModeOverwrite, DryRun: &dry})
	if err != nil {
		t.Fatalf("dry run failed: %v", err)
	}

	out, err := NewReadTool(dir).Execute(context.Background(), ReadInput{Path: "e.txt"})
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	if !strings.Contains(out.Content[0].Text, "old") {
		t.Fatalf("dry run should not mutate file, got: %s", out.Content[0].Text)
	}
}


func TestWriteReplaceRegexMode(t *testing.T) {
	dir := t.TempDir()
	tool := NewWriteTool(dir)

	_, err := tool.Execute(context.Background(), WriteInput{Path: "rx.txt", Content: "a1 a2 a3", Mode: WriteModeOverwrite})
	if err != nil {
		t.Fatalf("setup write failed: %v", err)
	}
	max := 2
	_, err = tool.Execute(context.Background(), WriteInput{
		Path:            "rx.txt",
		Mode:            WriteModeReplaceRegex,
		Find:            `a(\d)`,
		Replace:         `A$1`,
		MaxReplacements: &max,
	})
	if err != nil {
		t.Fatalf("replace_regex failed: %v", err)
	}
	out, err := NewReadTool(dir).Execute(context.Background(), ReadInput{Path: "rx.txt"})
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	if !strings.Contains(out.Content[0].Text, "A1 A2 a3") {
		t.Fatalf("unexpected content after replace_regex: %s", out.Content[0].Text)
	}
}
