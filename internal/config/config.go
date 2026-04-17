package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/ashiqrniloy/synapta-cli/internal/fsutil"
	"github.com/ashiqrniloy/synapta-cli/internal/normalize"
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
	Newline     string `mapstructure:"newline"      yaml:"newline"`
	Submit      string `mapstructure:"submit"       yaml:"submit"`
	Quit        string `mapstructure:"quit"         yaml:"quit"`
	Stop        string `mapstructure:"stop"         yaml:"stop"`
	Command     string `mapstructure:"command"      yaml:"command"`
	Context     string `mapstructure:"context"      yaml:"context"`
	FileBrowser string `mapstructure:"file_browser" yaml:"file_browser"`
	Help        string `mapstructure:"help"         yaml:"help"`
	Extensions  string `mapstructure:"extensions"   yaml:"extensions"`
}

// Theme defines a complete colour palette for a named theme.
type Theme struct {
	Name                        string  `mapstructure:"name"                          yaml:"name"`
	Background                  string  `mapstructure:"background"                    yaml:"background"`
	Foreground                  string  `mapstructure:"foreground"                    yaml:"foreground"`
	Primary                     string  `mapstructure:"primary"                       yaml:"primary"`
	Secondary                   string  `mapstructure:"secondary"                     yaml:"secondary"`
	Accent                      string  `mapstructure:"accent"                        yaml:"accent"`
	Muted                       string  `mapstructure:"muted"                         yaml:"muted"`
	Border                      string  `mapstructure:"border"                        yaml:"border"`
	Selection                   string  `mapstructure:"selection"                     yaml:"selection"`
	Error                       string  `mapstructure:"error"                         yaml:"error"`
	Success                     string  `mapstructure:"success"                       yaml:"success"`
	CursorFG                    string  `mapstructure:"cursor_fg"                     yaml:"cursor_fg"`
	CursorBG                    string  `mapstructure:"cursor_bg"                     yaml:"cursor_bg"`
	HighlightColor              string  `mapstructure:"highlight_color"               yaml:"highlight_color"`
	HighlightOpacity            float64 `mapstructure:"highlight_opacity"             yaml:"highlight_opacity"`
	InteractionHighlightColor   string  `mapstructure:"interaction_highlight_color"   yaml:"interaction_highlight_color"`
	InteractionHighlightOpacity float64 `mapstructure:"interaction_highlight_opacity" yaml:"interaction_highlight_opacity"`
	SystemMessageColor          string  `mapstructure:"system_message_color"          yaml:"system_message_color"`
	SystemMessageOpacity        float64 `mapstructure:"system_message_opacity"        yaml:"system_message_opacity"`
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
	Default string           `mapstructure:"default" yaml:"default"`
	Map     map[string]Theme `mapstructure:",remain" yaml:",inline"`
}

// ProviderConfig stores the active provider and model selection.
type ProviderConfig struct {
	Default string `mapstructure:"default" yaml:"default"` // Provider ID (e.g., "github-copilot", "kilo")
	Model   string `mapstructure:"model"   yaml:"model"`   // Model ID (e.g., "gpt-4o", "claude-sonnet-4-20250514")
}

type UIConfig struct {
	Density string `mapstructure:"density" yaml:"density"` // compact | comfortable
}

// AppConfig is the top-level configuration structure.
type AppConfig struct {
	Keybindings      Keybindings       `mapstructure:"keybindings"       yaml:"keybindings"`
	CommandShortcuts map[string]string `mapstructure:"command_shortcuts" yaml:"command_shortcuts"`
	Themes           RawThemes         `mapstructure:"themes"            yaml:"themes"`
	Provider         ProviderConfig    `mapstructure:"provider"          yaml:"provider"`
	UI               UIConfig          `mapstructure:"ui"                yaml:"ui"`
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
		"b": "shell",
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
	return normalize.ShortcutKey(key)
}

// mergeField descriptors keep merge logic table-driven and reduce field-by-field
// imperative copy blocks.
type keybindingField struct {
	key string
	set func(*Keybindings, string)
}

var keybindingFields = []keybindingField{
	{key: "newline", set: func(k *Keybindings, v string) { k.Newline = v }},
	{key: "submit", set: func(k *Keybindings, v string) { k.Submit = v }},
	{key: "quit", set: func(k *Keybindings, v string) { k.Quit = v }},
	{key: "stop", set: func(k *Keybindings, v string) { k.Stop = v }},
	{key: "command", set: func(k *Keybindings, v string) { k.Command = v }},
	{key: "context", set: func(k *Keybindings, v string) { k.Context = v }},
	{key: "file_browser", set: func(k *Keybindings, v string) { k.FileBrowser = v }},
	{key: "help", set: func(k *Keybindings, v string) { k.Help = v }},
	{key: "extensions", set: func(k *Keybindings, v string) { k.Extensions = v }},
}

