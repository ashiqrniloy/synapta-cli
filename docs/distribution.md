# Synapta distribution and release setup

This project is now set up to ship a **single compiled binary** and publish a release from `main` based on a controlled `VERSION` file.

## What was added

- `.goreleaser.yaml`
  - Builds static binaries for:
    - `linux/amd64`
    - `linux/arm64`
    - `darwin/amd64`
    - `darwin/arm64`
  - Creates `tar.gz` archives + checksums
  - Injects build metadata (`version`, `commit`, `date`)

- `.github/workflows/release.yml`
  - Triggers on every push to `main`
  - Reads `VERSION` from the repository
  - Creates tag `v<VERSION>` only if it does not already exist
  - Runs GoReleaser to publish a GitHub release with artifacts
  - If the tag already exists, it skips release creation

- `scripts/install.sh`
  - One-line install script for Linux/macOS users:
    ```bash
    curl -fsSL https://raw.githubusercontent.com/ashiqrniloy/synapta-cli/main/scripts/install.sh | sh
    ```
  - Downloads correct binary for user OS/arch and installs it to:
    - `/usr/local/bin` (if writable), otherwise
    - `~/.local/bin`

- `internal/version/version.go`
  - Build-time version metadata holder

- `internal/update/checker.go`
  - Checks latest GitHub release (with local cache)
  - Shows update message when a newer release exists

- `cmd/synapta/main.go`
  - Added `synapta version`
  - Added startup update notification

## One-time setup you must do (GitHub side)

### Ensure workflow permission allows write

In repo settings → Actions → General:

- Workflow permissions should allow **Read and write permissions**

(Required for tag + release creation with `GITHUB_TOKEN`.)

## User install/update flows

## A) Install via script (Linux/macOS)

```bash
curl -fsSL https://raw.githubusercontent.com/ashiqrniloy/synapta-cli/main/scripts/install.sh | sh
```

Update is the same command.

## Versioning flow (you control versions)

- Version source of truth: root `VERSION` file.
- Initial version is set to:

```text
0.1.0
```

### To publish a new release

1. Update `VERSION` (for example, `0.1.1`)
2. Commit and push to `main`
3. GitHub Action creates tag `v0.1.1` and publishes release artifacts

If you push again without changing `VERSION`, the workflow will skip release because that tag already exists.

## Notes

- The in-app update notice compares the running version with latest GitHub release.
- Update checks are cached for 6 hours in `~/.synapta/update-cache.json`.
