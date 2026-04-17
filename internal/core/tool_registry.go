package core

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ashiqrniloy/synapta-cli/internal/core/tools"
	"github.com/ashiqrniloy/synapta-cli/internal/fsutil"
	"github.com/ashiqrniloy/synapta-cli/internal/llm"
)

const (
	ToolSourceBuiltin   = "builtin"
	ToolSourceExtension = "extension"
	ToolSourceUser      = "user-manifest"
)

type ExecutionPolicy struct {
	TimeoutSeconds      int  `json:"timeout_seconds,omitempty"`
	RequireConfirmation bool `json:"require_confirmation,omitempty"`
	AllowNetwork        bool `json:"allow_network,omitempty"`
}

type SafeWorkingDirectoryScope struct {
	Mode  string   `json:"mode,omitempty"`
	Paths []string `json:"paths,omitempty"`
}
type ToolExecutor func(ctx context.Context, input any, onUpdate tools.StreamUpdate) (any, error)
type ToolDecoder func(raw string) (any, error)
type ToolMetadataExtractor func(decoded any) tools.ToolMetadata

type ToolSpec struct {
	Name                 string                    `json:"name"`
	Description          string                    `json:"description,omitempty"`
	Parameters           map[string]any            `json:"parameters,omitempty"`
	Policy               ExecutionPolicy           `json:"policy,omitempty"`
	Capabilities         []string                  `json:"capabilities,omitempty"`
	SafeWorkingDirectory SafeWorkingDirectoryScope `json:"safe_working_directory_scope,omitempty"`
	Streaming            bool                      `json:"streaming,omitempty"`
	Source               string                    `json:"source,omitempty"`
	Decoder              ToolDecoder               `json:"-"`
	Metadata             ToolMetadataExtractor     `json:"-"`
	Executor             ToolExecutor              `json:"-"`

	ResolvedWorkingDirectory string `json:"-"`
}

type ToolRegistry struct {
	mu    sync.RWMutex
	tools map[string]ToolSpec
}

type LoadToolRegistryOptions struct {
	CWD      string
	AgentDir string
}

type ToolManifest struct {
	Name                 string                    `json:"name"`
	Description          string                    `json:"description,omitempty"`
	Parameters           map[string]any            `json:"parameters,omitempty"`
	Command              string                    `json:"command"`
	Args                 []string                  `json:"args,omitempty"`
	WorkDir              string                    `json:"workdir,omitempty"`
	Policy               ExecutionPolicy           `json:"policy,omitempty"`
	Capabilities         []string                  `json:"capabilities,omitempty"`
	SafeWorkingDirectory SafeWorkingDirectoryScope `json:"safe_working_directory_scope,omitempty"`
	Streaming            bool                      `json:"streaming,omitempty"`
}

func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{tools: make(map[string]ToolSpec)}
}

func (r *ToolRegistry) Register(spec ToolSpec) error {
	name := strings.TrimSpace(spec.Name)
	if name == "" {
		return fmt.Errorf("tool name is required")
	}
	if spec.Executor == nil {
		return fmt.Errorf("tool %q has no executor", name)
	}
	spec.Name = name

	r.mu.Lock()
	defer r.mu.Unlock()
	r.tools[name] = spec
	return nil
}

func (r *ToolRegistry) Get(name string) (ToolSpec, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	spec, ok := r.tools[strings.TrimSpace(name)]
	return spec, ok
}

func (r *ToolRegistry) Decode(name string, raw string) (any, error) {
	spec, ok := r.Get(name)
	if !ok {
		return nil, fmt.Errorf("unknown tool: %s", strings.TrimSpace(name))
	}
	if spec.Decoder == nil {
		var v any
		if strings.TrimSpace(raw) == "" {
			raw = "{}"
		}
		if err := json.Unmarshal([]byte(raw), &v); err != nil {
			return nil, fmt.Errorf("invalid %s arguments: %w", spec.Name, err)
		}
		return v, nil
	}
	decoded, err := spec.Decoder(raw)
	if err != nil {
		return nil, err
	}
	return decoded, nil
}

func (r *ToolRegistry) Metadata(name string, decoded any) tools.ToolMetadata {
	spec, ok := r.Get(name)
	if !ok || spec.Metadata == nil {
		return tools.ToolMetadata{}
	}
	return spec.Metadata(decoded)
}

