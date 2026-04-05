package main

import (
	"fmt"
	"os"

	tea "charm.land/bubbletea/v2"
	"github.com/spf13/cobra"

	"github.com/ashiqrniloy/synapta-cli/internal/config"
	"github.com/ashiqrniloy/synapta-cli/internal/tui/components"
	"github.com/ashiqrniloy/synapta-cli/internal/update"
	"github.com/ashiqrniloy/synapta-cli/internal/version"
)

func main() {
	if err := rootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}

// ─── Root command ─────────────────────────────────────────────────────

func rootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "synapta",
		Short: "Synapta — Agentic AI Development Framework",
		Long: `Synapta is an agentic AI-driven application development framework.

Run without arguments to enter the interactive launcher, or use a
subcommand such as "synapta code" to launch the coding agent directly.`,
		RunE: runLauncher,
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			if cmd.Name() == "version" {
				return
			}
			update.NotifyIfAvailable(version.Version, os.Stderr)
		},
	}
	cmd.Version = version.Version
	cmd.SetVersionTemplate("{{printf \"synapta %s\\n\" .Version}}")
	cmd.AddCommand(codeCmd(), versionCmd())
	return cmd
}

func runLauncher(cmd *cobra.Command, args []string) error {
	lm := newLauncherModel()
	p := tea.NewProgram(lm)
	model, err := p.Run()
	if err != nil {
		return fmt.Errorf("launcher failed: %w", err)
	}

	if lm, ok := model.(*launcherModel); ok && lm.selected >= 0 {
		switch lm.selected {
		case 0: // Synapta Code
			if err2 := runCodeAgent(); err2 != nil {
				fmt.Fprintln(os.Stderr, err2)
			}
		}
	}
	return nil
}

// ─── Code subcommand ──────────────────────────────────────────────────

func codeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "code",
		Short: "Launch Synapta Code — the AI coding agent",
		Long: `Launches the Synapta Code interactive coding agent.

The agent provides a terminal UI where you can describe tasks,
view agent output, and collaborate on code development.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCodeAgent()
		},
	}
}

func runCodeAgent() error {
	cfg, err := config.LoadConfig()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	model := components.NewCodeAgentModel(cfg)
	p := tea.NewProgram(model)
	if _, err := p.Run(); err != nil {
		return fmt.Errorf("code agent error: %w", err)
	}
	return nil
}

func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print Synapta version information",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Fprintf(cmd.OutOrStdout(), "synapta %s\n", version.Version)
			fmt.Fprintf(cmd.OutOrStdout(), "commit: %s\n", version.Commit)
			fmt.Fprintf(cmd.OutOrStdout(), "built:  %s\n", version.Date)
		},
	}
}

// ─── Launcher model ───────────────────────────────────────────────────

var launcherOptions = []string{
	"Synapta Code  — AI Coding Agent",
}

type launcherModel struct {
	cursor   int
	selected int // -1 = nothing yet
	quit     bool
}

func newLauncherModel() *launcherModel {
	return &launcherModel{selected: -1}
}

func (m *launcherModel) Init() tea.Cmd { return nil }

func (m *launcherModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "ctrl+c", "q", "esc":
			m.quit = true
			return m, tea.Quit
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(launcherOptions)-1 {
				m.cursor++
			}
		case "enter":
			m.selected = m.cursor
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m *launcherModel) View() tea.View {
	if m.quit {
		return tea.NewView("")
	}
	s := "\n"
	s += "  ╔═══════════════════════════════════╗\n"
	s += "  ║       Welcome to Synapta          ║\n"
	s += "  ║     Choose an agent to launch     ║\n"
	s += "  ╚═══════════════════════════════════╝\n\n"
	for i, opt := range launcherOptions {
		prefix := "  "
		if i == m.cursor {
			prefix = "▸ "
			opt = "> " + opt
		}
		s += fmt.Sprintf("%s  %s\n", prefix, opt)
	}
	s += "\n  ↑/↓ navigate   enter select   q/esc quit\n"
	return tea.NewView(s)
}
