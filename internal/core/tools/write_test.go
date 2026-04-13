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

// ── new guard: overwrite with empty content is rejected ───────────────────────

func TestWriteOverwriteEmptyContentRejected(t *testing.T) {
	dir := t.TempDir()
	tool := NewWriteTool(dir)
	mustWrite(t, tool, WriteInput{Path: "nonempty.txt", Content: "original\n", Mode: WriteModeOverwrite})

	_, err := tool.Execute(context.Background(), WriteInput{
		Path:    "nonempty.txt",
		Mode:    WriteModeOverwrite,
		Content: "",
	})
	if err == nil {
		t.Fatal("expected error when overwriting with empty content, got nil")
	}
	if !strings.Contains(err.Error(), "non-empty") {
		t.Errorf("expected 'non-empty' in error, got: %v", err)
	}
	// Original file must be untouched.
	if !strings.Contains(readFile(t, NewReadTool(dir), "nonempty.txt"), "original") {
		t.Error("file was corrupted by empty overwrite that should have been rejected")
	}
}

// TestWriteOverwriteNewFileEmptyContentRejected ensures new-file creation with
// empty content is also refused.
func TestWriteOverwriteNewFileEmptyContentRejected(t *testing.T) {
	dir := t.TempDir()
	tool := NewWriteTool(dir)
	_, err := tool.Execute(context.Background(), WriteInput{
		Path:    "new.txt",
		Mode:    WriteModeOverwrite,
		Content: "",
	})
	if err == nil {
		t.Fatal("expected error when creating a new file with empty content, got nil")
	}
}

// ── new guard: path traversal outside CWD is rejected ────────────────────────

func TestWritePathTraversalRejected(t *testing.T) {
	dir := t.TempDir()
	tool := NewWriteTool(dir)

	_, err := tool.Execute(context.Background(), WriteInput{
		Path:    "../../etc/passwd",
		Mode:    WriteModeOverwrite,
		Content: "malicious\n",
	})
	if err == nil {
		t.Fatal("expected error for path traversal outside CWD, got nil")
	}
	if !strings.Contains(err.Error(), "outside the working directory") {
		t.Errorf("expected 'outside the working directory' in error, got: %v", err)
	}
}

// ── new guard: line_edit start_line < 1 is rejected ──────────────────────────

func TestWriteLineEditStartLineLessThanOne(t *testing.T) {
	dir := t.TempDir()
	tool := NewWriteTool(dir)
	mustWrite(t, tool, WriteInput{Path: "le0.txt", Content: "a\nb\n", Mode: WriteModeOverwrite})

	zero := 0
	_, err := tool.Execute(context.Background(), WriteInput{
		Path:      "le0.txt",
		Mode:      WriteModeLineEdit,
		StartLine: &zero,
		EndLine:   &zero,
		Content:   "x",
	})
	if err == nil {
		t.Fatal("expected error for start_line=0, got nil")
	}
	if !strings.Contains(err.Error(), "start_line") {
		t.Errorf("expected 'start_line' in error, got: %v", err)
	}
}

// ── new guard: WriteDetails only has preserve_trailing_newline for line_edit ──

func TestWriteDetailsPreserveTrailingNewlineOnlyForLineEdit(t *testing.T) {
	dir := t.TempDir()
	tool := NewWriteTool(dir)

	// overwrite: preserve_trailing_newline must NOT appear in details
	res, err := tool.Execute(context.Background(), WriteInput{
		Path:    "ptn.txt",
		Mode:    WriteModeOverwrite,
		Content: "hello\n",
	})
	if err != nil {
		t.Fatalf("write failed: %v", err)
	}
	d, ok := res.Details.(WriteDetails)
	if !ok {
		t.Fatalf("expected WriteDetails, got %T", res.Details)
	}
	if d.PreserveTrailingNewline != nil {
		t.Errorf("preserve_trailing_newline should be nil for overwrite mode, got %v", d.PreserveTrailingNewline)
	}

	// line_edit: preserve_trailing_newline MUST appear in details
	s, e := 1, 1
	res2, err := tool.Execute(context.Background(), WriteInput{
		Path:      "ptn.txt",
		Mode:      WriteModeLineEdit,
		StartLine: &s,
		EndLine:   &e,
		Content:   "world",
	})
	if err != nil {
		t.Fatalf("line_edit failed: %v", err)
	}
	d2, ok := res2.Details.(WriteDetails)
	if !ok {
		t.Fatalf("expected WriteDetails, got %T", res2.Details)
	}
	if d2.PreserveTrailingNewline == nil {
		t.Error("preserve_trailing_newline should be set for line_edit mode")
	}
}