func (r *ToolRegistry) Definitions() []llm.ToolDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()
	defs := make([]llm.ToolDefinition, 0, len(r.tools))
	for _, spec := range r.tools {
		defs = append(defs, llm.ToolDefinition{
			Type: "function",
			Function: llm.ToolFunctionDefinition{
				Name:        spec.Name,
				Description: spec.Description,
				Parameters:  normalizeToolParameters(spec.Parameters),
			},
		})
	}
	sort.Slice(defs, func(i, j int) bool {
		return defs[i].Function.Name < defs[j].Function.Name
	})
	return defs
}

func normalizeToolParameters(parameters map[string]any) map[string]any {
	if len(parameters) == 0 {
		return map[string]any{"type": "object", "properties": map[string]any{}}
	}
	return parameters
}

func (r *ToolRegistry) RegisterBuiltins(toolset *tools.ToolSet) error {
	if toolset == nil {
		return nil
	}

	if toolset.Read != nil {
		if err := r.Register(ToolSpec{
			Name:                 toolset.Read.Name(),
			Description:          toolset.Read.Description(),
			Parameters:           tools.ReadJSONSchema(),
			Source:               ToolSourceBuiltin,
			Capabilities:         []string{"filesystem", "read-only"},
			SafeWorkingDirectory: SafeWorkingDirectoryScope{Mode: "workspace"},
			Decoder: func(raw string) (any, error) {
				if strings.TrimSpace(raw) == "" {
					raw = "{}"
				}
				var in tools.ReadInput
				if err := json.Unmarshal([]byte(raw), &in); err != nil {
					return nil, fmt.Errorf("invalid read arguments: %w", err)
				}
				return in, nil
			},
			Metadata: func(decoded any) tools.ToolMetadata {
				in, ok := decoded.(tools.ReadInput)
				if !ok {
					return tools.ToolMetadata{}
				}
				return tools.ToolMetadata{Path: strings.TrimSpace(in.Path)}
			},
			Executor: func(ctx context.Context, input any, _ tools.StreamUpdate) (any, error) {
				in, ok := input.(tools.ReadInput)
				if !ok {
					return nil, fmt.Errorf("invalid read arguments: expected ReadInput")
				}
				return toolset.Read.Execute(ctx, in)
			},
		}); err != nil {
			return err
		}
	}

	if toolset.Write != nil {
		if err := r.Register(ToolSpec{
			Name:                 toolset.Write.Name(),
			Description:          toolset.Write.Description(),
			Parameters:           tools.WriteJSONSchema(),
			Source:               ToolSourceBuiltin,
			Capabilities:         []string{"filesystem", "mutating"},
			SafeWorkingDirectory: SafeWorkingDirectoryScope{Mode: "workspace"},
			Decoder: func(raw string) (any, error) {
				if strings.TrimSpace(raw) == "" {
					raw = "{}"
				}
				var in tools.WriteInput
				if err := json.Unmarshal([]byte(raw), &in); err != nil {
					return nil, fmt.Errorf("invalid write arguments: %w", err)
				}
				return in, nil
			},
			Metadata: func(decoded any) tools.ToolMetadata {
				in, ok := decoded.(tools.WriteInput)
				if !ok {
					return tools.ToolMetadata{}
				}
				return tools.ToolMetadata{Path: strings.TrimSpace(in.Path)}
			},
			Executor: func(ctx context.Context, input any, _ tools.StreamUpdate) (any, error) {
				in, ok := input.(tools.WriteInput)
				if !ok {
					return nil, fmt.Errorf("invalid write arguments: expected WriteInput")
				}
				return toolset.Write.Execute(ctx, in)
			},
		}); err != nil {
			return err
		}
	}

	if toolset.Bash != nil {
		if err := r.Register(ToolSpec{
			Name:                 toolset.Bash.Name(),
			Description:          toolset.Bash.Description(),
			Parameters:           tools.ShellJSONSchema(),
			Source:               ToolSourceBuiltin,
			Capabilities:         []string{"process", "mutating"},
			SafeWorkingDirectory: SafeWorkingDirectoryScope{Mode: "workspace"},
			Streaming:            true,
			Decoder: func(raw string) (any, error) {
				if strings.TrimSpace(raw) == "" {
					raw = "{}"
				}
				var in tools.BashInput
				if err := json.Unmarshal([]byte(raw), &in); err != nil {
					return nil, fmt.Errorf("invalid shell arguments: %w", err)
				}
				return in, nil
			},
			Metadata: func(decoded any) tools.ToolMetadata {
				in, ok := decoded.(tools.BashInput)
				if !ok {
					return tools.ToolMetadata{}
				}
				return tools.ToolMetadata{Command: strings.TrimSpace(in.Command)}
			},
			Executor: func(ctx context.Context, input any, onUpdate tools.StreamUpdate) (any, error) {
				in, ok := input.(tools.BashInput)
				if !ok {
					return nil, fmt.Errorf("invalid shell arguments: expected BashInput")
				}
				return toolset.Bash.Execute(ctx, in, onUpdate)
			},
		}); err != nil {
			return err
		}
	}

	return nil
}

