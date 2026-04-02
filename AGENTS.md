# Synapta — Agentic AI Development Framework

## Project Overview

**Synapta** is an agentic AI-driven application development framework. The core differentiator is the use of **Temporal** to provide deterministic, observable, and replayable agent workflows. This framework follows a philosophy similar to the Pi agent framework: build primitives that are extensible, composable, and configurable.

## Architecture

The project is structured as a modular Go framework with a shared core and pluggable agents:

```
synapta-cli/
├── cmd/              # CLI entrypoints built with Cobra
├── internal/
│   ├── core/         # Shared framework primitives
│   ├── config/       # Configuration loading via Viper
│   ├── llm/          # LLM provider layer (models, auth, registry)
│   ├── oauth/        # OAuth providers (GitHub Copilot, Kilo Gateway)
│   └── tui/          # Shared Bubbletea UI components and theme engine
├── config/           # User-facing configuration files
└── AGENTS.md         # Project context for AI agents
```

## Technology Stack

- **CLI**: Cobra (command structure) + Viper (configuration)
- **TUI**: Bubbletea (interactive terminal UI), Bubbles (UI components), Lipgloss (styling/theming)
- **Orchestration**: Temporal (deterministic agent workflows — upcoming)
- **Language**: Go 1.26+

## Current Components

- **Synapta Code** — The coding agent. Launched via `synapta code`. Provides an interactive TUI for AI-assisted code development driven by deterministic Temporal workflows (workflows coming after TUI foundation).

## LLM Provider Layer (`internal/llm/`)

The LLM layer provides a unified interface for connecting to different AI providers:

### Core Types (`types.go`)
- `Model` — Represents an LLM with capabilities (context window, pricing, etc.)
- `Provider` — Interface for LLM providers (Chat, ChatStream, HasAuth)
- `OAuthCredentials` / `APICredentials` — Authentication credentials
- `AuthStorage` — Manages credential persistence (`~/.synapta/auth.json`)
- `Registry` — Model registry for provider/model discovery
- `Manager` — Orchestrates providers, auth, and model registry

### Provider Implementations (`provider.go`)
- `OpenAIProvider` — Generic OpenAI-compatible API
- `GitHubCopilotProvider` — GitHub Copilot with special headers
- `KiloProvider` — Kilo Gateway with special headers

### OAuth Providers (`internal/oauth/`)
- `GitHubCopilotOAuth` — Device code flow for GitHub Copilot
  - Supports enterprise domains
  - Automatic token refresh
  - Model enablement after login
- `KiloOAuth` — Device code flow for Kilo Gateway
  - 300+ models via OpenRouter-compatible API
  - Dynamic model fetching
  - Credit balance display

### Authentication Flow
1. User runs `/login github-copilot` or `/login kilo`
2. OAuth provider initiates device code flow
3. Browser opens for user authorization
4. Polling for approval, token exchange
5. Credentials stored in `~/.synapta/auth.json`
6. Provider models refreshed with new credentials

## Getting Started

```bash
go build -o synapta ./cmd/synapta
./synapta          # Shows the launcher to pick an agent
./synapta code     # Launches Synapta Code directly
```

## Configuration

User configuration lives in `~/.synapta/config.yaml`. See `config/config.yaml` for the reference configuration.

### Key Areas
- **Keybindings** — Named keybinding definitions (e.g., `newline: shift+enter`)
- **Themes** — Named color themes with full palette customization
- **Providers** — LLM provider configuration (API keys, base URLs)

### Auth Storage (`~/.synapta/auth.json`)
Credentials are stored with restricted permissions (0600):
```json
{
  "github-copilot": {
    "type": "oauth",
    "oauth": {
      "refresh": "...",
      "access": "...",
      "expires": 1735000000000
    }
  },
  "anthropic": {
    "type": "api",
    "api": {
      "apiKey": "sk-..."
    }
  }
}
```
