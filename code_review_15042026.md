# Code Review Report — 15/04/2026

## Scope
Review of the Synapta CLI codebase for:
1. Simplification opportunities
2. Accuracy and robustness improvements
3. Redundancies
4. Performance improvements
5. Modularity and extensibility improvements

---

## Executive Summary
The codebase has a strong foundation: clear package boundaries, good use of internal packages, defensive HTTP client defaults, append-only session persistence, and practical tool abstractions. The biggest opportunities are not in broad rewrites, but in reducing duplication between orchestration layers, tightening correctness around token budgeting and file/path handling, and extracting reusable subsystems from very large files and stateful UI code.

The highest-impact issues are:
- duplicated tool metadata/decoding logic across core and TUI,
- oversized “god files” (`internal/core/chat.go`, `internal/llm/provider.go`, `internal/core/session_store.go`, `internal/tui/components/codeagent.go`, `internal/core/tools/write.go`),
- inaccurate or overly heuristic context budgeting,
- repeated full filesystem scans for skills/context discovery,
- inconsistent context/cancellation propagation,
- limited test coverage around failure paths and edge cases.

If addressed, the outcome would be a codebase that is easier to reason about, more reliable under edge conditions, faster on large repositories/sessions, and significantly more extensible for user-defined providers, tools, skills, and UI behaviors.

---

## 1. Simplification Opportunities



---

### 1.2 Replace stringly-typed roles/states/actions with typed enums/constants
**Issue**  
Many domains rely on raw strings for important control flow: message roles, tool event types, layout mode, input mode, compaction method, session operation action/type.

Examples include:
- `/home/arniloy/synapta-cli/internal/llm/messages.go`
- `/home/arniloy/synapta-cli/internal/tui/components/codeagent.go`
- `/home/arniloy/synapta-cli/internal/core/session_store.go`

This makes invalid values easy to introduce and hard to detect.

**What should be done**  
Introduce dedicated types and constants for domain enums.

**How it should be done**  
Define typed aliases such as:
- `type MessageRole string`
- `type CompactionMethod string`
- `type InputMode string`
- `type LayoutMode string`
- `type SessionEntryType string`

Update constructors/validators to reject invalid values early.

**Outcome**  
Safer refactors, fewer hidden bugs caused by typos, and clearer self-documenting code.

---

### 1.3 Simplify configuration merging using declarative merge helpers
**Issue**  
`/home/arniloy/synapta-cli/internal/config/config.go` contains a lot of repetitive field-by-field merge logic, especially in keybinding/theme handling.

**What should be done**  
Extract generic merge helpers or table-driven merge logic.

**How it should be done**  
Use one of these patterns:
- small field-copy helpers for `string`/`float64` values,
- map-driven merge descriptors,
- or separate `Normalize()`, `ApplyDefaults()`, and `Validate()` phases.

This is preferable to long imperative merge blocks.

**Outcome**  
Shorter config code, fewer omissions when adding new fields, and lower maintenance cost.

---

### 1.4 Centralize path discovery logic
**Issue**  
Path resolution and filesystem discovery are spread across multiple packages:
- skills loading,
- extension loading,
- project context discovery,
- tool path resolution,
- config/agent dir resolution.

This creates subtly different behaviors and duplicated normalization rules.

**What should be done**  
Create a shared path/discovery utility layer.

**How it should be done**  
Add a small package or internal module for:
- resolving project-relative vs absolute paths,
- canonicalizing symlinks,
- scanning directories with ignore rules,
- safely checking “inside project root”.

Then reuse it in `core/skills.go`, `core/extensions.go`, `core/context_manager.go`, `core/tools/path.go`, and config helpers.

**Outcome**  
Consistent behavior across the toolchain and simpler filesystem-related code.

---

## 2. Accuracy and Robustness Improvements

