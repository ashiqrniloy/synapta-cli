# Tool Registry and Custom Tools

Synapta now supports a **runtime tool registry** so tools are no longer hardcoded to only `read`, `write`, and `bash`.

At runtime, Synapta loads tools from three sources:

1. **Built-in tools** (`read`, `write`, `bash`)
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

## Tool manifest format

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
- `workdir`: working directory (relative paths resolve from the manifest directory).
- `policy`: execution metadata.
  - `timeout_seconds`: hard timeout for command execution.
  - `require_confirmation`: policy hint (for governance/UX hooks).
  - `allow_network`: policy hint.
- `capabilities`: free-form capability tags.
- `safe_working_directory_scope`: sandbox metadata for governance.
- `streaming`: if true, stdout/stderr lines are streamed as incremental tool updates.

---

## Execution contract

For manifest-backed tools:

- Synapta passes tool-call JSON arguments to the process **via stdin**.
- `stdout`/`stderr` are returned as tool output.
- If `streaming=true`, line updates are emitted while the command runs.
- Non-zero exit status returns an error to the model (with captured output included).

---

## Minimal example (project-local)

Create `./tools/date.json`:

```json
{
  "name": "now",
  "description": "Return current date/time",
  "parameters": { "type": "object", "properties": {} },
  "command": "bash",
  "args": ["-lc", "date"]
}
```

Restart Synapta and the `now` tool will be available to tool-calling models.

---

## Notes

- Built-ins still work exactly as before.
- Invalid manifests are skipped and surfaced as runtime warnings.
- Extension command manifests (`extension.json`) are separate from extension tool manifests (`tool.json`, `tools/*.json`).
