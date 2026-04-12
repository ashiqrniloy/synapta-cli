package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/viper"
	go_yaml "go.yaml.in/yaml/v3"
)

// Keybindings defines named keybinding mappings.
//
// Naming convention: lowercase snake_case, action-oriented
// (e.g., newline, submit, quit).
//
// Key format: modifier+key (e.g. shift+enter, ctrl+c, alt+q).
// Supported modifiers: ctrl, alt, shift.
type Keybindings struct {
	Newline     string `mapstructure:"newline"`
	Submit      string `mapstructure:"submit"`
	Quit        string `mapstructure:"quit"`
	Stop        string `mapstructure:"stop"`
	Command     string `mapstructure:"command"`
	Context     string `mapstructure:"context"`
	FileBrowser string `mapstructure:"file_browser"`
	Help        string `mapstructure:"help"`
	Extensions  string `mapstructure:"extensions"`
}

// Theme defines a complete colour palette for a named theme.
type Theme struct {
	Name                        string  `mapstructure:"name"`
	Background                  string  `mapstructure:"background"`
	Foreground                  string  `mapstructure:"foreground"`
	Primary                     string  `mapstructure:"primary"`
	Secondary                   string  `mapstructure:"secondary"`
	Accent                      string  `mapstructure:"accent"`
	Muted                       string  `mapstructure:"muted"`
	Border                      string  `mapstructure:"border"`
	Selection                   string  `mapstructure:"selection"`
	Error                       string  `mapstructure:"error"`
	Success                     string  `mapstructure:"success"`
	CursorFG                    string  `mapstructure:"cursor_fg"`
	CursorBG                    string  `mapstructure:"cursor_bg"`
	HighlightColor              string  `mapstructure:"highlight_color"`
	HighlightOpacity            float64 `mapstructure:"highlight_opacity"`
	InteractionHighlightColor   string  `mapstructure:"interaction_highlight_color"`
	InteractionHighlightOpacity float64 `mapstructure:"interaction_highlight_opacity"`
	SystemMessageColor          string  `mapstructure:"system_message_color"`
	SystemMessageOpacity        float64 `mapstructure:"system_message_opacity"`
}

func defaultTheme() Theme {
	return Theme{
		Name:                        "Nord Aurora Dark",
		Background:                  "#1f2430",
		Foreground:                  "#d8dee9",
		Primary:                     "#88c0d0",
		Secondary:                   "#a3be8c",
		Accent:                      "#b48ead",
		Muted:                       "#6b7488",
		Border:                      "#3b4252",
		Selection:                   "#2b303b",
		Error:                       "#bf616a",
		Success:                     "#a3be8c",
		CursorFG:                    "#1f2430",
		CursorBG:                    "#88c0d0",
		HighlightColor:              "#88c0d0",
		HighlightOpacity:            0.24,
		InteractionHighlightColor:   "#b48ead",
		InteractionHighlightOpacity: 0.14,
		SystemMessageColor:          "#81a1c1",
		SystemMessageOpacity:        0.14,
	}
}

func gruvboxMaterialTheme() Theme {
	return Theme{
		Name:                        "Gruvbox Material Dark",
		Background:                  "#282828",
		Foreground:                  "#d4be98",
		Primary:                     "#a9b665",
		Secondary:                   "#89b482",
		Accent:                      "#d8a657",
		Muted:                       "#7c6f64",
		Border:                      "#504945",
		Selection:                   "#3c3836",
		Error:                       "#ea6962",
		Success:                     "#a9b665",
		CursorFG:                    "#282828",
		CursorBG:                    "#a9b665",
		HighlightColor:              "#a9b665",
		HighlightOpacity:            0.2,
		InteractionHighlightColor:   "#d8a657",
		InteractionHighlightOpacity: 0.15,
		SystemMessageColor:          "#89b482",
		SystemMessageOpacity:        0.22,
	}
}

// RawThemes is the intermediate structure for YAML unmarshalling.
type RawThemes struct {
	Default string           `mapstructure:"default"`
	Map     map[string]Theme `mapstructure:",remain"`
}