### 2.1 Context budget logic compares against one threshold but enforces another :DONE
**Issue**  
In `/home/arniloy/synapta-cli/internal/core/context_budget.go`, `PrepareRequestSafely` checks `size.TotalTokens > maxRequestTokens`, but truncates to `effectiveMax = maxRequestTokens - reserveTokens`.

That means messages can exceed the actual usable budget without being trimmed until they cross the larger raw threshold.

**What should be done**  
Use a single, coherent enforcement threshold based on the effective request budget.

**How it should be done**  
Refactor `PrepareRequestSafely` so that:
- `usableBudget = maxRequestTokens - reserveTokens`
- compare `size.TotalTokens > usableBudget`
- enforce against `usableBudget`
- return both configured and effective budgets in diagnostics

Also guard against negative or tiny derived budgets explicitly.

**Outcome**  
More accurate request sizing, fewer model-side context overflow failures, and more predictable compaction behavior.

---

### 2.2 Token estimation is heuristic and likely provider-inaccurate
**Issue**  
Budgeting currently depends on estimates, but provider-specific token accounting can vary materially. This affects compaction thresholds, truncation, and request safety.

Relevant areas:
- `/home/arniloy/synapta-cli/internal/core/context_budget.go`
- `/home/arniloy/synapta-cli/internal/llm/tokens.go`

**What should be done**  
Move from one generic heuristic to provider/model-aware estimation with calibration.

**How it should be done**  
Introduce a tokenizer/estimator interface:
- default heuristic estimator,
- optional provider-specific estimators,
- telemetry hooks comparing estimated vs actual usage when provider returns usage stats.

Persist rolling error ratios per provider/model and use them to bias future budgeting conservatively.

**Outcome**  
More accurate context management, fewer over-budget requests, and less unnecessary truncation.

---

### 2.3 Tool result serialization swallows marshal errors
**Issue**  
In `/home/arniloy/synapta-cli/internal/core/chat.go`, tool results are marshaled with ignored errors:
- `payload, _ := json.Marshal(toolResult)`
- similar ignored marshaling in `toolResultText`

If a tool result becomes non-serializable, the conversation state can silently degrade.

**What should be done**  
Handle serialization failures explicitly and convert them into structured tool errors.

**How it should be done**  
Wrap JSON serialization in helper functions returning fallback objects like:
```json
{"error":"failed to serialize tool result","detail":"..."}
```
Log or surface the failure in `ToolEventEnd`.

**Outcome**  
No silent corruption of tool-turn history and easier debugging of unexpected tool payloads.

---

### 2.4 Duplicate tool argument decoding creates drift risk :DONE
**Issue**  
Tool argument parsing exists in more than one place. For example:
- `/home/arniloy/synapta-cli/internal/core/chat.go` (`toolEventMetadata`, `executeToolCall`)
- `/home/arniloy/synapta-cli/internal/tui/components/state.go` (tool metadata extraction)

This is a robustness issue because changes to tool inputs can easily update one path and not the other.

**What should be done**  
Create a single shared tool call parser/inspector.

**How it should be done**  
Add a helper in `internal/core/tools` or `internal/core` that accepts a tool call and returns:
- decoded input object,
- metadata (path/command),
- validation error.

Use it everywhere tool metadata is rendered or executed.

**Outcome**  
Consistent behavior across runtime execution and UI rendering, with lower regression risk.

---

### 2.5 `update.NotifyIfAvailable` performs network work in command pre-run path
**Issue**  
`/home/arniloy/synapta-cli/cmd/synapta/main.go` invokes update notification in `PersistentPreRun`. Although cached and timeout-limited, it still adds side effects and possible latency to most commands.

**What should be done**  
Decouple update checks from command startup.

**How it should be done**  
Options:
- run it asynchronously after TUI startup,
- perform it only for interactive entrypoints,
- or expose a dedicated `synapta self update-check` command and keep implicit checks opt-in.

If implicit behavior remains, pass command context instead of using `context.Background()` in the update package.

