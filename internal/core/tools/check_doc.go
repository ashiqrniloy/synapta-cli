package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

type CheckDocInput struct {
	Method      string `json:"method,omitempty"`
	LibraryName string `json:"library_name,omitempty"`
	LibraryID   string `json:"library_id,omitempty"`
	Version     string `json:"version,omitempty"`
	Query       string `json:"query"`
	Timeout     *int   `json:"timeout,omitempty"`
	OutputJSON  *bool  `json:"output_json,omitempty"`
}

type CheckDocTool struct {
	shell *BashTool
}

func NewCheckDocTool(shell *BashTool) *CheckDocTool {
	return &CheckDocTool{shell: shell}
}

func (t *CheckDocTool) Name() string { return "check-doc" }

func (t *CheckDocTool) Description() string {
	return "Check official library documentation before coding. Currently supports Context7 CLI (ctx7) by resolving a library ID (ctx7 library) and then fetching targeted docs (ctx7 docs)."
}

func (t *CheckDocTool) Execute(ctx context.Context, in CheckDocInput) (Result, error) {
	if t == nil || t.shell == nil {
		return Result{}, fmt.Errorf("check-doc shell backend is not configured")
	}
	method := strings.ToLower(strings.TrimSpace(in.Method))
	if method == "" {
		method = "context7"
	}
	if method != "context7" {
		return Result{}, fmt.Errorf("unsupported check-doc method %q (supported: context7)", method)
	}
	query := strings.TrimSpace(in.Query)
	if query == "" {
		return Result{}, fmt.Errorf("query is required")
	}
	libraryName := strings.TrimSpace(in.LibraryName)
	libraryID := strings.TrimSpace(in.LibraryID)
	version := strings.TrimSpace(in.Version)
	if libraryID == "" && libraryName == "" {
		return Result{}, fmt.Errorf("library_name or library_id is required")
	}

	metaLines := []string{
		"method: context7",
	}
	if libraryName != "" {
		metaLines = append(metaLines, "library_name: "+libraryName)
	}
	if version != "" {
		metaLines = append(metaLines, "version: "+version)
	}
	metaLines = append(metaLines, "query: "+query)

	sections := []string{"[check-doc] invocation", strings.Join(metaLines, "\n")}

	if libraryID == "" {
		libraryCmd := "ctx7 library " + shellQuote(libraryName) + " " + shellQuote(query) + " --json"
		sections = append(sections, "[check-doc] shell command (resolve library id)\n"+libraryCmd)
		libRes, err := t.shell.Execute(ctx, BashInput{Command: libraryCmd, Timeout: in.Timeout}, nil)
		libOut := extractResultText(libRes)
		sections = append(sections, "[check-doc] shell output (resolve library id)\n"+fallbackNoOutput(libOut))
		if err != nil {
			text := strings.Join(sections, "\n\n")
			return Result{Content: []ContentPart{{Type: ContentPartText, Text: text}}}, err
		}
		resolvedID, pickErr := pickContext7LibraryIDFromJSON(libOut, version)
		if pickErr != nil {
			text := strings.Join(sections, "\n\n")
			return Result{Content: []ContentPart{{Type: ContentPartText, Text: text}}}, pickErr
		}
		libraryID = resolvedID
	}

	sections = append(sections, "[check-doc] resolved_library_id\n"+libraryID)

	docsCmd := "ctx7 docs " + shellQuote(libraryID) + " " + shellQuote(query)
	if in.OutputJSON != nil && *in.OutputJSON {
		docsCmd += " --json"
	}
	sections = append(sections, "[check-doc] shell command (fetch docs)\n"+docsCmd)
	docsRes, err := t.shell.Execute(ctx, BashInput{Command: docsCmd, Timeout: in.Timeout}, nil)
	docsOut := extractResultText(docsRes)
	sections = append(sections, "[check-doc] shell output (fetch docs)\n"+fallbackNoOutput(docsOut))

	text := strings.Join(sections, "\n\n")
	result := Result{
		Content: []ContentPart{{Type: ContentPartText, Text: text}},
		Details: map[string]any{
			"method":               method,
			"library_name":         libraryName,
			"library_id":           libraryID,
			"version":              version,
			"query":                query,
			"check_doc_shell_tool": "shell",
		},
	}
	if err != nil {
		return result, err
	}
	return result, nil
}

func pickContext7LibraryIDFromJSON(raw string, version string) (string, error) {
	text := strings.TrimSpace(raw)
	if text == "" {
		return "", fmt.Errorf("ctx7 library returned empty output")
	}
	var rows []map[string]any
	if err := json.Unmarshal([]byte(text), &rows); err != nil {
		return "", fmt.Errorf("ctx7 library output is not valid JSON: %w", err)
	}
	if len(rows) == 0 {
		return "", fmt.Errorf("ctx7 library returned no matches")
	}
	if v := strings.TrimSpace(version); v != "" {
		for _, row := range rows {
			for _, vid := range collectVersionIDs(row["versions"]) {
				if versionMatches(vid, v) {
					return vid, nil
				}
			}
		}
	}
	if id := strings.TrimSpace(anyString(rows[0]["id"])); id != "" {
		return id, nil
	}
	for _, row := range rows {
		if id := strings.TrimSpace(anyString(row["library_id"])); id != "" {
			return id, nil
		}
	}
	return "", fmt.Errorf("ctx7 library output did not include a usable library id")
}

func collectVersionIDs(v any) []string {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, item := range arr {
		switch x := item.(type) {
		case string:
			x = strings.TrimSpace(x)
			if x != "" {
				out = append(out, x)
			}
		case map[string]any:
			for _, key := range []string{"id", "library_id", "version_id"} {
				if s := strings.TrimSpace(anyString(x[key])); s != "" {
					out = append(out, s)
					break
				}
			}
		}
	}
	return out
}

func anyString(v any) string {
	s, _ := v.(string)
	return s
}

func versionMatches(versionID, wanted string) bool {
	id := strings.ToLower(strings.TrimSpace(versionID))
	w := strings.ToLower(strings.TrimSpace(wanted))
	if id == "" || w == "" {
		return false
	}
	if strings.Contains(id, "/"+w) || strings.HasSuffix(id, "/"+w) || strings.Contains(id, w) {
		return true
	}
	return false
}

func extractResultText(res Result) string {
	var b strings.Builder
	for _, c := range res.Content {
		if c.Type == ContentPartText && c.Text != "" {
			if b.Len() > 0 {
				b.WriteString("\n")
			}
			b.WriteString(c.Text)
		}
	}
	return strings.TrimSpace(b.String())
}

func fallbackNoOutput(s string) string {
	if strings.TrimSpace(s) == "" {
		return "(no output)"
	}
	return s
}

func shellQuote(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}