type themeStringField struct {
	key string
	set func(*Theme, string)
}

type themeFloatField struct {
	key string
	set func(*Theme, float64)
}

var themeStringFields = []themeStringField{
	{key: "name", set: func(t *Theme, v string) { t.Name = v }},
	{key: "background", set: func(t *Theme, v string) { t.Background = v }},
	{key: "foreground", set: func(t *Theme, v string) { t.Foreground = v }},
	{key: "primary", set: func(t *Theme, v string) { t.Primary = v }},
	{key: "secondary", set: func(t *Theme, v string) { t.Secondary = v }},
	{key: "accent", set: func(t *Theme, v string) { t.Accent = v }},
	{key: "muted", set: func(t *Theme, v string) { t.Muted = v }},
	{key: "border", set: func(t *Theme, v string) { t.Border = v }},
	{key: "selection", set: func(t *Theme, v string) { t.Selection = v }},
	{key: "error", set: func(t *Theme, v string) { t.Error = v }},
	{key: "success", set: func(t *Theme, v string) { t.Success = v }},
	{key: "cursor_fg", set: func(t *Theme, v string) { t.CursorFG = v }},
	{key: "cursor_bg", set: func(t *Theme, v string) { t.CursorBG = v }},
	{key: "highlight_color", set: func(t *Theme, v string) { t.HighlightColor = v }},
	{key: "interaction_highlight_color", set: func(t *Theme, v string) { t.InteractionHighlightColor = v }},
	{key: "system_message_color", set: func(t *Theme, v string) { t.SystemMessageColor = v }},
}

var themeFloatFields = []themeFloatField{
	{key: "highlight_opacity", set: func(t *Theme, v float64) { t.HighlightOpacity = v }},
	{key: "interaction_highlight_opacity", set: func(t *Theme, v float64) { t.InteractionHighlightOpacity = v }},
	{key: "system_message_opacity", set: func(t *Theme, v float64) { t.SystemMessageOpacity = v }},
}

func mergeThemePalette(v *viper.Viper, prefix string, dst *Theme) {
	for _, f := range themeStringFields {
		k := prefix + "." + f.key
		if !v.IsSet(k) {
			continue
		}
		value := v.GetString(k)
		if value == "" {
			continue
		}
		f.set(dst, value)
	}
	for _, f := range themeFloatFields {
		k := prefix + "." + f.key
		if !v.IsSet(k) {
			continue
		}
		f.set(dst, v.GetFloat64(k))
	}
}

// mergeKeybindings copies only configured non-empty bindings onto dst,
// preserving defaults for fields that were omitted.
func mergeKeybindings(v *viper.Viper, dst *Keybindings) error {
	if !v.IsSet("keybindings") {
		return nil
	}

	for _, f := range keybindingFields {
		key := "keybindings." + f.key
		if !v.IsSet(key) {
			continue
		}
		value, ok := normalize.NonEmpty(v.GetString(key))
		if !ok {
			continue
		}
		f.set(dst, value)
	}

	return nil
}

// mergeThemes collects all theme palette sub-keys under "themes.*" (excluding
// "themes.default") and merges only configured fields into each palette.
func mergeThemes(v *viper.Viper, dst *RawThemes) error {
	if dk, ok := normalize.NonEmpty(v.GetString("themes.default")); ok {
		dst.Default = dk
	}

	// Collect unique top-level theme names from the flat key list viper
	// exposes (e.g. "themes.nord-aurora-dark.background" → "nord-aurora-dark").
	seen := make(map[string]struct{})
	for _, k := range v.AllKeys() {
		const prefix = "themes."
		if !strings.HasPrefix(k, prefix) {
			continue
		}
		rest := strings.TrimPrefix(k, prefix)
		if rest == "default" {
			continue
		}
		name := strings.SplitN(rest, ".", 2)[0]
		if name == "" {
			continue
		}
		seen[name] = struct{}{}
	}

	for name := range seen {
		if dst.Map == nil {
			dst.Map = make(map[string]Theme)
		}
		t := dst.Map[name]
		mergeThemePalette(v, "themes."+name, &t)
		dst.Map[name] = t
	}
	return nil
}

