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

### 3. Deduplicate `llm/kilo.go` and `oauth/kilo.go`

**Locations:**
- `internal/llm/kilo.go` (~500 lines) — `KiloGateway` struct
- `internal/oauth/kilo.go` (~380 lines) — `KiloOAuth` struct

**Problem:**
Both files implement nearly identical Kilo Gateway functionality:

| Capability | `llm/kilo.go` | `oauth/kilo.go` |
|---|---|---|
| Device auth flow | `initiateDeviceAuth` + `pollDeviceAuth` | `startDeviceAuth` + `pollDeviceAuth` |
| Model fetching | `FetchModels` + `mapModel` | `FetchModels` + `mapModel` |
| Balance fetching | `FetchBalance` | `FetchBalance` |
| Free-model detection | `isFreeModel` | `isFreeModel` |
| Price parsing | `parsePriceStr` | `parsePrice` |
| Response types | `DeviceAuthResponse`, `ModelsResponse`, etc. | `kiloDeviceAuthResponse`, `kiloModelsResponse`, etc. |

The `oauth/kilo.go` types use `*string` for pricing while `llm/kilo.go` uses `string`. The model mapping logic is slightly different. This is **~880 lines of near-duplicate code**.

**What To Do:**
1. Make `internal/llm/kilo.go` (`KiloGateway`) the **single source of truth** for all Kilo API interactions.
2. Rewrite `internal/oauth/kilo.go` (`KiloOAuth`) to **delegate** to `KiloGateway` for device auth, model fetching, and balance checking.
3. Remove all duplicate types, duplicate helper functions, and duplicate API call logic from `oauth/kilo.go`.
4. `KiloOAuth` should only contain the thin adapter logic needed to satisfy the `OAuthProvider` interface.

**Why:**
- ~400 lines of duplicate code removed.
- Single place to fix bugs or update API interactions.
- No risk of the two implementations drifting apart (they likely already have subtle differences in error handling).

**Effort:** ~2 hours

---

## P1 — High: Dead Code & Unused Abstractions

### 4. Remove Unused `Manager`/`Registry` Abstraction

**Location:** `internal/llm/types.go`

**Problem:**
The following types and functions are defined but **never called anywhere** outside their own file:

- `Manager` struct
- `Registry` struct
- `NewManager()`
- `NewRegistry()`
- `RegisterProvider()`
- `GetProvider()`
- `GetAllModels()`
- `GetAvailableModels()`
- `FindModel()`
- `GetModelProvider()`
- `RefreshModels()`
- `RegisterOAuthProvider()`
- `GetOAuthProvider()`
- `ListOAuthProviders()`
- `LoginOAuth()`
- `LogoutOAuth()`
- `SetAPIKey()`
- `RemoveAPIKey()`
- `GetAPIKeyForModel()`
- `getEnvVarName()`
- `IsUsingOAuth()`
- `refreshProviderModels()`

The codebase uses `ChatService` with direct provider construction instead. This is **~180 lines of dead code** that adds confusion about the intended architecture.

**What To Do:**
Remove the `Manager` struct, `Registry` struct, and all their methods from `llm/types.go`. If the abstraction is needed in the future, it can be reintroduced with a proper design.

**Why:**
- ~180 lines of dead code removed.
- Eliminates architectural ambiguity about how providers are supposed to be managed.
- New contributors won't be confused by an elaborate system that nothing uses.

**Effort:** ~30 minutes

---

### 5. Remove Additional Dead Functions and Types

**Problem:**
Scattered across the codebase are functions and types that are declared but never referenced:

| Function/Type | Location | Issue |
|---|---|---|
| `strPtr()` | `internal/llm/defaults.go:112` | Never called anywhere |
| `FormatCredits()` | `internal/llm/types.go:530` | Never called; `FormatBalance()` in `kilo.go` is used instead |
| `Time()` | `internal/llm/types.go:539` | Never called |
| `APIOpenAIResponses` | `internal/llm/types.go:18` | Declared as a constant but never referenced |
| `NewInMemoryAuthStorage()` | `internal/llm/types.go:227` | Never called |
| `GetAuthPath()` | `internal/llm/config.go:29` | Never called |
| `GetModelsPath()` | `internal/llm/config.go:34` | Never called |
| `SetInitiatorHeader()` | `internal/llm/provider.go:231` | Method exists but is never called |
| `SetAPIKey()` on `OpenAIProvider` | `internal/llm/provider.go:43` | Never called |
| `SetBaseURL()` on `OpenAIProvider` | `internal/llm/provider.go:47` | Never called |
| `BashExecutionToText()` | `internal/core/context_manager.go:339` | Never called |
| `DebugDescribe()` | `internal/core/context_manager.go:354` | Never called |
| `ReadExecutor`, `WriteExecutor`, `BashExecutor` type aliases | `internal/core/tools/types.go:93-95` | Declared as "compile-time guards" but never used for enforcement |
| `buildRuntimeMetadata()` | `internal/core/context_manager.go:193` | Always returns `""` — placeholder with no implementation |
| `ExpandSkillReferences()` | `internal/core/skills.go` | Only `ExpandSkillReferencesWithCache()` is called |

**What To Do:**
Remove all of the above. For `buildRuntimeMetadata()`, either implement it or remove the call and the method entirely.

