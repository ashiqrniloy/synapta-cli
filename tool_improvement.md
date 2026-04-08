# Tool Improvement Suggestions for Synapta Code Agent

## Scope Reviewed
- `/home/arniloy/synapta-cli/internal/core/tools/bash.go`
- `/home/arniloy/synapta-cli/internal/core/tools/read.go`
- `/home/arniloy/synapta-cli/internal/core/tools/write.go`
- `/home/arniloy/synapta-cli/internal/core/tools/types.go`
- `/home/arniloy/synapta-cli/internal/core/tools/truncate.go`
- `/home/arniloy/synapta-cli/internal/core/chat.go`
- `/home/arniloy/synapta-cli/internal/core/context_manager.go`

---

## Current Tool Behavior (Summary)

### Bash Tool
- Executes shell commands in cwd (`bash -lc`, fallback `sh -lc`, Windows support).
- Supports optional timeout.
- Streams partial updates.
- Truncates output to last 2000 lines / 50KB.
- Persists full output to temp log when truncated.
- Adds timeout/abort/exit diagnostics in text.

### Read Tool
- Reads text and image files.
- Supports `offset`/`limit` pagination.
- Truncates text at 2000 lines / 50KB with continuation hints.
- Returns truncation metadata in details.
- Has robust path normalization (`~`, unicode variants, etc.).

### Write Tool
- Supports `overwrite`, `replace`, `replace_regex`, `line_edit`, `patch`.
- Supports `dry_run` and match safety (`expected_matches`, `max_replacements`).
- Uses queued mutation + patch/cat strategy for durable writes.
- Returns text diff summary, preview, and details metadata.

---

## Improvement Suggestions

## P0 (Highest ROI)

1. **Add a structured `status` block to all tools**
   - Fields: `ok`, `error_type`, `exit_code`, `retryable`, `user_action`.
   - Benefit: avoids brittle parsing of freeform text and improves tool-result reliability.

2. **Add `file_facts` to Read results**
   - Fields: `language`, `line_count`, `byte_count`, `sha256`, `has_truncation`.
   - Benefit: helps the agent detect stale context and reason about file scope.

3. **Add `edit_facts` to Write results**
   - Fields: `changed_files`, `changed_line_ranges`, `insertions`, `deletions`, `applied_mode`.
   - Benefit: enables reasoning about impact without re-reading full files.

4. **Add `command_facts` to Bash results**
   - Fields: `exit_code`, `duration_ms`, `stdout_bytes`, `stderr_bytes`, `timed_out`.
   - Benefit: supports reliable debug loops and retry decisions.

---

## P1 (Accuracy + Context-Rot Control)

5. **Dual-channel output: `summary` + `artifacts`**
   - `summary`: concise model-facing digest.
   - `artifacts`: full logs/previews by file path or artifact id.
   - Benefit: keep context compact while preserving drill-down capability.

6. **Semantic Read modes**
   - Add `mode`: `full | symbols | imports | range | grep_context`.
   - Benefit: lets agent fetch only relevant slices for coding tasks.

7. **Write tool optional preview suppression**
   - Add `include_preview` flag (default `false` for large edits).
   - Benefit: prevents large repetitive previews from polluting context.

8. **Structured stdout/stderr in Bash details**
   - Keep human merged output but add split stats and optional split payloads.
   - Benefit: better error interpretation and remediation.

---

## P2 (Advanced)

9. **Cross-tool provenance IDs**
   - Return IDs like `read_id`, `write_id`, `bash_id`, and references (`derived_from_read_id`).
   - Benefit: traceability across long multi-step tasks.

10. **Tool-provided compaction hints**
   - Add `compact_hint` to each tool result indicating what should be retained long-term.
   - Benefit: helps avoid context rot in extended sessions.

11. **Repository map/index support**
   - Add a fast map of files, languages, key symbols, test commands.
   - Benefit: improves upfront planning and lowers exploratory token usage.

---

## Structured Output Redesign Proposal

Use a common envelope for all tools:

```json
{
  "summary": "short digest",
  "status": {
    "ok": true,
    "error_type": "",
    "retryable": false
  },
  "facts": {},
  "content": [{ "type": "text", "text": "optional short snippet" }],
  "artifacts": [
    { "kind": "full_output_log", "path": "/tmp/..." }
  ],
  "truncation": {},
  "next_actions": ["read offset=2001"]
}
```

Tool-specific `facts`:
- **bash**: `exit_code`, `duration_ms`, `timed_out`, stdout/stderr sizes.
- **read**: `abs_path`, `sha256`, `language`, `lines_returned`, `total_lines`.
- **write**: `changed`, `hunks`, `changed_ranges`, `insertions`, `deletions`, `post_write_sha256`.

---

## Context-Rot Mitigation Strategy

- Keep only `summary + facts + next_actions` in active chat context.
- Store full logs/previews as `artifacts`; fetch only when needed.
- Include file checksums and changed ranges so model state remains accurate.
- Prefer semantic reads (`symbols`, `imports`, targeted ranges) over full file dumps.

---

## Suggested Next Step

Implement schema changes incrementally and backward-compatibly:
1. Add new optional fields in `types.go`.
2. Start emitting them in `bash.go`, `read.go`, and `write.go`.
3. Update `chat.go` context packaging to prioritize compact `summary/facts` over verbose text.