// ProviderConfig stores the active provider and model selection.
type ProviderConfig struct {
	Default string `mapstructure:"default"` // Provider ID (e.g., "github-copilot", "kilo")
	Model   string `mapstructure:"model"`   // Model ID (e.g., "gpt-4o", "claude-sonnet-4-20250514")
}

type UIConfig struct {
	Density string `mapstructure:"density"` // compact | comfortable
}

// AppConfig is the top-level configuration structure.
type AppConfig struct {
	Keybindings      Keybindings       `mapstructure:"keybindings"`
	CommandShortcuts map[string]string `mapstructure:"command_shortcuts"`
	Themes           RawThemes         `mapstructure:"themes"`
	Provider         ProviderConfig    `mapstructure:"provider"`
	UI               UIConfig          `mapstructure:"ui"`
}

// ActiveTheme returns the currently selected Theme, falling back to the
// built-in default theme.
func (c *AppConfig) ActiveTheme() Theme {
	key := c.Themes.Default
	if key == "" {
		return defaultTheme()
	}
	t, ok := c.Themes.Map[key]
	if !ok {
		return defaultTheme()
	}
	if t.Name == "" {
		t.Name = key
	}
	return t
}

// ── Defaults ──────────────────────────────────────────────────────────
func defaultKeybindings() Keybindings {
	return Keybindings{
		Newline:     "shift+enter",
		Submit:      "enter",
		Quit:        "ctrl+c",
		Stop:        "ctrl+q",
		Command:     "ctrl+p",
		Context:     "ctrl+k",
		FileBrowser: "ctrl+f",
		Help:        "ctrl+j",
		Extensions:  "ctrl+e",
	}
}

func defaultThemesMap() map[string]Theme {
	return map[string]Theme{
		"nord-aurora-dark":      defaultTheme(),
		"gruvbox-material-dark": gruvboxMaterialTheme(),
	}
}

func defaultCommandShortcuts() map[string]string {
	return map[string]string{
		"q": "quit",
		"b": "bash",
		"f": "browse-files",
		"h": "help",
		"k": "context-manager",
		"m": "set-model",
		"n": "new-session",
		"c": "compact",
		"r": "resume-session",
	}
}

// DefaultConfig returns a fully-populated config with built-in defaults.
func DefaultConfig() *AppConfig {
	return &AppConfig{
		Keybindings:      defaultKeybindings(),
		CommandShortcuts: defaultCommandShortcuts(),
		Themes: RawThemes{
			Default: "nord-aurora-dark",
			Map:     defaultThemesMap(),
		},
		UI: UIConfig{Density: "comfortable"},
	}
}

// ── Loading ───────────────────────────────────────────────────────────

func normalizeShortcutKey(key string) string {
	k := strings.ToLower(strings.TrimSpace(key))
	k = strings.TrimPrefix(k, ":")
	return strings.TrimSpace(k)
}

