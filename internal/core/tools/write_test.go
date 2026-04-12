package tools

import (
	"context"
	"strings"
	"testing"
)

// ── helpers ───────────────────────────────────────────────────────────────────

func mustWrite(t *testing.T, tool *WriteTool, in WriteInput) {
	t.Helper()
	if _, err := tool.Execute(context.Background(), in); err != nil {
		t.Fatalf("write failed: %v", err)
	}
}

func readFile(t *testing.T, tool *ReadTool, path string) string {
	t.Helper()
	out, err := tool.Execute(context.Background(), ReadInput{Path: path})
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	return out.Content[0].Text
}

// ── existing mode tests ───────────────────────────────────────────────────────

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
	if !strings.Contains(res.Content[0].Text, "mode=overwrite") {
		t.Fatalf("expected overwrite mode in output, got: %s", res.Content[0].Text)
	}
}

func TestWriteReplaceMode(t *testing.T) {
	dir := t.TempDir()
	tool := NewWriteTool(dir)
	mustWrite(t, tool, WriteInput{Path: "b.txt", Content: "alpha beta beta", Mode: WriteModeOverwrite})

	expected := 2
	mustWrite(t, tool, WriteInput{
		Path:            "b.txt",
		Mode:            WriteModeReplace,
		Find:            "beta",
		Replace:         "BETA",
		ExpectedMatches: &expected,
	})
	if !strings.Contains(readFile(t, NewReadTool(dir), "b.txt"), "alpha BETA BETA") {
		t.Fatal("replace did not produce expected content")
	}
}

func TestWriteLineEditMode(t *testing.T) {
	dir := t.TempDir()
	tool := NewWriteTool(dir)
	mustWrite(t, tool, WriteInput{Path: "c.txt", Content: "1\n2\n3\n4\n", Mode: WriteModeOverwrite})

	s, e := 2, 3
	mustWrite(t, tool, WriteInput{
		Path:      "c.txt",
		Mode:      WriteModeLineEdit,
		StartLine: &s,
		EndLine:   &e,
		Content:   "X\nY",
	})
	if !strings.Contains(readFile(t, NewReadTool(dir), "c.txt"), "1\nX\nY\n4\n") {
		t.Fatal("line edit did not produce expected content")
	}
}

func TestWritePatchMode(t *testing.T) {
	dir := t.TempDir()
	tool := NewWriteTool(dir)
	mustWrite(t, tool, WriteInput{Path: "d.txt", Content: "a\nb\nc\n", Mode: WriteModeOverwrite})

	patch := strings.Join([]string{
		"--- a/d.txt",
		"+++ b/d.txt",
		"@@ -1,3 +1,3 @@",
		" a",
		"-b",
		"+B",
		" c",
	}, "\n")
	mustWrite(t, tool, WriteInput{Path: "d.txt", Mode: WriteModePatch, UnifiedDiff: patch})
	if !strings.Contains(readFile(t, NewReadTool(dir), "d.txt"), "a\nB\nc\n") {
		t.Fatal("patch mode did not produce expected content")
	}
}

func TestWriteDryRunDoesNotMutate(t *testing.T) {
	dir := t.TempDir()
	tool := NewWriteTool(dir)
	mustWrite(t, tool, WriteInput{Path: "e.txt", Content: "old", Mode: WriteModeOverwrite})
	dry := true
	mustWrite(t, tool, WriteInput{Path: "e.txt", Content: "new", Mode: WriteModeOverwrite, DryRun: &dry})
	if !strings.Contains(readFile(t, NewReadTool(dir), "e.txt"), "old") {
		t.Fatal("dry run must not mutate the file")
	}
}

func TestWriteReplaceRegexMode(t *testing.T) {
	dir := t.TempDir()
	tool := NewWriteTool(dir)
	mustWrite(t, tool, WriteInput{Path: "rx.txt", Content: "a1 a2 a3", Mode: WriteModeOverwrite})
	m := 2
	mustWrite(t, tool, WriteInput{
		Path:            "rx.txt",
		Mode:            WriteModeReplaceRegex,
		Find:            `a(\d)`,
		Replace:         `A$1`,
		MaxReplacements: &m,
	})
	if !strings.Contains(readFile(t, NewReadTool(dir), "rx.txt"), "A1 A2 a3") {
		t.Fatal("replace_regex with max_replacements did not produce expected content")
	}
}

// ── structured output tests ───────────────────────────────────────────────────

