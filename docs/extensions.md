# Synapta Extensions

Synapta Code supports **extension modules** loaded from disk, so you can add new capabilities without editing core TUI code.

## Overview

Extensions are discovered from:

- `./extensions/*/extension.json` (project-local)
- `~/.synapta/extensions/*/extension.json` (global)

Each extension appears in the command palette and can be opened with the extension shortcut.

## Launching Extensions

Inside Synapta Code:

- Press **Ctrl+E** (default) to open extension launcher
- or open command palette (**Ctrl+P**) and choose an extension entry

You can change the extension keybind in config:

```yaml
keybindings:
  extensions: "ctrl+e"
```

## Extension Manifest

Create `extension.json` in your extension directory:

```json
{
  "id": "my-extension",
  "name": "My Extension",
  "description": "Run my custom workflow",
  "command": "bash",
  "args": ["-lc", "echo hello from extension; read -n 1"],
  "workdir": "."
}
```

### Fields

- `id` (optional): unique ID. If omitted, directory name is used.
- `name` (optional): display name. Defaults to `id`.
- `description` (optional): shown in command picker.
- `command` (**required**): executable to launch.
- `args` (optional): command arguments.
- `workdir` (optional): working directory. Relative paths are resolved against extension directory.

## Extension Tool Manifests

Extensions can also contribute tools to the runtime tool registry.

Supported files inside an extension:

- `tool.json`
- `tools/*.json`

These follow the custom tool manifest documented in [`docs/tools.md`](./tools.md).

Example `extensions/my-extension/tool.json`:

```json
{
  "name": "my_extension_tool",
  "description": "Tool exposed by my extension",
  "parameters": {
    "type": "object",
    "properties": {
      "input": { "type": "string" }
    }
  },
  "command": "bash",
  "args": ["-lc", "cat"],
  "streaming": false
}
```

## Example: Local Extension

```bash
mkdir -p extensions/quick-notes
cat > extensions/quick-notes/extension.json <<'JSON'
{
  "id": "quick-notes",
  "name": "Quick Notes",
  "description": "Open a scratch note in nano",
  "command": "nano",
  "args": ["notes.txt"],
  "workdir": "."
}
JSON
```

Restart Synapta Code and launch it with **Ctrl+E**.

## Behavior Notes

- Extensions run as external processes from inside Synapta Code.
- Synapta UI pauses while the extension process is active and resumes after exit.
- Invalid manifests are skipped and surfaced in system messages.
- Duplicate extension IDs are ignored after the first match.

## Best Practices

- Keep extension IDs stable and unique.
- Prefer project-local extensions for repo-specific workflows.
- Use global extensions (`~/.synapta/extensions`) for reusable personal tools.
- Add a README in each extension directory to document expected dependencies.
