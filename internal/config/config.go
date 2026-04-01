package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/viper"
)

// Keybindings defines named keybinding mappings.
//
// Naming convention: lowercase snake_case, action-oriented
// (e.g., newline, submit, quit).
//
// Key format: modifier+key (e.g. shift+enter, ctrl+c, alt+q).
// Supported modifiers: ctrl, alt, shift.
type Keybindings struct {
	Newline string `mapstructure:"newline"`
	Submit  string `mapstructure:"submit"`
	Quit    string `mapstructure:"quit"`
}

// Theme defines a complete colour palette for a named theme.
type Theme struct {
	Name       string `mapstructure:"name"`
	Background string `mapstructure:"background"`
	Foreground string `mapstructure:"foreground"`
	Primary    string `mapstructure:"primary"`
	Secondary  string `mapstructure:"secondary"`
	Accent     string `mapstructure:"accent"`
	Muted      string `mapstructure:"muted"`
	Border     string `mapstructure:"border"`
	Selection  string `mapstructure:"selection"`
	Error      string `mapstructure:"error"`
	Success    string `mapstructure:"success"`
	CursorFG   string `mapstructure:"cursor_fg"`
	CursorBG   string `mapstructure:"cursor_bg"`
}

func defaultTheme() Theme {
	return Theme{
		Name:       "Gruvbox Material Dark",
		Background: "#282828",
		Foreground: "#d4be98",
		Primary:    "#a9b665",
		Secondary:  "#89b482",
		Accent:     "#d8a657",
		Muted:      "#7c6f64",
		Border:     "#504945",
		Selection:  "#3c3836",
		Error:      "#ea6962",
		Success:    "#a9b665",
		CursorFG:   "#282828",
		CursorBG:   "#a9b665",
	}
}

// RawThemes is the intermediate structure for YAML unmarshalling.
type RawThemes struct {
	Default string           `mapstructure:"default"`
	Map     map[string]Theme `mapstructure:",remain"`
}

// AppConfig is the top-level configuration structure.
type AppConfig struct {
	Keybindings Keybindings `mapstructure:"keybindings"`
	Themes      RawThemes   `mapstructure:"themes"`
}

// ActiveTheme returns the currently selected Theme, falling back to the
// built-in Gruvbox Material Dark default.
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
		Newline: "shift+enter",
		Submit:  "enter",
		Quit:    "ctrl+c",
	}
}

func defaultThemesMap() map[string]Theme {
	return map[string]Theme{
		"gruvbox-material-dark": defaultTheme(),
	}
}

// DefaultConfig returns a fully-populated config with built-in defaults.
func DefaultConfig() *AppConfig {
	return &AppConfig{
		Keybindings: defaultKeybindings(),
		Themes: RawThemes{
			Default: "gruvbox-material-dark",
			Map:     defaultThemesMap(),
		},
	}
}

// ── Loading ───────────────────────────────────────────────────────────

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