func (r *ToolRegistry) LoadRuntimeTools(opts LoadToolRegistryOptions) []string {
	cwd := strings.TrimSpace(opts.CWD)
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	cwd = fsutil.CleanAbs(cwd)
	agentDir := fsutil.CleanAbs(strings.TrimSpace(opts.AgentDir))
	warnings := make([]string, 0)

	registerDirManifests := func(dir string, source string) {
		entries, err := fsutil.ReadDirFiltered(dir, fsutil.DefaultIgnoreRules())
		if err != nil {
			return
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".json") {
				continue
			}
			manifestPath := filepath.Join(dir, entry.Name())
			if err := r.registerManifestFile(manifestPath, source, cwd); err != nil {
				warnings = append(warnings, err.Error())
			}
		}
	}

	if agentDir != "" {
		registerDirManifests(filepath.Join(agentDir, "tools"), ToolSourceUser)
		warnings = append(warnings, r.loadExtensionToolManifests(filepath.Join(agentDir, "extensions"), cwd)...)
	}
	if cwd != "" {
		registerDirManifests(filepath.Join(cwd, ".synapta", "tools"), ToolSourceUser)
		registerDirManifests(filepath.Join(cwd, "tools"), ToolSourceUser)
		warnings = append(warnings, r.loadExtensionToolManifests(filepath.Join(cwd, "extensions"), cwd)...)
	}

	return warnings
}

func (r *ToolRegistry) loadExtensionToolManifests(extensionsDir string, cwd string) []string {
	entries, err := fsutil.ReadDirFiltered(extensionsDir, fsutil.DefaultIgnoreRules())
	if err != nil {
		return nil
	}
	warnings := make([]string, 0)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		extDir := filepath.Join(extensionsDir, entry.Name())
		toolJSON := filepath.Join(extDir, "tool.json")
		if _, err := os.Stat(toolJSON); err == nil {
			if regErr := r.registerManifestFile(toolJSON, ToolSourceExtension, cwd); regErr != nil {
				warnings = append(warnings, regErr.Error())
			}
		}
		toolsDir := filepath.Join(extDir, "tools")
		subEntries, err := fsutil.ReadDirFiltered(toolsDir, fsutil.DefaultIgnoreRules())
		if err != nil {
			continue
		}
		for _, sub := range subEntries {
			if sub.IsDir() || !strings.HasSuffix(strings.ToLower(sub.Name()), ".json") {
				continue
			}
			manifestPath := filepath.Join(toolsDir, sub.Name())
			if regErr := r.registerManifestFile(manifestPath, ToolSourceExtension, cwd); regErr != nil {
				warnings = append(warnings, regErr.Error())
			}
		}
	}
	return warnings
}

func (r *ToolRegistry) registerManifestFile(path string, source string, cwd string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading tool manifest %s: %w", path, err)
	}
	var mf ToolManifest
	if err := json.Unmarshal(raw, &mf); err != nil {
		return fmt.Errorf("invalid tool manifest %s: %w", path, err)
	}
	name := strings.TrimSpace(mf.Name)
	if name == "" {
		return fmt.Errorf("invalid tool manifest %s: missing name", path)
	}
	command := strings.TrimSpace(mf.Command)
	if command == "" {
		return fmt.Errorf("invalid tool manifest %s: missing command", path)
	}
	manifestDir := filepath.Dir(path)
	workDir := resolveManifestWorkDir(mf.WorkDir, manifestDir, cwd, source)
	if workDir == "" {
		workDir = cwd
	}
	spec := ToolSpec{
		Name:                     name,
		Description:              strings.TrimSpace(mf.Description),
		Parameters:               normalizeToolParameters(mf.Parameters),
		Policy:                   mf.Policy,
		Capabilities:             append([]string(nil), mf.Capabilities...),
		SafeWorkingDirectory:     mf.SafeWorkingDirectory,
		Streaming:                mf.Streaming,
		Source:                   source,
		ResolvedWorkingDirectory: workDir,
		Decoder: func(raw string) (any, error) {
			if strings.TrimSpace(raw) == "" {
				raw = "{}"
			}
			var in map[string]any
			if err := json.Unmarshal([]byte(raw), &in); err != nil {
				return nil, fmt.Errorf("invalid %s arguments: %w", name, err)
			}
			return in, nil
		},
		Metadata: func(decoded any) tools.ToolMetadata {
			meta := tools.ToolMetadata{}
			m, ok := decoded.(map[string]any)
			if !ok {
				return meta
			}
			if v, ok := m["path"].(string); ok {
				meta.Path = strings.TrimSpace(v)
			}
			if v, ok := m["command"].(string); ok {
				meta.Command = strings.TrimSpace(v)
			}
			return meta
		},
		Executor: manifestExecutor(command, mf.Args, workDir, mf.Policy.TimeoutSeconds, mf.Streaming),
	}
	if spec.SafeWorkingDirectory.Mode == "" {
		spec.SafeWorkingDirectory.Mode = "workspace"
	}
	if spec.Description == "" {
		spec.Description = fmt.Sprintf("External tool from %s", source)
	}
	return r.Register(spec)
}

