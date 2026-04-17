# Tool Registry and Custom Tools

Synapta uses a **runtime tool registry** so tools are not hardcoded in chat execution.

At runtime, Synapta loads tools from three sources:

1. **Built-in tools** (`read`, `write`, `shell`)
2. **Extension-provided tools** (from extension directories)
3. **User manifests** (JSON files you add)

---

## How tool loading works

When chat starts, Synapta builds a registry and loads tools from:

- Built-ins (always)
- Global/user tools:
  - `~/.synapta/tools/*.json`
- Project-local tools:
  - `./.synapta/tools/*.json`
  - `./tools/*.json`
- Extension tool manifests:
  - `~/.synapta/extensions/<ext>/tool.json`
  - `~/.synapta/extensions/<ext>/tools/*.json`
  - `./extensions/<ext>/tool.json`
  - `./extensions/<ext>/tools/*.json`

If multiple manifests reuse the same tool name, the later loaded one replaces the earlier registration.

---

## Built-in registry contract (for Go tools)

Built-ins are registered in:

- `/home/arn/Projects/synapta-cli/internal/core/tool_registry.go`

Each registered tool now provides a **single registry record** with:

- `Name`
- `Description`
- `Parameters` (JSON schema)
- `Decoder(raw string) (any, error)`
- `Metadata(decoded any) tools.ToolMetadata`
- `Executor(ctx, decoded any, onUpdate)`

This is the runtime source of truth used for:

- OpenAI-compatible tool definitions
- argument decode/validation
- metadata extraction for stream/UI (`path`, `command`)
- execution

### Current built-ins

- `read`
- `write`
- `shell`

Schemas for built-ins live in:

- `/home/arn/Projects/synapta-cli/internal/core/tools/schema.go`

---

## Adding a new built-in tool (step-by-step)

1. **Implement tool logic** in `internal/core/tools/`.
   - expose `Name()` and `Description()`
   - add your input struct
   - add `Execute(...)`

2. **Add schema function** in:
   - `/home/arn/Projects/synapta-cli/internal/core/tools/schema.go`

3. **Wire it in ToolSet** (if needed) in:
   - `/home/arn/Projects/synapta-cli/internal/core/tools/types.go`

4. **Register in registry** in:
   - `/home/arn/Projects/synapta-cli/internal/core/tool_registry.go`

   Add one `ToolSpec` with:
   - `Name`, `Description`, `Parameters`
   - `Decoder`: unmarshal raw JSON args into typed input
   - `Metadata`: extract `path` and/or `command` if applicable
   - `Executor`: type assert decoded input and run the tool

5. **Run tests**
   - `go test ./...`

### Minimal registration pattern

```go
if toolset.MyTool != nil {
    if err := r.Register(ToolSpec{
        Name:        toolset.MyTool.Name(),
        Description: toolset.MyTool.Description(),
        Parameters:  tools.MyToolJSONSchema(),
        Source:      ToolSourceBuiltin,
        Decoder: func(raw string) (any, error) {
            if strings.TrimSpace(raw) == "" {
                raw = "{}"
            }
            var in tools.MyToolInput
            if err := json.Unmarshal([]byte(raw), &in); err != nil {
                return nil, fmt.Errorf("invalid my_tool arguments: %w", err)
            }
            return in, nil
        },
        Metadata: func(decoded any) tools.ToolMetadata {
            in, ok := decoded.(tools.MyToolInput)
            if !ok {
                return tools.ToolMetadata{}
            }
            return tools.ToolMetadata{Path: strings.TrimSpace(in.Path)}
        },
        Executor: func(ctx context.Context, input any, onUpdate tools.StreamUpdate) (any, error) {
            in, ok := input.(tools.MyToolInput)
            if !ok {
                return nil, fmt.Errorf("invalid my_tool arguments: expected MyToolInput")
            }
            return toolset.MyTool.Execute(ctx, in)
        },
    }); err != nil {
        return err
    }
}
```

---

## Manifest tool format (external tools)

Create a JSON file like:

```json
{
  "name": "echo_payload",
  "description": "Echoes JSON args from stdin",
  "parameters": {
    "type": "object",
    "properties": {
      "message": { "type": "string" }
    },
    "required": ["message"]
  },
  "command": "bash",
  "args": ["-lc", "cat"],
  "workdir": ".",
  "policy": {
    "timeout_seconds": 15,
    "require_confirmation": false,
    "allow_network": false
  },
  "capabilities": ["process", "io"],
  "safe_working_directory_scope": {
    "mode": "workspace",
    "paths": []
  },
  "streaming": false
}
```

### Fields

- `name` (**required**): tool name exposed to the model.
- `description`: model-facing behavior description.
- `parameters`: JSON schema for tool arguments.
- `command` (**required**): executable to run.
- `args`: command arguments.
- `workdir`: working directory (relative paths resolve from manifest directory).
- `policy`: execution metadata.
  - `timeout_seconds`: hard timeout for command execution.
  - `require_confirmation`: policy hint.
  - `allow_network`: policy hint.
- `capabilities`: free-form tags.
- `safe_working_directory_scope`: governance metadata.
- `streaming`: if true, stdout/stderr lines are streamed as incremental updates.

---

## Manifest execution contract

For manifest-backed tools:

- Synapta passes tool-call JSON arguments via **stdin**.
- `stdout`/`stderr` are returned as tool output.
- If `streaming=true`, line updates are emitted while the command runs.
- Non-zero exit status returns an error to the model (with captured output).

---

## Notes

- Built-ins still work exactly as before (with `bash` renamed to `shell`).
- Invalid manifests are skipped and surfaced as runtime warnings.
- Extension command manifests (`extension.json`) are separate from tool manifests (`tool.json`, `tools/*.json`).
