# Synapta — Agentic AI Development Framework

## Project Overview

**Synapta** is an agentic AI-driven application development framework. The core differentiator is the use of **Temporal** to provide deterministic, observable, and replayable agent workflows. This framework follows a philosophy similar to the Pi agent framework: build primitives that are extensible, composable, and configurable.

## Architecture

The project is structured as a modular Go framework with a shared core and pluggable agents:

```
synapta-cli/
├── cmd/              # CLI entrypoints built with Cobra
├── internal/
│   ├── core/         # Shared framework primitives (provider abstractions, config, logging)
│   ├── config/       # Configuration loading via Viper
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

## Getting Started

```bash
go build -o synapta ./cmd/synapta
./synapta          # Shows the launcher to pick an agent
./synapta code     # Launches Synapta Code directly
```

## Configuration

User configuration lives in `~/.synapta/config.yaml`. See `config/config.yaml` for the reference configuration. Key areas:

- **Keybindings** — Named keybinding definitions (e.g., `newline: shift+enter`)
- **Themes** — Named color themes with full palette customization
