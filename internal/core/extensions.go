package core

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func collectExtensionToolManifestWarnings(extDir string) []string {
	warnings := make([]string, 0)
	candidates := []string{filepath.Join(extDir, "tool.json")}

	toolsDir := filepath.Join(extDir, "tools")
	if entries, err := os.ReadDir(toolsDir); err == nil {
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".json") {
				continue
			}
			candidates = append(candidates, filepath.Join(toolsDir, entry.Name()))
		}
	}

	for _, path := range candidates {
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var probe map[string]any
		if err := json.Unmarshal(raw, &probe); err != nil {
			warnings = append(warnings, fmt.Sprintf("invalid extension tool manifest: %s (%v)", path, err))
		}
	}
	return warnings
}

type Extension struct {
	ID          string
	Name        string
	Description string
	Command     string
	Args        []string
	Dir         string
	WorkDir     string
	Source      string
}

type LoadExtensionsOptions struct {
	CWD      string
	AgentDir string
}

type ExtensionsResult struct {
	Extensions []Extension
	Warnings   []string
}

type extensionManifest struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Command     string   `json:"command"`
	Args        []string `json:"args"`
	WorkDir     string   `json:"workdir"`
}

func LoadExtensions(opts LoadExtensionsOptions) ExtensionsResult {
	cwd := strings.TrimSpace(opts.CWD)
	if cwd == "" {
		cwd, _ = os.Getwd()
	}

	dirs := make([]string, 0, 2)
	if strings.TrimSpace(opts.AgentDir) != "" {
		dirs = append(dirs, filepath.Join(opts.AgentDir, "extensions"))
	}
	if cwd != "" {
		dirs = append(dirs, filepath.Join(cwd, "extensions"))
	}

	seen := map[string]struct{}{}
	out := make([]Extension, 0)
	warnings := make([]string, 0)

	for _, base := range dirs {
		entries, err := os.ReadDir(base)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			extDir := filepath.Join(base, entry.Name())
			manifestPath := filepath.Join(extDir, "extension.json")
			raw, err := os.ReadFile(manifestPath)
			if err != nil {
				continue
			}

			var mf extensionManifest
			if err := json.Unmarshal(raw, &mf); err != nil {
				warnings = append(warnings, fmt.Sprintf("invalid extension manifest: %s (%v)", manifestPath, err))
				continue
			}

			id := strings.TrimSpace(mf.ID)
			if id == "" {
				id = entry.Name()
			}
			if _, ok := seen[id]; ok {
				warnings = append(warnings, fmt.Sprintf("duplicate extension id %q at %s", id, manifestPath))
				continue
			}

			cmd := strings.TrimSpace(mf.Command)
			if cmd == "" {
				warnings = append(warnings, fmt.Sprintf("extension %q has empty command (%s)", id, manifestPath))
				continue
			}

			warnings = append(warnings, collectExtensionToolManifestWarnings(extDir)...)

			workdir := strings.TrimSpace(mf.WorkDir)
			if workdir == "" {
				workdir = extDir
			} else if !filepath.IsAbs(workdir) {
				workdir = filepath.Join(extDir, workdir)
			}

			name := strings.TrimSpace(mf.Name)
			if name == "" {
				name = id
			}
			desc := strings.TrimSpace(mf.Description)
			if desc == "" {
				desc = "No description"
			}

			out = append(out, Extension{
				ID:          id,
				Name:        name,
				Description: desc,
				Command:     cmd,
				Args:        append([]string(nil), mf.Args...),
				Dir:         extDir,
				WorkDir:     workdir,
				Source:      manifestPath,
			})
			seen[id] = struct{}{}
		}
	}

	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})

	return ExtensionsResult{Extensions: out, Warnings: warnings}
}