**Outcome**  
Faster, more deterministic command startup and fewer user-visible stalls.

---

### 2.6 Several HTTP/OAuth flows still use raw `io.ReadAll` patterns without size guards
**Issue**  
A number of HTTP response handlers read full bodies into memory:
- `/home/arniloy/synapta-cli/internal/llm/provider.go`
- `/home/arniloy/synapta-cli/internal/llm/kilo.go`
- `/home/arniloy/synapta-cli/internal/oauth/github_copilot.go`

This is usually fine for small payloads, but it weakens robustness against unexpected upstream behavior.

**What should be done**  
Apply bounded-body reads and shared response decoding helpers.

**How it should be done**  
Create helper functions like:
- `ReadJSONBody(resp, maxBytes, &dst)`
- `ReadErrorBody(resp, maxBytes)`

Use `io.LimitReader` consistently for non-streaming endpoints.

**Outcome**  
Reduced memory risk and more uniform HTTP error handling.

---

### 2.7 Some long-running background operations ignore caller context : DONE
**Issue**  
There are multiple `context.Background()` invocations in the TUI action layer, such as in:
- `/home/arniloy/synapta-cli/internal/tui/components/actions.go`
- `/home/arniloy/synapta-cli/internal/update/checker.go`

This weakens cancellation and shutdown behavior.

**What should be done**  
Propagate request/session lifecycle contexts through all async operations.

**How it should be done**  
Give `CodeAgentModel` a root lifecycle context and derive child contexts for:
- model fetch,
- balance checks,
- compaction,
- bash execution,
- update checks.

Avoid creating detached background contexts unless the operation is intentionally fire-and-forget.

**Outcome**  
Cleaner cancellation semantics and fewer leaked goroutines/work after the UI state changes.

---

### 2.8 Tests are too light on failure-path behavior for core infrastructure : DONE
**Issue**  
There are tests for context budget, provider normalization, session store, and write tool, but failure-path coverage appears limited relative to code complexity.

Examples needing more coverage:
- stream fallback behavior in `chat.go`,
- malformed tool call arguments,
- provider API fallback from chat/completions to responses,
- session compaction/recovery from partial or corrupt JSONL lines,
- skill/extension manifest collisions and invalid files,
- HTTP tracing and timeout behavior.

**What should be done**  
Expand tests around edge cases, not only happy paths.

**How it should be done**  
Use table-driven tests and local fake servers/filesystems for:
- partial stream chunks,
- invalid SSE events,
- stale write hash rejections,
- duplicate skill and extension names/IDs,
- path traversal and symlink behavior.

**Outcome**  
Higher confidence in behavior under real-world failures and easier refactoring.

---

## 3. Redundancies

### 3.1 Tool schema, metadata extraction, and dispatch knowledge are duplicated
**Issue**  
The system currently stores tool knowledge in multiple forms:
- schema declarations in `/home/arniloy/synapta-cli/internal/core/chat.go`
- execution switch in the same file,
- metadata extraction in both core and TUI,
- descriptions inside the tool implementations themselves.

This is a classic duplication hotspot.

**What should be done**  
Move to a registry-driven tool system where each tool supplies its schema, metadata extractor, validator, and executor.

**How it should be done**  
Define a tool interface like:
- `Name() string`
- `Description() string`
- `JSONSchema() map[string]any`
- `Decode(raw string) (any, error)`
- `Metadata(any) ToolMetadata`
- `Execute(ctx, any) (Result, error)`

Register tools centrally and generate the OpenAI-compatible tool definition list from the registry.

**Outcome**  
One source of truth for tools, easier addition of new tools, and less drift between docs/schema/runtime/UI.

---

### 3.2 Skills and project-context scanning repeat expensive directory traversal patterns
**Issue**  
`/home/arniloy/synapta-cli/internal/core/skills.go`, `skills_cache.go`, and `context_manager.go` all walk the filesystem and construct signatures independently.