// LoadConfig reads YAML config from the user directory (~/.synapta/config.yaml),
// falling through to the local config/ directory, then to hard-coded defaults.
func LoadConfig() (*AppConfig, error) {
	v := viper.New()
	v.SetConfigName("config")
	v.SetConfigType("yaml")

	agentDir := fsutil.ResolveAgentDir("synapta", "SYNAPTA_DIR")
	v.AddConfigPath(agentDir)
	v.AddConfigPath("./config")
	v.AddConfigPath("../config")

	// Attempt to read — if not found, return defaults.
	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); ok {
			return DefaultConfig(), nil
		}
		return nil, fmt.Errorf("reading config: %w", err)
	}

	cfg := DefaultConfig()

	// Keybindings — unmarshal via struct tags; keep defaults for empty fields.
	if err := mergeKeybindings(v, &cfg.Keybindings); err != nil {
		return nil, err
	}

	// Command shortcuts — merge over defaults; normalise keys.
	if shortcuts := v.GetStringMapString("command_shortcuts"); len(shortcuts) > 0 {
		for rawKey, commandID := range shortcuts {
			key := normalizeShortcutKey(rawKey)
			id, ok := normalize.NonEmpty(commandID)
			if key == "" || !ok {
				continue
			}
			cfg.CommandShortcuts[key] = id
		}
	}

	// Provider config.
	if p := v.GetString("provider.default"); p != "" {
		cfg.Provider.Default = p
	}
	if m := v.GetString("provider.model"); m != "" {
		cfg.Provider.Model = m
	}

	// UI config.
	if d := normalize.ID(v.GetString("ui.density")); d != "" {
		if d == "compact" || d == "comfortable" {
			cfg.UI.Density = d
		}
	}

	// Themes — unmarshal each palette via struct tags; preserve built-in defaults.
	if err := mergeThemes(v, &cfg.Themes); err != nil {
		return nil, err
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

// ── Saving ────────────────────────────────────────────────────────────

// configToMap converts an AppConfig to a plain map[string]any suitable for
// YAML marshalling and merging.  RawThemes.Map is inlined under "themes" so
// the on-disk shape matches what LoadConfig expects.
func configToMap(cfg *AppConfig) (map[string]any, error) {
	// Marshal then unmarshal via YAML to get a generic map; this honours the
	// yaml struct tags and avoids maintaining a parallel hand-written map.
	raw, err := go_yaml.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("marshaling config to intermediate YAML: %w", err)
	}
	var m map[string]any
	if err := go_yaml.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("unmarshaling config to map: %w", err)
	}

	// RawThemes.Map is tagged ",inline" so the theme palette entries are
	// siblings of "default" under the "themes" key — exactly what LoadConfig
	// expects.  No further fixup needed.
	return m, nil
}

// deepMerge copies all keys from src into dst recursively.  src wins for
// scalar values; nested maps are merged rather than replaced so that user
// values not present in src are retained.
func deepMerge(dst, src map[string]any) {
	for k, sv := range src {
		dv, exists := dst[k]
		if !exists {
			dst[k] = sv
			continue
		}
		// If both sides are maps, recurse.
		srcMap, srcIsMap := sv.(map[string]any)
		dstMap, dstIsMap := dv.(map[string]any)
		if srcIsMap && dstIsMap {
			deepMerge(dstMap, srcMap)
		} else {
			// Scalar or type mismatch — src (the new value) wins.
			dst[k] = sv
		}
	}
}

// SaveConfig writes the complete AppConfig to ~/.synapta/config.yaml.
//
// When a file already exists it is read, the new config is deep-merged over
// it (new values win for scalars; nested maps are merged), and the result is
// written back.  This preserves any user comments or keys that AppConfig does
// not know about, while guaranteeing every section of the config is up-to-date.
func SaveConfig(cfg *AppConfig) error {
	if _, err := os.UserHomeDir(); err != nil {
		return fmt.Errorf("getting home dir: %w", err)
	}

	configDir := fsutil.ResolveAgentDir("synapta", "SYNAPTA_DIR")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return fmt.Errorf("creating config dir: %w", err)
	}

	configPath := fsutil.ResolvePath(configDir, "config.yaml")

	// Build the new config map from the full AppConfig.
	newMap, err := configToMap(cfg)
	if err != nil {
		return err
	}

	// Read the existing file (if any) and deep-merge the new values over it.
	data, err := os.ReadFile(configPath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("reading existing config: %w", err)
	}

	if len(data) > 0 {
		var existing map[string]any
		if err := go_yaml.Unmarshal(data, &existing); err != nil {
			// Corrupted file — start fresh.
			existing = make(map[string]any)
		}
		// Merge: new config values overwrite existing; unknown user keys survive.
		deepMerge(existing, newMap)
		newMap = existing
	}

	out, err := go_yaml.Marshal(newMap)
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}

	return os.WriteFile(configPath, out, 0644)
}

// SetProvider updates the active provider and model, then saves to disk.
func (c *AppConfig) SetProvider(providerID, modelID string) error {
	c.Provider.Default = providerID
	c.Provider.Model = modelID
	return SaveConfig(c)
}
