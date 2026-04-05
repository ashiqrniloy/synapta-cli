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
  - Generates Homebrew formula (tap-based)

- `.github/workflows/release.yml`
  - Triggers on every push to `main`
  - Reads `VERSION` from the repository
  - Creates tag `v<VERSION>` only if it does not already exist
  - Runs GoReleaser to publish a GitHub release with artifacts
  - If the tag already exists, it skips release creation

- `scripts/install.sh`
  - One-line install script for Linux/macOS users:
    ```bash
    curl -fsSL https://raw.githubusercontent.com/synapta/synapta-cli/main/scripts/install.sh | sh
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

### 1) Create a Homebrew tap repository

Create this repository in GitHub:

- `synapta/homebrew-tap` (or your preferred org/user)

Then keep it empty (GoReleaser will commit formula files there).

> If you use a different tap repo, update `.goreleaser.yaml` under `brews.repository`.

### 2) Add GitHub Actions secret for Homebrew tap writes

In `synapta-cli` repo settings → **Secrets and variables** → **Actions**, add:

- `HOMEBREW_TAP_GITHUB_TOKEN`

Use a PAT (classic or fine-grained) that can push to your tap repo (`contents: write`).

### 3) Ensure workflow permission allows write

In repo settings → Actions → General:

- Workflow permissions should allow **Read and write permissions**

(Required for tag + release creation with `GITHUB_TOKEN`.)

## User install/update flows

## A) Install via script (Linux/macOS)

```bash
curl -fsSL https://raw.githubusercontent.com/synapta/synapta-cli/main/scripts/install.sh | sh
```

Update is the same command.

## B) Install via Homebrew

```bash
brew tap synapta/tap https://github.com/synapta/homebrew-tap
brew install synapta
```

Update:

```bash
brew update
brew upgrade synapta
```

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