// LoadConfig reads YAML config from the user directory (~/.synapta/config.yaml),
// falling through to the local config/ directory, then to hard-coded defaults.
func LoadConfig() (*AppConfig, error) {
	v := viper.New()
	v.SetConfigName("config")
	v.SetConfigType("yaml")

	homeDir, err := os.UserHomeDir()
	if err == nil {
		v.AddConfigPath(filepath.Join(homeDir, ".synapta"))
	}
	v.AddConfigPath("./config")
	v.AddConfigPath("../config")

	// Attempt to read — if not found, return defaults
	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); ok {
			return DefaultConfig(), nil
		}
		return nil, fmt.Errorf("reading config: %w", err)
	}

	cfg := DefaultConfig()
	// Keybindings
	if kb := v.GetStringMapString("keybindings"); len(kb) > 0 {
		if v, ok := kb["newline"]; ok && v != "" {
			cfg.Keybindings.Newline = v
		}
		if v, ok := kb["submit"]; ok && v != "" {
			cfg.Keybindings.Submit = v
		}
		if v, ok := kb["quit"]; ok && v != "" {
			cfg.Keybindings.Quit = v
		}
		if v, ok := kb["stop"]; ok && v != "" {
			cfg.Keybindings.Stop = v
		}
		if v, ok := kb["command"]; ok && v != "" {
			cfg.Keybindings.Command = v
		}
		if v, ok := kb["context"]; ok && v != "" {
			cfg.Keybindings.Context = v
		}
		if v, ok := kb["file_browser"]; ok && v != "" {
			cfg.Keybindings.FileBrowser = v
		}
		if v, ok := kb["help"]; ok && v != "" {
			cfg.Keybindings.Help = v
		}
		if v, ok := kb["extensions"]; ok && v != "" {
			cfg.Keybindings.Extensions = v
		}
	}

	if shortcuts := v.GetStringMapString("command_shortcuts"); len(shortcuts) > 0 {
		for rawKey, commandID := range shortcuts {
			key := normalizeShortcutKey(rawKey)
			id := strings.TrimSpace(commandID)
			if key == "" || id == "" {
				continue
			}
			cfg.CommandShortcuts[key] = id
		}
	}

	// Provider config
	if p := v.GetString("provider.default"); p != "" {
		cfg.Provider.Default = p
	}
	if m := v.GetString("provider.model"); m != "" {
		cfg.Provider.Model = m
	}

	// UI config
	if d := strings.ToLower(strings.TrimSpace(v.GetString("ui.density"))); d != "" {
		if d == "compact" || d == "comfortable" {
			cfg.UI.Density = d
		}
	}

	// Active theme key
	if dk := v.GetString("themes.default"); dk != "" {
		cfg.Themes.Default = dk
	}

	// Theme palettes (walk viper keys that look like themes.<name>)
	for _, k := range v.AllKeys() {
		const prefix = "themes."
		if strings.HasPrefix(k, prefix) && k != "themes.default" {
			name := strings.TrimPrefix(k, prefix)
			var t Theme
			if err := v.UnmarshalKey("themes."+name, &t); err == nil {
				if cfg.Themes.Map == nil {
					cfg.Themes.Map = make(map[string]Theme)
				}
				cfg.Themes.Map[name] = t
			}
		}
	}

	return cfg, nil
}

// ── Key helpers ───────────────────────────────────────────────────────

// NewlineKey returns the key part and whether shift is used.
func (k *Keybindings) NewlineKey() (key string, shift bool) {
	return parseBinding(k.Newline)
}

// SubmitKey returns the submit key name.
func (k *Keybindings) SubmitKey() string {
	key, _ := parseBinding(k.Submit)
	return key
}

// QuitKey returns the quit key name (the key portion, not the modifier).
func (k *Keybindings) QuitKey() string {
	p := strings.Split(k.Quit, "+")
	if len(p) == 2 {
		return p[1]
	}
	return k.Quit
}

func parseBinding(s string) (key string, shift bool) {
	parts := strings.Split(strings.ToLower(s), "+")
	switch len(parts) {
	case 2:
		return parts[1], parts[0] == "shift"
	default:
		return s, false
	}
}

// SaveConfig writes the current config to ~/.synapta/config.yaml.
func SaveConfig(cfg *AppConfig) error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("getting home dir: %w", err)
	}

	configDir := filepath.Join(homeDir, ".synapta")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return fmt.Errorf("creating config dir: %w", err)
	}

	configPath := filepath.Join(configDir, "config.yaml")

	// Read existing config if it exists
	data, err := os.ReadFile(configPath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("reading existing config: %w", err)
	}

	// If config exists, update provider section only
	if len(data) > 0 {
		var existing map[string]any
		if err := go_yaml.Unmarshal(data, &existing); err != nil {
			existing = make(map[string]any)
		}

		// Update provider section
		existing["provider"] = map[string]any{
			"default": cfg.Provider.Default,
			"model":   cfg.Provider.Model,
		}

		data, err = go_yaml.Marshal(existing)
		if err != nil {
			return fmt.Errorf("marshaling config: %w", err)
		}
	} else {
		// Create new config
		data, err = go_yaml.Marshal(cfg)
		if err != nil {
			return fmt.Errorf("marshaling config: %w", err)
		}
	}

	return os.WriteFile(configPath, data, 0644)
}

// SetProvider updates the active provider and model, then saves to disk.
func (c *AppConfig) SetProvider(providerID, modelID string) error {
	c.Provider.Default = providerID
	c.Provider.Model = modelID
	return SaveConfig(c)
}
