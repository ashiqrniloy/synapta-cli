# Synapta Code Tool Improvement Plan

Ordered by priority for implementation.

1. **Add native line lookup to avoid bash `nl`/`grep` usage**
   - Add a `locate`/search mode or line-annotated read output.
   - Return matching line numbers and optional context.

2. **Make write results structured and compact**
   - Add machine-friendly metadata: `mode`, `changed`, `changed_ranges`, `insertions`, `deletions`, `bytes_before`, `bytes_after`.
   - Keep the human text summary short.

3. **Replace the quadratic diff implementation**
   - Swap the current LCS-based line diff for a better algorithm/library.
   - Preserve readable hunks for large files.

4. **Make preview output optional**
   - Add an `include_preview` flag.
   - Default to off for larger edits.

5. **Harden patch mode**
   - Apply unified diffs more directly and validate hunks clearly.
   - Reduce dependence on external `diff`/`patch` behavior where possible.

6. **Add compaction-friendly metadata**
   - Return file facts such as `line_count`, `byte_count`, `hash`, and truncation state.
   - Use these to reduce rereads and stale context.