**What should be done**  
Consolidate discovery and caching into a shared indexed catalog.

**How it should be done**  
Introduce a filesystem-backed catalog service that can answer:
- current skill set,
- skill body/signature,
- project context file chain,
- extension manifests.

Use a common invalidation strategy keyed by directory mtimes, file stats, or optional fsnotify.

**Outcome**  
Less duplicate logic and fewer repeated scans on each prompt build.

---

### 3.3 Provider-specific HTTP request building has overlapping flows
**Issue**  
`/home/arniloy/synapta-cli/internal/llm/provider.go` contains parallel implementations for chat/completions and responses APIs with repeated request construction and error handling.

**What should be done**  
Extract shared request/response plumbing and isolate only endpoint-specific mapping differences.

**How it should be done**  
Create shared helpers for:
- request creation,
- header setup,
- dynamic header application,
- JSON encode/decode,
- API error formatting,
- SSE event loops.

Then keep only payload mapping separate for each API flavor.

**Outcome**  
Less duplication, easier provider compatibility work, and lower bug surface.

---

### 3.4 Similar domain normalization logic appears in multiple packages
**Issue**  
There are repeated patterns of trimming, validating, lowercasing, and path/domain normalization across config, skills, OAuth, tools, and TUI.

**What should be done**  
Pull these into small shared normalization helpers.

**How it should be done**  
Examples:
- normalize domain/hostnames,
- normalize shortcut keys,
- normalize IDs/names,
- trim and validate non-empty strings.

Keep helpers intentionally small and domain-specific rather than building a generic utility dump.

**Outcome**  
Less repetitive code and more consistent validation behavior.

---

## 4. Performance Improvement Areas

### 4.1 Prompt building rescans files/directories too often
**Issue**  
`/home/arniloy/synapta-cli/internal/core/context_manager.go` rebuilds signatures using repeated file stats and file reads, while `skillsLoadSignature` can walk full trees. On large repos, this may become a recurring tax on every interaction.

**What should be done**  
Reduce prompt-build filesystem work through stronger caching and incremental invalidation.

**How it should be done**  
Use a catalog cache keyed by root dirs plus file stat snapshots. Invalidate only when:
- cwd changes,
- skill paths change,
- specific watched files change.

Consider optional `fsnotify` for local cache invalidation.

**Outcome**  
Lower latency per message, especially in large projects with many skill files.

---

### 4.2 `ListAllSessions` and session preview generation are O(n files × file read)
**Issue**  
`/home/arniloy/synapta-cli/internal/core/session_store.go` lists sessions by scanning directories and reading session files to build metadata. This can become slow with many sessions.

**What should be done**  
Introduce a lightweight session index.

**How it should be done**  
Persist a sidecar index or write metadata into a compact manifest at session creation/update time:
- session ID,
- modified time,
- cwd,
- preview of first user message,
- message count.

Fall back to full scan only for recovery or missing index entries.

**Outcome**  
Faster session search/resume UI and reduced disk I/O.

---

### 4.3 Kilo gateway constructs its own HTTP client instead of shared clients : DONE
**Issue**   
`/home/arniloy/synapta-cli/internal/llm/kilo.go` creates a dedicated `http.Client{Timeout: ...}` rather than reusing `/home/arniloy/synapta-cli/internal/httpclient/client.go`.

This bypasses shared transport tuning, tracing, and pooling strategy.

**What should be done**  
Reuse the shared HTTP client package or inject a client dependency.

**How it should be done**  
Either:
- use `httpclient.Default` for Kilo fetches,
- or accept `*http.Client` in `NewKiloGateway()` and provide a default constructor.

**Outcome**  
More consistent networking behavior and less duplicated transport configuration.

---

### 4.4 Repeated JSON marshal/unmarshal in hot paths can be reduced
**Issue**  
Tool events and message transformation repeatedly marshal/unmarshal JSON in streaming paths and UI state extraction.