**Why:**
- ~250+ lines of dead code removed.
- Reduced cognitive load when reading the codebase.
- Clearer picture of what the system actually does vs. what was planned but never wired up.

**Effort:** ~1 hour

---

## P2 — Medium: Performance Issues

### 6. Cache `buildContextEntries()` Results

**Location:** `internal/tui/components/codeagent.go` — `renderContextPane()`

**Problem:**
`renderContextPane()` calls `m.buildContextEntries()` which calls `m.contextManager.Build()`. This reconstructs the entire prompt including **reading project context files from disk**, loading skills, and computing **SHA-256 hashes**. This happens on every `View()` call, which in Bubbletea means **every keypress, every scroll, every mouse move, every tick timer**.

**What To Do:**
1. Add a `cachedContextEntries []contextEntry` field and a `contextEntriesDirty bool` flag to `CodeAgentModel`.
2. Set `contextEntriesDirty = true` when `conversationHistory` changes, skills change, or the session changes.
3. In `renderContextPane()`, rebuild entries only when the dirty flag is set; otherwise use the cached slice.

**Why:**
- Eliminates disk I/O and crypto operations from the render hot path.
- Smoother TUI experience, especially over SSH or slow disks.
- Context pane content only actually changes when conversation or context changes — not on every render.

**Effort:** ~1 hour

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

### 8. Replace O(n*m) LCS Diff Algorithm

**Location:** `internal/core/tools/write.go` — `computeLineDiffOps()`

**Problem:**
The function computes line diffs using a classic DP LCS algorithm with **O(n*m) memory**. While there's a guard at 2M cells, for files of 1000 lines being diffed against 1000 lines, it allocates a **4MB+ matrix** on every write operation. Larger files hit the guard and fall back to a full-replacement diff, losing useful context.

**What To Do:**
- Replace with **Myers' diff algorithm** (O(n+m) space in practice) or use a well-tested library like `github.com/sergi/go-diff`.
- Alternative: Use patience diff which produces more readable output for code changes.

**Why:**
- Better memory efficiency for large file diffs.
- Faster execution — Myers is O(nd) where d is the number of differences.
- More readable diff output for code changes.
- Removes the arbitrary 2M cell cutoff and its lossy fallback.

**Effort:** ~2 hours

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

### 10. Use Built-in `min()`/`max()`

**Locations:**
- `min()` defined in `codeagent.go:3100` and `truncate.go:179`
- `max()` defined in `codeagent.go:3107` and `read.go:138`
- `maxInt()` defined in `github_copilot.go:411`

**Problem:**
Custom `min`/`max`/`maxInt` helper functions are defined in multiple files. The `go.mod` declares `go 1.26`, which has built-in generic `min()` and `max()` functions since Go 1.21.

**What To Do:**
Delete all custom `min()`, `max()`, and `maxInt()` definitions. Use the Go builtins directly.

**Why:**
- Less code.
- No risk of shadowing or conflicting definitions.
- Standard Go idiom that every Go developer recognizes.

**Effort:** ~15 minutes

---

## P3 — Low: Code Quality & Modularity

### 11. Split `llm/types.go` into Focused Files

**Location:** `internal/llm/types.go` (541 lines)

**Problem:**
This single file contains: message types, API type constants, tool types, credential types, auth storage interfaces and implementations, provider interfaces, OAuth types, the unused Registry, and the unused Manager. It's the most imported file in the project and mixes data types with business logic.

**What To Do:**

| New File | Contents |
|---|---|
| `llm/messages.go` | `Message`, `ChatRequest`, `ChatResponse`, `StreamChunk`, `StopReason`, `Usage` |
| `llm/auth.go` | `AuthStorage`, `AuthEntry`, `Credentials`, `OAuthCredentials`, `InMemoryAuthStorage`, `FileAuthStorage` |
| `llm/model.go` | `Model`, `Cost`, `CompatConfig`, `InputModality` |
| `llm/interfaces.go` | `Provider` interface, `OAuthProvider` interface |
| `llm/tools.go` | `ToolDefinition`, `ToolCall`, `ToolResult` |

Keep `llm/types.go` only for truly shared constants (`APIType` enum).

**Why:**
- Each file has a clear, single concern.
- Finding types becomes intuitive by filename.
- Merge conflicts reduced — changes to auth types don't touch message types.

**Effort:** ~1 hour

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

### 15. Add LLM Streaming Cancellation Support

**Location:** `internal/tui/components/codeagent.go` — `startChatStream()`

**Problem:**
`startChatStream()` uses `context.Background()`. There is **no way for a user to cancel an in-flight LLM request**. If the model takes 60 seconds to respond or hangs, the user must wait or quit the entire application.

**What To Do:**
1. Add a `cancelStream context.CancelFunc` field to `CodeAgentModel`.
2. When `startChatStream()` is called, create a cancellable context: `ctx, cancel := context.WithCancel(context.Background())`.
3. Store `cancel` in `m.cancelStream`.
4. On `Ctrl+C` when `isWorking == true`, call `m.cancelStream()` instead of quitting the app.
5. Pass the cancellable context to `chatService.Stream()`.
6. Handle the `context.Canceled` error gracefully in the response handler.

**Why:**
- Users can cancel long-running requests without killing the app.
- Essential for large-context queries that may take a long time.
- Standard UX expectation for any streaming interface.

**Effort:** ~2 hours

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
