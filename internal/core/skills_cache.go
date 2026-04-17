package core

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/ashiqrniloy/synapta-cli/internal/fsutil"
)

type SkillCatalogCache struct {
	mu sync.Mutex

	lastSignature string
	lastOptions   LoadSkillsOptions
	lastResult    SkillsResult

	bodyByPath map[string]cachedSkillBody
}

type cachedSkillBody struct {
	signature string
	body      string
}

func NewSkillCatalogCache() *SkillCatalogCache {
	return &SkillCatalogCache{bodyByPath: map[string]cachedSkillBody{}}
}

func (c *SkillCatalogCache) Load(options LoadSkillsOptions) SkillsResult {
	c.mu.Lock()
	defer c.mu.Unlock()

	sig := skillsLoadSignature(options)
	if sig == c.lastSignature {
		return cloneSkillsResult(c.lastResult)
	}
	result := LoadSkills(options)
	c.lastSignature = sig
	c.lastOptions = options
	c.lastResult = cloneSkillsResult(result)
	return cloneSkillsResult(result)
}

func (c *SkillCatalogCache) Invalidate() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lastSignature = ""
	c.lastResult = SkillsResult{}
	c.bodyByPath = map[string]cachedSkillBody{}
}

func (c *SkillCatalogCache) SkillBody(skill Skill) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	sig := fileStatSignature(skill.FilePath)
	if cached, ok := c.bodyByPath[skill.FilePath]; ok && cached.signature == sig {
		return cached.body, nil
	}
	raw, err := os.ReadFile(skill.FilePath)
	if err != nil {
		return "", err
	}
	body := StripFrontmatter(string(raw))
	c.bodyByPath[skill.FilePath] = cachedSkillBody{signature: sig, body: body}
	return body, nil
}

func cloneSkillsResult(in SkillsResult) SkillsResult {
	out := SkillsResult{}
	if len(in.Skills) > 0 {
		out.Skills = append([]Skill(nil), in.Skills...)
	}
	if len(in.Diagnostics) > 0 {
		out.Diagnostics = append([]SkillDiagnostic(nil), in.Diagnostics...)
	}
	return out
}

func skillsLoadSignature(options LoadSkillsOptions) string {
	cwd := options.CWD
	if strings.TrimSpace(cwd) == "" {
		cwd, _ = os.Getwd()
	}
	cwd = fsutil.CleanAbs(cwd)
	agentDir := fsutil.CleanAbs(strings.TrimSpace(options.AgentDir))
	parts := []string{"cwd=" + cwd, "agent=" + agentDir}
	if options.IncludeDefaults {
		parts = append(parts, "defaults=1")
	} else {
		parts = append(parts, "defaults=0")
	}

	dirs := make([]string, 0)
	if options.IncludeDefaults {
		if agentDir != "" {
			dirs = append(dirs, filepath.Join(agentDir, "skills"))
		}
		dirs = append(dirs, filepath.Join(cwd, ".agents", "skills"))
	}
	for _, p := range options.SkillPaths {
		resolved := fsutil.ResolvePath(cwd, p)
		if strings.TrimSpace(resolved) == "" {
			continue
		}
		dirs = append(dirs, resolved)
	}
	sort.Strings(dirs)
	for _, p := range dirs {
		parts = append(parts, pathTreeSignature(p))
	}
	return strings.Join(parts, "|")
}

func pathTreeSignature(path string) string {
	st, err := os.Stat(path)
	if err != nil {
		return path + ":missing"
	}
	if !st.IsDir() {
		return fileStatSignature(path)
	}
	files := make([]string, 0)
	_ = fsutil.WalkFiles(path, fsutil.DefaultIgnoreRules(), func(p string, d os.DirEntry) error {
		if d.IsDir() {
			return nil
		}
		lower := strings.ToLower(d.Name())
		if lower == "skill.md" || strings.HasSuffix(lower, ".md") {
			files = append(files, fileStatSignature(p))
		}
		return nil
	})
	sort.Strings(files)
	return path + ":" + strings.Join(files, ";")
}

func fileStatSignature(path string) string {
	st, err := os.Stat(path)
	if err != nil {
		return path + ":missing"
	}
	return fmt.Sprintf("%s:%s:%d", path, st.ModTime().UTC().Format("20060102150405.000000000"), st.Size())
}