Relevant files:
- `/home/arniloy/synapta-cli/internal/core/chat.go`
- `/home/arniloy/synapta-cli/internal/tui/components/state.go`

**What should be done**  
Decode once, reuse structured values, and avoid converting back and forth between JSON strings unnecessarily.

**How it should be done**  
Carry typed tool invocation metadata and typed tool result summaries alongside raw message payloads. Only serialize when actually required for provider/tool protocol boundaries.

**Outcome**  
Less CPU churn in chat loops and cleaner code structure.

---

### 4.5 UI model likely does more full-state rendering/derivation than necessary
**Issue**  
The TUI model in `/home/arniloy/synapta-cli/internal/tui/components/codeagent.go` and related files holds large amounts of derived state, with many string builders and repeated transformations. As the transcript grows, this risks degraded interaction smoothness.

**What should be done**  
Separate canonical state from derived render state and cache expensive render fragments.

**How it should be done**  
Maintain:
- canonical transcript data,
- memoized rendered blocks per message/tool event,
- incremental viewport updates rather than rebuilding large strings whenever possible.

Profile before and after if transcript size grows into the hundreds/thousands of events.

**Outcome**  
More responsive TUI and better scalability for long sessions.

---

### 4.6 `skillsLoadSignature` does full tree walks where narrower checks may suffice
**Issue**  
`/home/arniloy/synapta-cli/internal/core/skills_cache.go` computes signatures by walking directory trees and stat-ing markdown files. This is robust but potentially expensive.

**What should be done**  
Use staged cache validation.

**How it should be done**  
Validate in layers:
1. check root directory stat,
2. if unchanged, reuse cached result,
3. only fall back to deep walk on mismatch or forced invalidation.

**Outcome**  
Reduced repeated traversal cost while keeping correctness.

---

## 5. Modularity and Extensibility Improvements

### 5.1 Introduce a first-class tool plugin/registry architecture : DONE
**Issue**  
Tooling is currently effectively hardcoded to `read`, `write`, and `bash` inside `/home/arniloy/synapta-cli/internal/core/chat.go` and toolset wiring.

This limits user extensibility.

**What should be done**  
Define a pluggable tool registry that users or extensions can contribute to.

**How it should be done**  
Create a runtime registry that supports:
- built-in tools,
- extension-provided tools,
- user-configured tools from manifests.

A tool should declare schema, execution policy, capabilities, safe working directory scope, and optional streaming behavior.

**Outcome**  
The CLI becomes much more extensible without editing core orchestration code.

---

### 5.2 Make providers injectable and registry-based :DONE
**Issue**  
Provider selection is hardcoded around Kilo and GitHub Copilot in `/home/arniloy/synapta-cli/internal/core/chat.go`.

**What should be done**  
Abstract provider registration and discovery.

**How it should be done**  
Introduce a provider registry with:
- provider metadata,
- auth strategy,
- model discovery,
- chat implementation,
- refresh/token hooks.

Core chat should ask the registry for a provider by ID rather than switch on constants.

**Outcome**  
Adding a new provider becomes incremental instead of invasive.

---

### 5.3 Extract session compaction strategy into interchangeable policies
**Issue**  
Compaction is embedded directly in session store behavior. This couples persistence with one summarization policy.

**What should be done**  
Make compaction strategy pluggable.

**How it should be done**  
Define interfaces such as:
- `CompactionPlanner`
- `CompactionSummarizer`
- `CompactionApplier`

Allow policies like:
- deterministic fallback only,
- model-based only,
- hybrid by provider/model/context size,
- user-defined keep rules.

**Outcome**  
Users can tailor memory behavior without rewriting storage internals.

---

### 5.4 Decouple TUI actions from business logic services - DONE
**Issue**  
`/home/arniloy/synapta-cli/internal/tui/components/actions.go` directly knows about auth storage, chat service, session store, model loading, balance fetches, compaction, and bash execution.

