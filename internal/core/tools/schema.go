package tools

func ReadJSONSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path":   map[string]any{"type": "string", "description": "Path to the file to read (relative or absolute)"},
			"offset": map[string]any{"type": "number", "description": "Line number to start reading from (1-indexed)"},
			"limit":  map[string]any{"type": "number", "description": "Maximum number of lines to read"},
			"include_line_numbers": map[string]any{
				"type":        "boolean",
				"description": "Prefix each output line with its 1-indexed line number. Use this instead of running nl or cat -n in bash.",
			},
			"pattern": map[string]any{
				"type":        "string",
				"description": "Search for a literal string (or RE2 regex when pattern_is_regex=true) and return matching lines with line numbers and optional context. Use this instead of grep/nl in bash.",
			},
			"pattern_is_regex": map[string]any{
				"type":        "boolean",
				"description": "When true, treat pattern as a RE2 regex. Default false (literal string search).",
			},
			"context_lines": map[string]any{
				"type":        "number",
				"description": "Number of surrounding lines to include around each pattern match. Default 0.",
			},
		},
		"required": []string{"path"},
	}
}

func WriteJSONSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Path to the file to create or edit (relative or absolute). Required.",
			},
			"mode": map[string]any{
				"type": "string",
				"enum": []string{"overwrite", "append", "replace", "replace_regex", "line_edit", "insert_after_line", "patch"},
				"description": "Edit strategy (default: overwrite). " +
					"overwrite=replace whole file (content required, must not be empty). " +
					"append=add text to end of file or create it (content required). " +
					"replace=literal find/replace in existing content (requires find+content, set expected_matches=1 for single occurrence). " +
					"replace_regex=RE2 find/replace in existing content (requires find+content). " +
					"line_edit=replace start_line..end_line inclusive (requires start_line+end_line+content). " +
					"insert_after_line=insert lines after after_line without replacing (requires after_line+content; after_line=0 inserts at top). " +
					"patch=apply unified diff with hunk headers @@ -old,+new @@ (requires unified_diff).",
			},
			"content": map[string]any{
				"type":        "string",
				"description": "New file content (overwrite, must not be empty), text to append (append), or replacement/inserted text (replace/replace_regex/line_edit/insert_after_line).",
			},
			"sha256_before": map[string]any{
				"type":        "string",
				"description": "Optional. Hex SHA-256 of the file as you last read it. If the on-disk file differs, the write is rejected to prevent stale overwrites. Obtain from the read tool's details.sha256 field.",
			},
			"find": map[string]any{
				"type":        "string",
				"description": "Required for replace/replace_regex. Exact literal text to find (replace) or RE2 regex pattern (replace_regex).",
			},
			"expected_matches": map[string]any{
				"type":        "number",
				"description": "Recommended for replace/replace_regex. Expected number of matches; tool errors if actual count differs. Set to 1 when targeting a single occurrence.",
			},
			"max_replacements": map[string]any{
				"type":        "number",
				"description": "Optional for replace/replace_regex. Maximum number of replacements to apply. Defaults to all matches.",
			},
			"start_line": map[string]any{
				"type":        "number",
				"description": "Required for line_edit. 1-indexed start line (inclusive). Use read with include_line_numbers=true to find exact line numbers.",
			},
			"end_line": map[string]any{
				"type":        "number",
				"description": "Required for line_edit. 1-indexed end line (inclusive). Must be >= start_line.",
			},
			"after_line": map[string]any{
				"type":        "number",
				"description": "Required for insert_after_line. 1-indexed line after which content is inserted. Use 0 to insert before line 1 (top of file).",
			},
			"unified_diff": map[string]any{
				"type":        "string",
				"description": "Required for patch mode. Standard unified diff with file headers (---/+++) and hunk headers (@@ -old,+new @@). Do not use *** Begin Patch wrappers.",
			},
			"dry_run": map[string]any{
				"type":        "boolean",
				"description": "When true, compute and return the diff without writing the file. Useful to verify changes before committing.",
			},
			"preserve_trailing_newline": map[string]any{
				"type":        "boolean",
				"description": "line_edit and insert_after_line only. When true (default), preserve the original file's trailing newline after the edit.",
			},
			"include_preview": map[string]any{
				"type":        "boolean",
				"description": "When true, append a head-truncated preview of the resulting file to the response. Default false — keeps responses compact.",
			},
		},
		"required": []string{"path"},
	}
}

func ShellJSONSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"command": map[string]any{"type": "string", "description": "Shell command to execute"},
			"timeout": map[string]any{"type": "number", "description": "Timeout in seconds (optional, no default timeout)"},
		},
		"required": []string{"command"},
	}
}