func TestWriteDetailsHasInsertionsAndDeletions(t *testing.T) {
	dir := t.TempDir()
	tool := NewWriteTool(dir)
	mustWrite(t, tool, WriteInput{Path: "f.txt", Content: "line1\nline2\nline3\n", Mode: WriteModeOverwrite})

	res, err := tool.Execute(context.Background(), WriteInput{
		Path:    "f.txt",
		Content: "line1\nLINE2\nline3\nnewline\n",
		Mode:    WriteModeOverwrite,
	})
	if err != nil {
		t.Fatalf("write failed: %v", err)
	}
	d, ok := res.Details.(WriteDetails)
	if !ok {
		t.Fatalf("details is not WriteDetails, got %T", res.Details)
	}
	if d.Insertions == 0 {
		t.Error("expected insertions > 0")
	}
	if d.Deletions == 0 {
		t.Error("expected deletions > 0")
	}
	if d.SHA256After == "" {
		t.Error("expected SHA256After to be set")
	}
	if d.LineCountBefore == 0 || d.LineCountAfter == 0 {
		t.Error("expected line counts to be set")
	}
}

func TestWritePreviewOffByDefault(t *testing.T) {
	dir := t.TempDir()
	tool := NewWriteTool(dir)
	res, err := tool.Execute(context.Background(), WriteInput{
		Path:    "g.txt",
		Content: "hello\nworld\n",
		Mode:    WriteModeOverwrite,
	})
	if err != nil {
		t.Fatalf("write failed: %v", err)
	}
	if strings.Contains(res.Content[0].Text, "--- file preview ---") {
		t.Error("preview should be off by default")
	}
}

func TestWritePreviewOnWhenRequested(t *testing.T) {
	dir := t.TempDir()
	tool := NewWriteTool(dir)
	yes := true
	res, err := tool.Execute(context.Background(), WriteInput{
		Path:           "h.txt",
		Content:        "hello\nworld\n",
		Mode:           WriteModeOverwrite,
		IncludePreview: &yes,
	})
	if err != nil {
		t.Fatalf("write failed: %v", err)
	}
	if !strings.Contains(res.Content[0].Text, "--- file preview ---") {
		t.Error("preview should appear when include_preview=true")
	}
}

func TestWriteCompactSummaryContainsStats(t *testing.T) {
	dir := t.TempDir()
	tool := NewWriteTool(dir)
	mustWrite(t, tool, WriteInput{Path: "i.txt", Content: "a\nb\nc\n", Mode: WriteModeOverwrite})
	res, err := tool.Execute(context.Background(), WriteInput{
		Path:    "i.txt",
		Content: "a\nB\nc\n",
		Mode:    WriteModeOverwrite,
	})
	if err != nil {
		t.Fatalf("write failed: %v", err)
	}
	text := res.Content[0].Text
	if !strings.Contains(text, "+1") || !strings.Contains(text, "-1") {
		t.Errorf("expected insertion/deletion counts in summary, got: %s", text)
	}
}

// ── read: line numbers ────────────────────────────────────────────────────────

func TestReadIncludeLineNumbers(t *testing.T) {
	dir := t.TempDir()
	w := NewWriteTool(dir)
	mustWrite(t, w, WriteInput{Path: "ln.txt", Content: "alpha\nbeta\ngamma\n", Mode: WriteModeOverwrite})

	yes := true
	out, err := NewReadTool(dir).Execute(context.Background(), ReadInput{
		Path:               "ln.txt",
		IncludeLineNumbers: &yes,
	})
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	text := out.Content[0].Text
	if !strings.Contains(text, "1 | alpha") {
		t.Errorf("expected line numbers in output, got: %s", text)
	}
	if !strings.Contains(text, "2 | beta") {
		t.Errorf("expected line 2 in output, got: %s", text)
	}
}

// ── read: locate mode ─────────────────────────────────────────────────────────

func TestReadLocateLiteralMatch(t *testing.T) {
	dir := t.TempDir()
	w := NewWriteTool(dir)
	mustWrite(t, w, WriteInput{Path: "src.go", Content: "package main\n\nfunc Foo() {}\n\nfunc Bar() {}\n", Mode: WriteModeOverwrite})

	out, err := NewReadTool(dir).Execute(context.Background(), ReadInput{
		Path:    "src.go",
		Pattern: "func Foo",
	})
	if err != nil {
		t.Fatalf("locate failed: %v", err)
	}
	text := out.Content[0].Text
	if !strings.Contains(text, "func Foo") {
		t.Errorf("expected match in locate output, got: %s", text)
	}
	// Should show the match line number
	if !strings.Contains(text, "3") {
		t.Errorf("expected line number 3 in locate output, got: %s", text)
	}
}