// ── new guard: patch hunk with oldStart == 0 is rejected ─────────────────────

func TestPatchModeZeroOldStartRejected(t *testing.T) {
	dir := t.TempDir()
	tool := NewWriteTool(dir)
	mustWrite(t, tool, WriteInput{Path: "z.txt", Content: "a\nb\n", Mode: WriteModeOverwrite})

	badPatch := strings.Join([]string{
		"--- a/z.txt",
		"+++ b/z.txt",
		"@@ -0,1 +0,1 @@", // invalid: 0-indexed start
		"-a",
		"+A",
	}, "\n")
	_, err := tool.Execute(context.Background(), WriteInput{
		Path:        "z.txt",
		Mode:        WriteModePatch,
		UnifiedDiff: badPatch,
	})
	if err == nil {
		t.Fatal("expected error for hunk with oldStart=0, got nil")
	}
	if !strings.Contains(err.Error(), "1-indexed") {
		t.Errorf("expected '1-indexed' guidance in error, got: %v", err)
	}
}

// ── diff label change: "Changes:" instead of "--- diff ---" ──────────────────

func TestWriteDiffLabelIsChanges(t *testing.T) {
	dir := t.TempDir()
	tool := NewWriteTool(dir)
	mustWrite(t, tool, WriteInput{Path: "lbl.txt", Content: "a\nb\nc\n", Mode: WriteModeOverwrite})
	res, err := tool.Execute(context.Background(), WriteInput{
		Path:    "lbl.txt",
		Mode:    WriteModeOverwrite,
		Content: "a\nB\nc\n",
	})
	if err != nil {
		t.Fatalf("write failed: %v", err)
	}
	text := res.Content[0].Text
	if !strings.Contains(text, "Changes:") {
		t.Errorf("expected 'Changes:' label in diff output, got: %s", text)
	}
	if strings.Contains(text, "--- diff ---") {
		t.Errorf("old '--- diff ---' label must be removed, got: %s", text)
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// Feature: append mode
// ═══════════════════════════════════════════════════════════════════════════

// TestAppendModeToExistingFile verifies basic append to an existing file.
func TestAppendModeToExistingFile(t *testing.T) {
	dir := t.TempDir()
	tool := NewWriteTool(dir)
	mustWrite(t, tool, WriteInput{Path: "app_ex.txt", Content: "line1\nline2\n", Mode: WriteModeOverwrite})

	mustWrite(t, tool, WriteInput{
		Path:    "app_ex.txt",
		Mode:    WriteModeAppend,
		Content: "line3\n",
	})

	got := readFile(t, NewReadTool(dir), "app_ex.txt")
	if got != "line1\nline2\nline3\n" {
		t.Fatalf("append produced wrong result: %q", got)
	}
}

// TestAppendModeCreatesNewFile verifies append creates the file when it does not exist.
func TestAppendModeCreatesNewFile(t *testing.T) {
	dir := t.TempDir()
	tool := NewWriteTool(dir)

	mustWrite(t, tool, WriteInput{
		Path:    "app_new.txt",
		Mode:    WriteModeAppend,
		Content: "hello\n",
	})

	got := readFile(t, NewReadTool(dir), "app_new.txt")
	if got != "hello\n" {
		t.Fatalf("append to new file produced wrong result: %q", got)
	}
}

// TestAppendModeEmptyContentRejected verifies empty content is refused.
func TestAppendModeEmptyContentRejected(t *testing.T) {
	dir := t.TempDir()
	tool := NewWriteTool(dir)
	mustWrite(t, tool, WriteInput{Path: "app_mt.txt", Content: "x\n", Mode: WriteModeOverwrite})

	_, err := tool.Execute(context.Background(), WriteInput{
		Path:    "app_mt.txt",
		Mode:    WriteModeAppend,
		Content: "",
	})
	if err == nil {
		t.Fatal("expected error for empty append content, got nil")
	}
	if !strings.Contains(err.Error(), "non-empty") {
		t.Errorf("expected 'non-empty' in error, got: %v", err)
	}
	if readFile(t, NewReadTool(dir), "app_mt.txt") != "x\n" {
		t.Error("file was modified by rejected append")
	}
}

// TestAppendModeDryRun verifies dry_run does not write.
func TestAppendModeDryRun(t *testing.T) {
	dir := t.TempDir()
	tool := NewWriteTool(dir)
	mustWrite(t, tool, WriteInput{Path: "app_dr.txt", Content: "orig\n", Mode: WriteModeOverwrite})

	dry := true
	res, err := tool.Execute(context.Background(), WriteInput{
		Path:    "app_dr.txt",
		Mode:    WriteModeAppend,
		Content: "added\n",
		DryRun:  &dry,
	})
	if err != nil {
		t.Fatalf("dry-run append failed: %v", err)
	}
	if readFile(t, NewReadTool(dir), "app_dr.txt") != "orig\n" {
		t.Error("dry-run must not write the file")
	}
	d := res.Details.(WriteDetails)
	if !d.DryRun {
		t.Error("expected DryRun=true in details")
	}
	if d.Insertions == 0 {
		t.Error("expected insertions > 0 in dry-run diff")
	}
}

// TestAppendModeDetailsMode verifies WriteDetails.Mode is "append".
func TestAppendModeDetailsMode(t *testing.T) {
	dir := t.TempDir()
	tool := NewWriteTool(dir)
	res, err := tool.Execute(context.Background(), WriteInput{
		Path:    "app_dm.txt",
		Mode:    WriteModeAppend,
		Content: "line\n",
	})
	if err != nil {
		t.Fatalf("append failed: %v", err)
	}
	d := res.Details.(WriteDetails)
	if d.Mode != WriteModeAppend {
		t.Errorf("expected mode=append in details, got %q", d.Mode)
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// Feature: insert_after_line mode
// ═══════════════════════════════════════════════════════════════════════════

// TestInsertAfterLineMiddle inserts lines in the middle of a file.
func TestInsertAfterLineMiddle(t *testing.T) {
	dir := t.TempDir()
	tool := NewWriteTool(dir)
	mustWrite(t, tool, WriteInput{Path: "ial_mid.txt", Content: "a\nb\nc\n", Mode: WriteModeOverwrite})

	one := 1
	mustWrite(t, tool, WriteInput{
		Path:      "ial_mid.txt",
		Mode:      WriteModeInsertAfterLine,
		AfterLine: &one,
		Content:   "X\nY",
	})

	got := readFile(t, NewReadTool(dir), "ial_mid.txt")
	if got != "a\nX\nY\nb\nc\n" {
		t.Fatalf("insert_after_line(1) produced wrong result: %q", got)
	}
}

// TestInsertAfterLineZeroInsertsAtTop inserts before the first line.
func TestInsertAfterLineZeroInsertsAtTop(t *testing.T) {
	dir := t.TempDir()
	tool := NewWriteTool(dir)
	mustWrite(t, tool, WriteInput{Path: "ial_top.txt", Content: "b\nc\n", Mode: WriteModeOverwrite})

	zero := 0
	mustWrite(t, tool, WriteInput{
		Path:      "ial_top.txt",
		Mode:      WriteModeInsertAfterLine,
		AfterLine: &zero,
		Content:   "a",
	})

	got := readFile(t, NewReadTool(dir), "ial_top.txt")
	if got != "a\nb\nc\n" {
		t.Fatalf("insert_after_line(0) produced wrong result: %q", got)
	}
}

// TestInsertAfterLineAtEnd inserts after the last line.
func TestInsertAfterLineAtEnd(t *testing.T) {
	dir := t.TempDir()
	tool := NewWriteTool(dir)
	mustWrite(t, tool, WriteInput{Path: "ial_end.txt", Content: "a\nb\n", Mode: WriteModeOverwrite})

	two := 2
	mustWrite(t, tool, WriteInput{
		Path:      "ial_end.txt",
		Mode:      WriteModeInsertAfterLine,
		AfterLine: &two,
		Content:   "c",
	})

	got := readFile(t, NewReadTool(dir), "ial_end.txt")
	if got != "a\nb\nc\n" {
		t.Fatalf("insert_after_line(end) produced wrong result: %q", got)
	}
}

// TestInsertAfterLineOutOfBoundsError verifies an out-of-range after_line is rejected.
func TestInsertAfterLineOutOfBoundsError(t *testing.T) {
	dir := t.TempDir()
	tool := NewWriteTool(dir)
	mustWrite(t, tool, WriteInput{Path: "ial_oob.txt", Content: "a\nb\n", Mode: WriteModeOverwrite})

	big := 99
	_, err := tool.Execute(context.Background(), WriteInput{
		Path:      "ial_oob.txt",
		Mode:      WriteModeInsertAfterLine,
		AfterLine: &big,
		Content:   "x",
	})
	if err == nil {
		t.Fatal("expected out-of-bounds error, got nil")
	}
	if !strings.Contains(err.Error(), "out of bounds") {
		t.Errorf("expected 'out of bounds' in error, got: %v", err)
	}
}

// TestInsertAfterLineNegativeAfterLineError verifies negative after_line is rejected.
func TestInsertAfterLineNegativeAfterLineError(t *testing.T) {
	dir := t.TempDir()
	tool := NewWriteTool(dir)
	mustWrite(t, tool, WriteInput{Path: "ial_neg.txt", Content: "a\n", Mode: WriteModeOverwrite})

	neg := -1
	_, err := tool.Execute(context.Background(), WriteInput{
		Path:      "ial_neg.txt",
		Mode:      WriteModeInsertAfterLine,
		AfterLine: &neg,
		Content:   "x",
	})
	if err == nil {
		t.Fatal("expected error for after_line=-1, got nil")
	}
	if !strings.Contains(err.Error(), ">= 0") {
		t.Errorf("expected '>= 0' guidance in error, got: %v", err)
	}
}

// TestInsertAfterLineMissingAfterLineError verifies missing after_line field is caught.
func TestInsertAfterLineMissingAfterLineError(t *testing.T) {
	dir := t.TempDir()
	tool := NewWriteTool(dir)
	mustWrite(t, tool, WriteInput{Path: "ial_miss.txt", Content: "a\n", Mode: WriteModeOverwrite})

	_, err := tool.Execute(context.Background(), WriteInput{
		Path:    "ial_miss.txt",
		Mode:    WriteModeInsertAfterLine,
		Content: "x",
		// AfterLine intentionally omitted
	})
	if err == nil {
		t.Fatal("expected error for missing after_line, got nil")
	}
	if !strings.Contains(err.Error(), "after_line") {
		t.Errorf("expected 'after_line' in error, got: %v", err)
	}
}

// TestInsertAfterLineEmptyContentError verifies empty content is rejected.
func TestInsertAfterLineEmptyContentError(t *testing.T) {
	dir := t.TempDir()
	tool := NewWriteTool(dir)
	mustWrite(t, tool, WriteInput{Path: "ial_ec.txt", Content: "a\n", Mode: WriteModeOverwrite})

	one := 1
	_, err := tool.Execute(context.Background(), WriteInput{
		Path:      "ial_ec.txt",
		Mode:      WriteModeInsertAfterLine,
		AfterLine: &one,
		Content:   "",
	})
	if err == nil {
		t.Fatal("expected error for empty insert content, got nil")
	}
}

// TestInsertAfterLineRequiresExistingFile verifies mode fails on missing files.
func TestInsertAfterLineRequiresExistingFile(t *testing.T) {
	dir := t.TempDir()
	tool := NewWriteTool(dir)

	zero := 0
	_, err := tool.Execute(context.Background(), WriteInput{
		Path:      "ial_missing.txt",
		Mode:      WriteModeInsertAfterLine,
		AfterLine: &zero,
		Content:   "x",
	})
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
	if !strings.Contains(err.Error(), "requires an existing file") {
		t.Errorf("expected 'requires an existing file' in error, got: %v", err)
	}
}

// TestInsertAfterLineDetailsFields verifies WriteDetails contains after_line and
// preserve_trailing_newline, but NOT start_line / end_line.
func TestInsertAfterLineDetailsFields(t *testing.T) {
	dir := t.TempDir()
	tool := NewWriteTool(dir)
	mustWrite(t, tool, WriteInput{Path: "ial_det.txt", Content: "a\nb\n", Mode: WriteModeOverwrite})

	one := 1
	res, err := tool.Execute(context.Background(), WriteInput{
		Path:      "ial_det.txt",
		Mode:      WriteModeInsertAfterLine,
		AfterLine: &one,
		Content:   "x",
	})
	if err != nil {
		t.Fatalf("insert_after_line failed: %v", err)
	}
	d, ok := res.Details.(WriteDetails)
	if !ok {
		t.Fatalf("expected WriteDetails, got %T", res.Details)
	}
	if d.AfterLine == nil || *d.AfterLine != 1 {
		t.Errorf("expected after_line=1 in details, got %v", d.AfterLine)
	}
	if d.PreserveTrailingNewline == nil {
		t.Error("expected preserve_trailing_newline to be set for insert_after_line")
	}
	if d.StartLine != nil || d.EndLine != nil {
		t.Error("start_line/end_line must not appear in insert_after_line details")
	}
	if d.Mode != WriteModeInsertAfterLine {
		t.Errorf("expected mode=insert_after_line in details, got %q", d.Mode)
	}
}

// TestInsertAfterLineDryRun verifies dry_run does not write.
func TestInsertAfterLineDryRun(t *testing.T) {
	dir := t.TempDir()
	tool := NewWriteTool(dir)
	mustWrite(t, tool, WriteInput{Path: "ial_dr.txt", Content: "a\nb\n", Mode: WriteModeOverwrite})

	one := 1
	dry := true
	res, err := tool.Execute(context.Background(), WriteInput{
		Path:      "ial_dr.txt",
		Mode:      WriteModeInsertAfterLine,
		AfterLine: &one,
		Content:   "X",
		DryRun:    &dry,
	})
	if err != nil {
		t.Fatalf("dry-run insert_after_line failed: %v", err)
	}
	if readFile(t, NewReadTool(dir), "ial_dr.txt") != "a\nb\n" {
		t.Error("dry-run must not write the file")
	}
	d := res.Details.(WriteDetails)
	if !d.DryRun {
		t.Error("expected DryRun=true in details")
	}
	if d.Insertions == 0 {
		t.Error("expected insertions > 0 in dry-run diff")
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// Feature: sha256_before stale-write protection
// ═══════════════════════════════════════════════════════════════════════════

// TestSHA256BeforePassesWhenMatchingHash verifies the write succeeds when
// sha256_before matches the current file hash.
func TestSHA256BeforePassesWhenMatchingHash(t *testing.T) {
	dir := t.TempDir()
	tool := NewWriteTool(dir)
	mustWrite(t, tool, WriteInput{Path: "sha_ok.txt", Content: "hello\n", Mode: WriteModeOverwrite})

	out, err := NewReadTool(dir).Execute(context.Background(), ReadInput{Path: "sha_ok.txt"})
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	rd := out.Details.(ReadDetails)

	one := 1
	_, err = tool.Execute(context.Background(), WriteInput{
		Path:            "sha_ok.txt",
		Mode:            WriteModeReplace,
		Find:            "hello",
		Content:         "world",
		ExpectedMatches: &one,
		SHA256Before:    rd.SHA256,
	})
	if err != nil {
		t.Fatalf("expected success with correct sha256_before, got: %v", err)
	}
	if readFile(t, NewReadTool(dir), "sha_ok.txt") != "world\n" {
		t.Error("file content not updated")
	}
}

// TestSHA256BeforeRejectsStaleWrite verifies the write fails when sha256_before
// does not match the current file.
func TestSHA256BeforeRejectsStaleWrite(t *testing.T) {
	dir := t.TempDir()
	tool := NewWriteTool(dir)
	mustWrite(t, tool, WriteInput{Path: "stale.txt", Content: "v1\n", Mode: WriteModeOverwrite})

	// Concurrent modification: another write changes the file.
	mustWrite(t, tool, WriteInput{Path: "stale.txt", Content: "v2\n", Mode: WriteModeOverwrite})

	staleHash := "0000000000000000000000000000000000000000000000000000000000000000"
	_, err := tool.Execute(context.Background(), WriteInput{
		Path:         "stale.txt",
		Mode:         WriteModeOverwrite,
		Content:      "agent-content\n",
		SHA256Before: staleHash,
	})
	if err == nil {
		t.Fatal("expected stale-write error, got nil")
	}
	if !strings.Contains(err.Error(), "stale write rejected") {
		t.Errorf("expected 'stale write rejected' in error, got: %v", err)
	}
	// File must be untouched (still v2).
	if readFile(t, NewReadTool(dir), "stale.txt") != "v2\n" {
		t.Error("stale write must not have mutated the file")
	}
}

// TestSHA256BeforeIgnoredForNewFile verifies that sha256_before is a no-op when
// the file does not exist yet (overwrite creating a new file).
func TestSHA256BeforeIgnoredForNewFile(t *testing.T) {
	dir := t.TempDir()
	tool := NewWriteTool(dir)

	_, err := tool.Execute(context.Background(), WriteInput{
		Path:         "sha_new.txt",
		Mode:         WriteModeOverwrite,
		Content:      "fresh\n",
		SHA256Before: "deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef",
	})
	if err != nil {
		t.Fatalf("expected success for new file regardless of sha256_before, got: %v", err)
	}
}

// TestSHA256AfterCanBeUsedAsSHA256Before verifies the round-trip:
// sha256_after from one write can be fed back as sha256_before for the next.
func TestSHA256AfterCanBeUsedAsSHA256Before(t *testing.T) {
	dir := t.TempDir()
	tool := NewWriteTool(dir)
	mustWrite(t, tool, WriteInput{Path: "sha_rt.txt", Content: "line1\n", Mode: WriteModeOverwrite})

	one := 1
	res1, err := tool.Execute(context.Background(), WriteInput{
		Path:            "sha_rt.txt",
		Mode:            WriteModeReplace,
		Find:            "line1",
		Content:         "line2",
		ExpectedMatches: &one,
	})
	if err != nil {
		t.Fatalf("first write failed: %v", err)
	}
	d1 := res1.Details.(WriteDetails)

	res2, err := tool.Execute(context.Background(), WriteInput{
		Path:            "sha_rt.txt",
		Mode:            WriteModeReplace,
		Find:            "line2",
		Content:         "line3",
		ExpectedMatches: &one,
		SHA256Before:    d1.SHA256After,
	})
	if err != nil {
		t.Fatalf("second write with sha256_before round-trip failed: %v", err)
	}
	d2 := res2.Details.(WriteDetails)
	if !d2.Changed {
		t.Error("expected second write to produce a change")
	}
	if readFile(t, NewReadTool(dir), "sha_rt.txt") != "line3\n" {
		t.Error("file content does not reflect second write")
	}
}

// TestSHA256BeforeWithAppendMode verifies stale detection works with append mode.
func TestSHA256BeforeWithAppendMode(t *testing.T) {
	dir := t.TempDir()
	tool := NewWriteTool(dir)
	mustWrite(t, tool, WriteInput{Path: "sha_ap.txt", Content: "base\n", Mode: WriteModeOverwrite})

	// Mutate so the hash no longer matches.
	mustWrite(t, tool, WriteInput{Path: "sha_ap.txt", Content: "changed\n", Mode: WriteModeOverwrite})

	_, err := tool.Execute(context.Background(), WriteInput{
		Path:         "sha_ap.txt",
		Mode:         WriteModeAppend,
		Content:      "extra\n",
		SHA256Before: "0000000000000000000000000000000000000000000000000000000000000000",
	})
	if err == nil {
		t.Fatal("expected stale-write error for append, got nil")
	}
	if !strings.Contains(err.Error(), "stale write rejected") {
		t.Errorf("expected 'stale write rejected', got: %v", err)
	}
}

// TestUnsupportedModeErrorMentionsNewModes verifies the error message lists
// the two new modes so agents know they exist.
func TestUnsupportedModeErrorMentionsNewModes(t *testing.T) {
	dir := t.TempDir()
	tool := NewWriteTool(dir)

	_, err := tool.Execute(context.Background(), WriteInput{
		Path:    "un.txt",
		Mode:    WriteMode("nonsense"),
		Content: "x",
	})
	if err == nil {
		t.Fatal("expected unsupported-mode error, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "append") {
		t.Errorf("expected 'append' in unsupported-mode error, got: %s", msg)
	}
	if !strings.Contains(msg, "insert_after_line") {
		t.Errorf("expected 'insert_after_line' in unsupported-mode error, got: %s", msg)
	}
}