func resolveManifestWorkDir(configured, manifestDir, cwd, source string) string {
	wd := strings.TrimSpace(configured)
	manifestDir = fsutil.CleanAbs(strings.TrimSpace(manifestDir))
	cwd = fsutil.CleanAbs(strings.TrimSpace(cwd))
	if wd == "" {
		if source == ToolSourceExtension {
			return manifestDir
		}
		return cwd
	}
	return fsutil.ResolvePath(manifestDir, wd)
}

func manifestExecutor(command string, args []string, workDir string, timeoutSeconds int, streaming bool) ToolExecutor {
	trimmedCmd := strings.TrimSpace(command)
	baseArgs := append([]string(nil), args...)
	return func(ctx context.Context, input any, onUpdate tools.StreamUpdate) (any, error) {
		var arguments []byte
		switch v := input.(type) {
		case nil:
			arguments = []byte("{}")
		case map[string]any:
			b, err := json.Marshal(v)
			if err != nil {
				return nil, fmt.Errorf("marshal manifest tool args: %w", err)
			}
			arguments = b
		default:
			b, err := json.Marshal(v)
			if err != nil {
				return nil, fmt.Errorf("marshal manifest tool args: %w", err)
			}
			arguments = b
		}
		runCtx := ctx
		cancel := func() {}
		if timeoutSeconds > 0 {
			runCtx, cancel = context.WithTimeout(ctx, time.Duration(timeoutSeconds)*time.Second)
		}
		defer cancel()

		cmd := exec.CommandContext(runCtx, trimmedCmd, baseArgs...)
		if strings.TrimSpace(workDir) != "" {
			cmd.Dir = workDir
		}
		cmd.Stdin = bytes.NewReader(arguments)

		if !streaming {
			combined, err := cmd.CombinedOutput()
			text := strings.TrimSpace(string(combined))
			res := tools.Result{
				Content: []tools.ContentPart{{Type: tools.ContentPartText, Text: text}},
				Details: map[string]any{"command": trimmedCmd, "args": baseArgs, "workdir": cmd.Dir},
			}
			if err != nil {
				return res, err
			}
			return res, nil
		}

		stdout, err := cmd.StdoutPipe()
		if err != nil {
			return nil, err
		}
		stderr, err := cmd.StderrPipe()
		if err != nil {
			return nil, err
		}
		if err := cmd.Start(); err != nil {
			return nil, err
		}

		var outBuf, errBuf bytes.Buffer
		readPipe := func(r io.Reader, sink *bytes.Buffer, emit bool, done chan<- error) {
			s := bufio.NewScanner(r)
			for s.Scan() {
				line := s.Text()
				sink.WriteString(line)
				sink.WriteByte('\n')
				if emit && onUpdate != nil {
					onUpdate(tools.Result{Content: []tools.ContentPart{{Type: tools.ContentPartText, Text: line}}})
				}
			}
			done <- s.Err()
		}

		errCh := make(chan error, 2)
		go readPipe(stdout, &outBuf, true, errCh)
		go readPipe(stderr, &errBuf, true, errCh)
		readErr1 := <-errCh
		readErr2 := <-errCh
		waitErr := cmd.Wait()
		if readErr1 != nil {
			return nil, readErr1
		}
		if readErr2 != nil {
			return nil, readErr2
		}

		text := strings.TrimSpace(outBuf.String())
		if strings.TrimSpace(errBuf.String()) != "" {
			if text != "" {
				text += "\n"
			}
			text += strings.TrimSpace(errBuf.String())
		}
		res := tools.Result{
			Content: []tools.ContentPart{{Type: tools.ContentPartText, Text: text}},
			Details: map[string]any{"command": trimmedCmd, "args": baseArgs, "workdir": cmd.Dir, "stderr": strings.TrimSpace(errBuf.String())},
		}
		if waitErr != nil {
			return res, waitErr
		}
		return res, nil
	}
}