func TestReadLocateNoMatch(t *testing.T) {
	dir := t.TempDir()
	w := NewWriteTool(dir)
	mustWrite(t, w, WriteInput{Path: "nomt.txt", Content: "hello world\n", Mode: WriteModeOverwrite})

	out, err := NewReadTool(dir).Execute(context.Background(), ReadInput{
		Path:    "nomt.txt",
		Pattern: "nothere",
	})
	if err != nil {
		t.Fatalf("locate failed: %v", err)
	}
	if !strings.Contains(out.Content[0].Text, "No matches") {
		t.Errorf("expected no-match message, got: %s", out.Content[0].Text)
	}
}

func TestReadLocateRegex(t *testing.T) {
	dir := t.TempDir()
	w := NewWriteTool(dir)
	mustWrite(t, w, WriteInput{Path: "re.txt", Content: "foo1\nbar\nfoo2\n", Mode: WriteModeOverwrite})

	yes := true
	out, err := NewReadTool(dir).Execute(context.Background(), ReadInput{
		Path:           "re.txt",
		Pattern:        `foo\d`,
		PatternIsRegex: &yes,
	})
	if err != nil {
		t.Fatalf("locate regex failed: %v", err)
	}
	text := out.Content[0].Text
	if !strings.Contains(text, "foo1") || !strings.Contains(text, "foo2") {
		t.Errorf("expected both regex matches, got: %s", text)
	}
}

func TestReadLocateWithContext(t *testing.T) {
	dir := t.TempDir()
	w := NewWriteTool(dir)
	mustWrite(t, w, WriteInput{
		Path:    "ctx.txt",
		Content: "before\ntarget\nafter\n",
		Mode:    WriteModeOverwrite,
	})

	ctx := 1
	out, err := NewReadTool(dir).Execute(context.Background(), ReadInput{
		Path:         "ctx.txt",
		Pattern:      "target",
		ContextLines: &ctx,
	})
	if err != nil {
		t.Fatalf("locate with context failed: %v", err)
	}
	text := out.Content[0].Text
	if !strings.Contains(text, "before") || !strings.Contains(text, "after") {
		t.Errorf("expected context lines in output, got: %s", text)
	}
}

// ── read: details metadata ────────────────────────────────────────────────────

func TestReadDetailsContainsFileFacts(t *testing.T) {
	dir := t.TempDir()
	w := NewWriteTool(dir)
	mustWrite(t, w, WriteInput{Path: "meta.txt", Content: "line1\nline2\n", Mode: WriteModeOverwrite})

	out, err := NewReadTool(dir).Execute(context.Background(), ReadInput{Path: "meta.txt"})
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	d, ok := out.Details.(ReadDetails)
	if !ok {
		t.Fatalf("details is not ReadDetails, got %T", out.Details)
	}
	if d.SHA256 == "" {
		t.Error("expected SHA256 to be set")
	}
	if d.LineCount == 0 {
		t.Error("expected LineCount > 0")
	}
	if d.ByteCount == 0 {
		t.Error("expected ByteCount > 0")
	}
	if d.AbsPath == "" {
		t.Error("expected AbsPath to be set")
	}
}

// ── Myers diff correctness ────────────────────────────────────────────────────

func TestMyersDiffBasic(t *testing.T) {
	a := []string{"a", "b", "c"}
	b := []string{"a", "X", "c"}
	ops := myersDiff(a, b)

	dels, adds := 0, 0
	for _, op := range ops {
		switch op.Kind {
		case diffDel:
			dels++
		case diffAdd:
			adds++
		}
	}
	if dels != 1 || adds != 1 {
		t.Errorf("expected 1 del + 1 add, got %d del + %d add", dels, adds)
	}
}

func TestMyersDiffEmpty(t *testing.T) {
	ops := myersDiff(nil, nil)
	if len(ops) != 0 {
		t.Errorf("expected no ops for empty inputs, got %d", len(ops))
	}
}

func TestMyersDiffAllAdd(t *testing.T) {
	ops := myersDiff(nil, []string{"x", "y"})
	if len(ops) != 2 {
		t.Errorf("expected 2 add ops, got %d", len(ops))
	}
	for _, op := range ops {
		if op.Kind != diffAdd {
			t.Errorf("expected all adds, got %s", op.Kind)
		}
	}
}

func TestMyersDiffAllDel(t *testing.T) {
	ops := myersDiff([]string{"x", "y"}, nil)
	if len(ops) != 2 {
		t.Errorf("expected 2 del ops, got %d", len(ops))
	}
	for _, op := range ops {
		if op.Kind != diffDel {
			t.Errorf("expected all dels, got %s", op.Kind)
		}
	}
}