This makes the TUI the orchestration hub instead of a thin client over services.

**What should be done**  
Move use-case orchestration into application services.

**How it should be done**  
Introduce service layer components like:
- `ChatController`
- `SessionService`
- `ProviderService`
- `ExtensionService`
- `WorkspaceService`

The TUI should send intents and render results, not coordinate all dependencies itself.

**Outcome**  
Cleaner architecture, easier non-TUI frontends later, and better unit-testability.

---

### 5.5 Formalize extension manifests and lifecycle hooks
**Issue**  
`/home/arniloy/synapta-cli/internal/core/extensions.go` currently supports a simple manifest with command/args/workdir, but extensibility is shallow.

**What should be done**  
Expand extensions into a clearer contract.

**How it should be done**  
Add optional manifest sections for:
- environment variables,
- permissions/capabilities,
- custom tools,
- hooks (`pre-chat`, `post-tool`, `session-open`, etc.),
- UI contributions,
- version compatibility.

Validate manifests strictly and surface diagnostics clearly.

**Outcome**  
Extensions become a proper ecosystem surface instead of only shell command launchers.

---

### 5.6 Expose skill loading as a reusable subsystem with policy hooks
**Issue**  
Skills are loaded and formatted in a relatively fixed way from default directories and optional paths.

**What should be done**  
Allow users to customize skill resolution policy and formatting.

**How it should be done**  
Support:
- custom priority order,
- include/exclude rules,
- conflict resolution policy,
- lazy vs eager body loading,
- prompt formatting strategies.

These can be driven by config and implemented behind interfaces.

**Outcome**  
More flexible skill systems for different teams and workflows.

---

### 5.7 Separate domain model from persistence DTOs in session storage : Done
**Issue**  
`sessionEntry` in `/home/arniloy/synapta-cli/internal/core/session_store.go` is both a persistence format and part of the working domain model.

**What should be done**  
Separate storage DTOs from domain entities.

**How it should be done**  
Keep a JSONL DTO layer and convert into richer internal types used by session logic. This allows schema evolution, validation, and migration support without contaminating runtime logic.

**Outcome**  
Cleaner persistence boundaries and easier future format upgrades.

---

## 6. Priority Recommendations

### High Priority
1. Fix context budget threshold inconsistency in `/home/arniloy/synapta-cli/internal/core/context_budget.go` - DONE
2. Remove duplicated tool decoding/metadata logic and replace with a shared tool registry/parser - DONE
3. Split `chat.go`, `provider.go`, `session_store.go`, and `write.go` into focused modules - DONE
4. Improve cancellation propagation by eliminating unnecessary `context.Background()` use in async flows - DONE
5. Add failure-path tests for streaming, compaction, malformed tool payloads, and session corruption - DONE

### Medium Priority
1. Add provider/model-aware token estimation and calibration
2. Introduce session indexing for faster listing/search
3. Consolidate filesystem scanning/caching for skills, extensions, and project context
4. Reuse shared HTTP clients in Kilo and other provider paths
5. Decouple TUI action orchestration into service layer components

### Lower Priority but Valuable
1. Typed enums/constants for roles and states
2. Config merge simplification
3. Expanded extension manifest capabilities
4. Render-state memoization in the TUI
5. Persistence DTO/domain separation in session store

---

## 7. Expected Overall Outcome
If the recommendations above are implemented, the codebase should gain:
- **simpler maintenance** through smaller modules and less duplicated logic,
- **higher robustness** through better budget enforcement, error handling, and cancellation,
- **better performance** in large projects and long sessions,
- **better extensibility** via registries for tools, providers, compaction policies, and extensions,
- **greater confidence** through stronger failure-path test coverage.

In short: the codebase is already promising, but it is at the point where architecture consolidation will deliver disproportionately large gains in reliability, velocity, and extensibility.
