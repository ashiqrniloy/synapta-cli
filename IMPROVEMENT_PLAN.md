# Synapta CLI — Comprehensive Improvement Plan

> **Generated:** Review of ~10,650 lines of Go across 32 files.
> **Scope:** Performance, dead code, refactoring, modularity, and maintenance.

---

## Table of Contents

- [Executive Summary](#executive-summary)
- [P0 — Critical: Architecture \& Maintainability](#p0--critical-architecture--maintainability)
  - [1. Decompose `codeagent.go` (3,405-line God Object)](#1-decompose-codeagentgo-3405-line-god-object)
  - [2. Cache Providers in `ChatService.providerFor()`](#2-cache-providers-in-chatserviceproviderfor)
  - [3. Deduplicate `llm/kilo.go` and `oauth/kilo.go`](#3-deduplicate-llmkilogo-and-oauthkilogo)
- [P1 — High: Dead Code \& Unused Abstractions](#p1--high-dead-code--unused-abstractions)
  - [4. Remove Unused `Manager`/`Registry` Abstraction](#4-remove-unused-managerregistry-abstraction)
  - [5. Remove Additional Dead Functions and Types](#5-remove-additional-dead-functions-and-types)
- [P2 — Medium: Performance Issues](#p2--medium-performance-issues)
  - [6. Cache `buildContextEntries()` Results](#6-cache-buildcontextentries-results)
  - [7. Shared HTTP Client with Proper Configuration](#7-shared-http-client-with-proper-configuration)
  - [8. Replace O(n\*m) LCS Diff Algorithm](#8-replace-onm-lcs-diff-algorithm)
  - [9. Deduplicate `estimateTextTokens()`](#9-deduplicate-estimatetexttokens)
  - [10. Use Built-in `min()`/`max()`](#10-use-built-in-minmax)
- [P3 — Low: Code Quality \& Modularity](#p3--low-code-quality--modularity)
  - [11. Split `llm/types.go` into Focused Files](#11-split-llmtypesgo-into-focused-files)
  - [12. Simplify Config Loading with `viper.UnmarshalKey`](#12-simplify-config-loading-with-viperunmarshalkey)
  - [13. Fix `SaveConfig()` Scope/Naming](#13-fix-saveconfig-scopenaming)
  - [14. Remove Trivial `core/toolset.go` Wrapper](#14-remove-trivial-coretoolsetgo-wrapper)
  - [15. Add LLM Streaming Cancellation Support](#15-add-llm-streaming-cancellation-support)
  - [16. Buffer Session File Writes](#16-buffer-session-file-writes)
  - [17. Fix Kilo Token Refresh](#17-fix-kilo-token-refresh)
  - [18. Cache `escapeXML` Replacer](#18-cache-escapexml-replacer)
- [Summary Priority Matrix](#summary-priority-matrix)
- [Recommended Execution Order](#recommended-execution-order)

---

## Executive Summary

The codebase is a well-structured agentic coding assistant with a Bubbletea TUI, multi-provider LLM support (Kilo Gateway + GitHub Copilot), session persistence, context management, and a skills system. However, there are significant opportunities across four dimensions:

| Dimension | Key Finding |
|---|---|
| **Architecture** | `codeagent.go` at 3,405 lines is a God Object; ~880 lines of Kilo logic is duplicated across two packages |
| **Dead Code** | ~430+ lines of entirely unused code (Manager/Registry system, orphaned functions/types) |
| **Performance** | Provider recreation + model list fetch on every API call; context entries rebuilt on every TUI render cycle |
| **Modularity** | `llm/types.go` is a 541-line grab bag; config parsing is fragile and manual |

Addressing all items removes ~650+ lines of dead/duplicate code, eliminates redundant network calls on every chat interaction, and makes the codebase significantly easier to maintain and extend.

---


---



## P2 — Medium: Performance Issues
---

### 7. Shared HTTP Client with Proper Configuration

**Locations:**
- `internal/llm/provider.go` — uses `http.DefaultClient`
- `internal/oauth/kilo.go` — uses `http.DefaultClient`
- `internal/oauth/github_copilot.go` — uses `http.DefaultClient`
- `internal/llm/kilo.go` — creates client with 10s timeout

**Problem:**
10+ callsites use `http.DefaultClient` which has **no timeout configured**. Each file handles HTTP client creation inconsistently: `KiloGateway` creates one with a 10s timeout, but `OpenAIProvider` uses `DefaultClient` with no timeout at all. A stalled connection to any provider could hang the entire application indefinitely.

**What To Do:**
1. Create a shared utility (e.g., `internal/httpclient/client.go` or a function in `internal/llm/`) that returns a configured `*http.Client` with sensible defaults: 30s timeout, keep-alive enabled, connection pooling.
2. Replace all `http.DefaultClient.Do()` calls with the shared client.
3. Pass the client to providers via constructor injection for testability.

**Why:**
- Prevents hanging on stalled connections.
- Consistent timeout and pooling behavior across all providers.
- Enables future features like retry logic, circuit-breaking, or request tracing.
- Testability: inject a mock client for unit tests.

**Effort:** ~1 hour

---

### 9. Deduplicate `estimateTextTokens()`

**Locations:**
- `internal/core/session_store.go:791`
- `internal/tui/components/codeagent.go:2110`

**Problem:**
The exact same function is defined in two places: `(len(text) + 3) / 4`. If the estimation logic is ever improved (e.g., using a proper tokenizer), it must be updated in both locations.

**What To Do:**
Move to a shared utility (e.g., `internal/core/tokenutil.go` or `internal/llm/tokens.go`) and import from both locations.

**Why:**
- Single definition, consistent behavior.
- One place to improve the estimation later (e.g., swap in a real tokenizer).

**Effort:** ~15 minutes


---


### 12. Simplify Config Loading with `viper.UnmarshalKey`

**Location:** `internal/config/config.go` — `LoadConfig()`

**Problem:**
The function manually extracts each keybinding field individually:
```go
if v, ok := kb["newline"]; ok && v != "" { cfg.Keybindings.Newline = v }
if v, ok := kb["submit"]; ok && v != "" { cfg.Keybindings.Submit = v }
// ... repeated 6 times
```
Same pattern for theme loading — it walks all viper keys with prefix matching.

**What To Do:**
Use `viper.UnmarshalKey("keybindings", &cfg.Keybindings)` with proper struct tags and merge behavior instead of manual field-by-field extraction.

**Why:**
- Less code, fewer bugs when adding new keybindings.
- Automatic handling of new fields without touching the loading logic.
- Consistent with Viper's intended usage pattern.

**Effort:** ~30 minutes

---

### 13. Fix `SaveConfig()` Scope/Naming

**Location:** `internal/config/config.go` — `SaveConfig()`

**Problem:**
`SaveConfig()` accepts the full `AppConfig` struct but **only writes the `provider` section** when updating an existing file. The function name and signature suggest it saves the entire config, but it silently ignores keybindings, theme, and other sections.

**What To Do:**
Either:
- Rename to `SaveProviderConfig()` to accurately describe its behavior, **or**
- Implement full-section merging so it truly saves the complete config.

**Why:**
- Clear API contract — no surprise behavior.
- Prevents future bugs where a caller assumes `SaveConfig` persists their keybinding changes.

**Effort:** ~15 minutes

---

### 14. Remove Trivial `core/toolset.go` Wrapper

**Location:** `internal/core/toolset.go` (9 lines)

**Problem:**
The entire file is a one-function wrapper:
```go
func NewToolSet(cwd string) *tools.ToolSet {
    return tools.NewToolSet(cwd)
}
```
This adds a layer of indirection with zero added value. `core/core.go` is similarly just a package doc comment.

**What To Do:**
Remove `core/toolset.go`. Have callers import and call `tools.NewToolSet()` directly.

**Why:**
- Less indirection, clearer dependency graph.
- One fewer file to maintain.
- Callers can see exactly what they're getting.

**Effort:** ~10 minutes

---



---

### 16. Buffer Session File Writes

**Location:** `internal/core/session_store.go` — `appendEntryLocked()`

**Problem:**
`appendEntryLocked()` opens the file, writes one JSON line, and closes it — on **every single message**. During a tool-heavy session with bash commands, reads, and writes, this could be dozens of open/write/close cycles per interaction.

**What To Do:**
Either:
- Use a `bufio.Writer` with periodic flushing, **or**
- Keep the file handle open for the duration of the session and close on session switch/exit, **or**
- Batch writes — collect entries and flush every N entries or every T seconds.

**Why:**
- Reduced I/O system calls.
- Faster session persistence, especially on networked filesystems.
- Lower disk contention during tool-heavy interactions.

**Effort:** ~1 hour

---

### 17. Fix Kilo Token Refresh

**Location:** `internal/oauth/kilo.go` — `RefreshToken()`

**Problem:**
The `RefreshToken()` implementation simply checks if the token has expired and returns either the existing credentials or an error:
```go
func (k *KiloOAuth) RefreshToken(creds *llm.OAuthCredentials) (*llm.OAuthCredentials, error) {
    if creds.ExpiresAt != nil && time.Now().After(*creds.ExpiresAt) {
        return nil, fmt.Errorf("kilo token has expired, please re-authenticate")
    }
    return creds, nil
}
```
It **doesn't actually refresh** the token. This means Kilo tokens silently fail after expiry with no automatic recovery.

**What To Do:**
Either:
- Implement actual token refresh using a refresh token (if Kilo's API supports it), **or**
- Trigger an automatic re-authentication flow when the token expires, **or**
- At minimum, surface a clear user-facing notification in the TUI when the token has expired with instructions to re-auth.

**Why:**
- Users don't get unexplained auth failures after long sessions.
- Reduces friction — users shouldn't need to manually re-authenticate.

**Effort:** ~1 hour

---

### 18. Cache `escapeXML` Replacer

**Location:** `internal/core/skills.go` — `escapeXML()`

**Problem:**
`escapeXML()` creates a new `strings.Replacer` on every call:
```go
func escapeXML(s string) string {
    r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&apos;")
    return r.Replace(s)
}
```
This is called inside a loop for every skill (name + description + path = 3 calls per skill). Each call allocates a new `strings.Replacer`.

**What To Do:**
Move the `Replacer` to a package-level variable:
```go
var xmlReplacer = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&apos;")

func escapeXML(s string) string {
    return xmlReplacer.Replace(s)
}
```

**Why:**
- Zero allocation in the hot path.
- `strings.Replacer` is safe for concurrent use.
- Trivial change with measurable improvement when many skills are loaded.

**Effort:** ~5 minutes

---

## Summary Priority Matrix

| Priority | # | Item | Impact | Effort |
|---|---|---|---|---|
| **P0** | 1 | Decompose `codeagent.go` (3,405 lines) | High maintenance risk | ~4 hours |
| **P0** | 2 | Cache providers in `ChatService` | Network call on every chat turn | ~1 hour |
| **P0** | 3 | Deduplicate `llm/kilo.go` vs `oauth/kilo.go` | ~400 lines duplicate code | ~2 hours |
| **P1** | 4 | Remove unused `Manager`/`Registry` (~180 lines) | Dead code confusion | ~30 min |
| **P1** | 5 | Remove all dead functions/types (~250+ lines) | Dead code | ~1 hour |
| **P2** | 6 | Cache `buildContextEntries()` results | Disk I/O on every render | ~1 hour |
| **P2** | 7 | Shared HTTP client | No timeouts, inconsistent | ~1 hour |
| **P2** | 8 | Replace O(n*m) LCS diff | Memory spikes on large files | ~2 hours |
| **P2** | 9 | Deduplicate `estimateTextTokens` | Duplicate logic | ~15 min |
| **P2** | 10 | Use builtin `min`/`max` | Duplicate helpers | ~15 min |
| **P3** | 11 | Split `llm/types.go` | Single-file grab bag | ~1 hour |
| **P3** | 12 | Use `viper.UnmarshalKey` for config | Fragile manual parsing | ~30 min |
| **P3** | 13 | Fix `SaveConfig` scope/naming | Misleading API | ~15 min |
| **P3** | 14 | Remove trivial `core/toolset.go` wrapper | Unnecessary indirection | ~10 min |
| **P3** | 15 | Add streaming cancellation | UX blocker on long requests | ~2 hours |
| **P3** | 16 | Buffer session file writes | Excessive I/O syscalls | ~1 hour |
| **P3** | 17 | Fix Kilo token refresh | Silent auth failure | ~1 hour |
| **P3** | 18 | Cache `escapeXML` replacer | Micro-allocation per skill | ~5 min |

**Total estimated effort:** ~18 hours

---

## Recommended Execution Order

The order below optimizes for **maximum impact with minimal risk**, grouping quick wins together and tackling the big refactor after the codebase is cleaner.

### Phase 1: Quick Wins (Dead Code & Micro-fixes) — ~2 hours
1. **#10** — Use builtin `min`/`max` (15 min)
2. **#18** — Cache `escapeXML` replacer (5 min)
3. **#9** — Deduplicate `estimateTextTokens` (15 min)
4. **#4** — Remove unused `Manager`/`Registry` (30 min)
5. **#5** — Remove all dead functions/types (1 hour)

> **Outcome:** ~430+ lines of dead code removed. Codebase is cleaner and easier to reason about before tackling structural changes.

### Phase 2: Performance Fixes — ~2 hours
6. **#2** — Cache providers in `ChatService` (1 hour)
7. **#6** — Cache `buildContextEntries()` results (1 hour)

> **Outcome:** Eliminates the two biggest performance bottlenecks: redundant network calls per chat turn and disk I/O on every render cycle.

### Phase 3: Deduplication — ~2 hours
8. **#3** — Deduplicate `llm/kilo.go` vs `oauth/kilo.go` (2 hours)

> **Outcome:** ~400 lines of duplicate code consolidated into a single source of truth.

### Phase 4: Big Refactor — ~4 hours
9. **#1** — Decompose `codeagent.go` (4 hours)

> **Outcome:** The God Object is broken into 5+ focused files. This is safest to do after phases 1-3 because there's less dead code to move around.

### Phase 5: Structural Improvements — ~4 hours
10. **#7** — Shared HTTP client (1 hour)
11. **#11** — Split `llm/types.go` (1 hour)
12. **#15** — Add streaming cancellation (2 hours)

> **Outcome:** Consistent HTTP behavior, cleaner type organization, and users can cancel hung requests.

### Phase 6: Polish — ~4 hours
13. **#8** — Replace LCS diff algorithm (2 hours)
14. **#12** — Simplify config loading (30 min)
15. **#16** — Buffer session file writes (1 hour)
16. **#17** — Fix Kilo token refresh (1 hour)
17. **#13** — Fix `SaveConfig` naming (15 min)
18. **#14** — Remove `core/toolset.go` wrapper (10 min)

> **Outcome:** All remaining improvements applied. Codebase is clean, performant, and modular.