// ── patch mode edge cases ─────────────────────────────────────────────────────

func TestPatchModeContextMismatchError(t *testing.T) {
	dir := t.TempDir()
	tool := NewWriteTool(dir)
	mustWrite(t, tool, WriteInput{Path: "pm.txt", Content: "a\nb\nc\n", Mode: WriteModeOverwrite})

	// Context line says "X" but file has "b" — should return a clear error.
	patch := strings.Join([]string{
		"--- a/pm.txt",
		"+++ b/pm.txt",
		"@@ -1,3 +1,3 @@",
		" a",
		" X", // wrong context
		"+B",
		" c",
	}, "\n")
	_, err := tool.Execute(context.Background(), WriteInput{
		Path:        "pm.txt",
		Mode:        WriteModePatch,
		UnifiedDiff: patch,
	})
	if err == nil {
		t.Fatal("expected error for context mismatch, got nil")
	}
	if !strings.Contains(err.Error(), "mismatch") {
		t.Errorf("expected 'mismatch' in error, got: %v", err)
	}
}

func TestPatchModeEmptyDiffError(t *testing.T) {
	dir := t.TempDir()
	tool := NewWriteTool(dir)
	mustWrite(t, tool, WriteInput{Path: "empty.txt", Content: "a\n", Mode: WriteModeOverwrite})
	_, err := tool.Execute(context.Background(), WriteInput{
		Path:        "empty.txt",
		Mode:        WriteModePatch,
		UnifiedDiff: "   ",
	})
	if err == nil {
		t.Fatal("expected error for empty unified_diff, got nil")
	}
}
func TestWriteHelpfulErrorMessagesByMode(t *testing.T) {
	dir := t.TempDir()
	tool := NewWriteTool(dir)

	// replace: missing file
	_, err := tool.Execute(context.Background(), WriteInput{
		Path: "missing-replace.txt",
		Mode: WriteModeReplace,
		Find: "x",
		Content: "y",
	})
	if err == nil || !strings.Contains(err.Error(), "replace mode requires an existing file") {
		t.Fatalf("expected replace helpful error, got: %v", err)
	}

	// replace_regex: missing file
	_, err = tool.Execute(context.Background(), WriteInput{
		Path: "missing-rx.txt",
		Mode: WriteModeReplaceRegex,
		Find: "x+",
		Content: "y",
	})
	if err == nil || !strings.Contains(err.Error(), "replace_regex mode requires an existing file") {
		t.Fatalf("expected replace_regex helpful error, got: %v", err)
	}

	// line_edit: missing start/end
	mustWrite(t, tool, WriteInput{Path: "line.txt", Content: "a\nb\n", Mode: WriteModeOverwrite})
	_, err = tool.Execute(context.Background(), WriteInput{
		Path:    "line.txt",
		Mode:    WriteModeLineEdit,
		Content: "x",
	})
	if err == nil || !strings.Contains(err.Error(), "line_edit mode requires start_line and end_line") {
		t.Fatalf("expected line_edit helpful error, got: %v", err)
	}

	// patch: missing unified_diff
	_, err = tool.Execute(context.Background(), WriteInput{
		Path: "line.txt",
		Mode: WriteModePatch,
	})
	if err == nil || !strings.Contains(err.Error(), "patch mode requires `unified_diff`") {
		t.Fatalf("expected patch helpful error, got: %v", err)
	}

	// unsupported mode
	_, err = tool.Execute(context.Background(), WriteInput{
		Path: "line.txt",
		Mode: WriteMode("wat"),
	})
	if err == nil || !strings.Contains(err.Error(), "Supported modes") {
		t.Fatalf("expected unsupported-mode helpful error, got: %v", err)
	}
}

func TestPatchModeBeginPatchWrapperError(t *testing.T) {
	dir := t.TempDir()
	tool := NewWriteTool(dir)
	mustWrite(t, tool, WriteInput{Path: "bp.txt", Content: "a\nb\n", Mode: WriteModeOverwrite})

	beginPatch := strings.Join([]string{
		"*** Begin Patch",
		"*** Update File: bp.txt",
		"@@",
		"-a",
		"+A",
		"*** End Patch",
	}, "\n")

	_, err := tool.Execute(context.Background(), WriteInput{
		Path:        "bp.txt",
		Mode:        WriteModePatch,
		UnifiedDiff: beginPatch,
	})
	if err == nil {
		t.Fatal("expected error for Begin/End Patch wrapper, got nil")
	}
	if !strings.Contains(err.Error(), "Begin/End Patch wrapper format") {
		t.Fatalf("expected Begin/End Patch guidance, got: %v", err)
	}
}
