package core

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"
)

type Skill struct {
	Name                   string
	Description            string
	FilePath               string
	BaseDir                string
	DisableModelInvocation bool
}

type SkillDiagnostic struct {
	Type    string // "warning" | "collision"
	Message string
	Path    string
}

type SkillsResult struct {
	Skills      []Skill
	Diagnostics []SkillDiagnostic
}

type LoadSkillsOptions struct {
	CWD             string
	AgentDir        string
	IncludeDefaults bool
	SkillPaths      []string
}

const (
	maxSkillNameLength        = 64
	maxSkillDescriptionLength = 1024
)

func LoadSkills(options LoadSkillsOptions) SkillsResult {
	cwd := options.CWD
	if strings.TrimSpace(cwd) == "" {
		cwd, _ = os.Getwd()
	}
	agentDir := strings.TrimSpace(options.AgentDir)
	includeDefaults := true
	if !options.IncludeDefaults {
		includeDefaults = false
	}

	skillByName := map[string]Skill{}
	realPathSeen := map[string]struct{}{}
	diagnostics := make([]SkillDiagnostic, 0)

	addResult := func(result SkillsResult) {
		diagnostics = append(diagnostics, result.Diagnostics...)
		for _, skill := range result.Skills {
			realPath := skill.FilePath
			if rp, err := filepath.EvalSymlinks(skill.FilePath); err == nil && strings.TrimSpace(rp) != "" {
				realPath = rp
			}
			if _, exists := realPathSeen[realPath]; exists {
				continue
			}
			if _, exists := skillByName[skill.Name]; exists {
				diagnostics = append(diagnostics, SkillDiagnostic{
					Type:    "collision",
					Message: fmt.Sprintf("name %q collision", skill.Name),
					Path:    skill.FilePath,
				})
				continue
			}
			skillByName[skill.Name] = skill
			realPathSeen[realPath] = struct{}{}
		}
	}

	if includeDefaults {
		if strings.TrimSpace(agentDir) != "" {
			addResult(loadSkillsFromDir(filepath.Join(agentDir, "skills"), true))
		}
		addResult(loadSkillsFromDir(filepath.Join(cwd, ".agents", "skills"), true))
	}

	for _, p := range options.SkillPaths {
		resolved := resolveSkillPath(cwd, p)
		if strings.TrimSpace(resolved) == "" {
			continue
		}
		info, err := os.Stat(resolved)
		if err != nil {
			diagnostics = append(diagnostics, SkillDiagnostic{Type: "warning", Message: "skill path does not exist", Path: resolved})
			continue
		}
		if info.IsDir() {
			addResult(loadSkillsFromDir(resolved, true))
			continue
		}
		if strings.HasSuffix(strings.ToLower(info.Name()), ".md") {
			if skill, sd := loadSkillFromFile(resolved); skill != nil {
				addResult(SkillsResult{Skills: []Skill{*skill}, Diagnostics: sd})
			} else {
				diagnostics = append(diagnostics, sd...)
			}
			continue
		}
		diagnostics = append(diagnostics, SkillDiagnostic{Type: "warning", Message: "skill path is not a markdown file", Path: resolved})
	}

	skills := make([]Skill, 0, len(skillByName))
	for _, skill := range skillByName {
		skills = append(skills, skill)
	}
	sort.Slice(skills, func(i, j int) bool { return skills[i].Name < skills[j].Name })

	return SkillsResult{Skills: skills, Diagnostics: diagnostics}
}

func loadSkillsFromDir(dir string, includeRootFiles bool) SkillsResult {
	skills := make([]Skill, 0)
	diagnostics := make([]SkillDiagnostic, 0)

	entries, err := os.ReadDir(dir)
	if err != nil {
		return SkillsResult{Skills: skills, Diagnostics: diagnostics}
	}

	for _, entry := range entries {
		if strings.EqualFold(entry.Name(), "SKILL.md") {
			skill, sd := loadSkillFromFile(filepath.Join(dir, entry.Name()))
			diagnostics = append(diagnostics, sd...)
			if skill != nil {
				skills = append(skills, *skill)
			}
			return SkillsResult{Skills: skills, Diagnostics: diagnostics}
		}
	}

	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".") || entry.Name() == "node_modules" {
			continue
		}
		fullPath := filepath.Join(dir, entry.Name())
		if entry.IsDir() {
			result := loadSkillsFromDir(fullPath, false)
			skills = append(skills, result.Skills...)
			diagnostics = append(diagnostics, result.Diagnostics...)
			continue
		}
		if !includeRootFiles || !strings.HasSuffix(strings.ToLower(entry.Name()), ".md") {
			continue
		}
		skill, sd := loadSkillFromFile(fullPath)
		diagnostics = append(diagnostics, sd...)
		if skill != nil {
			skills = append(skills, *skill)
		}
	}

	return SkillsResult{Skills: skills, Diagnostics: diagnostics}
}

type skillFrontmatter struct {
	Name                   string `yaml:"name"`
	Description            string `yaml:"description"`
	DisableModelInvocation bool   `yaml:"disable-model-invocation"`
}

func loadSkillFromFile(filePath string) (*Skill, []SkillDiagnostic) {
	raw, err := os.ReadFile(filePath)
	if err != nil {
		return nil, []SkillDiagnostic{{Type: "warning", Message: err.Error(), Path: filePath}}
	}

	fm, _ := parseSkillFrontmatter(string(raw))
	baseDir := filepath.Dir(filePath)
	parentDir := filepath.Base(baseDir)
	name := strings.TrimSpace(fm.Name)
	if name == "" {
		name = parentDir
	}

	diagnostics := make([]SkillDiagnostic, 0)
	for _, v := range validateSkillName(name, parentDir) {
		diagnostics = append(diagnostics, SkillDiagnostic{Type: "warning", Message: v, Path: filePath})
	}
	for _, v := range validateSkillDescription(fm.Description) {
		diagnostics = append(diagnostics, SkillDiagnostic{Type: "warning", Message: v, Path: filePath})
	}

	if strings.TrimSpace(fm.Description) == "" {
		return nil, diagnostics
	}

	skill := &Skill{
		Name:                   name,
		Description:            strings.TrimSpace(fm.Description),
		FilePath:               filePath,
		BaseDir:                baseDir,
		DisableModelInvocation: fm.DisableModelInvocation,
	}
	return skill, diagnostics
}

func parseSkillFrontmatter(content string) (skillFrontmatter, string) {
	trimmed := strings.TrimLeft(content, "\ufeff")
	if !strings.HasPrefix(trimmed, "---\n") {
		return skillFrontmatter{}, content
	}
	rest := strings.TrimPrefix(trimmed, "---\n")
	end := strings.Index(rest, "\n---\n")
	if end < 0 {
		return skillFrontmatter{}, content
	}
	fmRaw := rest[:end]
	body := rest[end+5:]

	var fm skillFrontmatter
	if err := yaml.Unmarshal([]byte(fmRaw), &fm); err != nil {
		return skillFrontmatter{}, content
	}
	return fm, body
}

func StripFrontmatter(content string) string {
	_, body := parseSkillFrontmatter(content)
	return strings.TrimSpace(body)
}

func FormatSkillsForPrompt(skills []Skill) string {
	visible := make([]Skill, 0, len(skills))
	for _, skill := range skills {
		if skill.DisableModelInvocation {
			continue
		}
		visible = append(visible, skill)
	}
	if len(visible) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("\n\nThe following skills provide specialized instructions for specific tasks.\n")
	b.WriteString("Use the read tool to load a skill's file when the task matches its description.\n")
	b.WriteString("When a skill file references a relative path, resolve it against the skill directory (parent of SKILL.md / dirname of the path) and use that absolute path in tool commands.\n\n")
	b.WriteString("<available_skills>\n")
	for _, skill := range visible {
		b.WriteString("  <skill>\n")
		b.WriteString("    <name>" + escapeXML(skill.Name) + "</name>\n")
		b.WriteString("    <description>" + escapeXML(skill.Description) + "</description>\n")
		b.WriteString("    <location>" + escapeXML(skill.FilePath) + "</location>\n")
		b.WriteString("  </skill>\n")
	}
	b.WriteString("</available_skills>")
	return b.String()
}

func ExpandSkillReferences(text string, skills []Skill) (string, []Skill, error) {
	return expandSkillReferencesWithLoader(text, skills, func(skill Skill) (string, error) {
		raw, err := os.ReadFile(skill.FilePath)
		if err != nil {
			return "", fmt.Errorf("reading skill %q: %w", skill.Name, err)
		}
		return StripFrontmatter(string(raw)), nil
	})
}

func ExpandSkillReferencesWithCache(text string, skills []Skill, cache *SkillCatalogCache) (string, []Skill, error) {
	if cache == nil {
		return ExpandSkillReferences(text, skills)
	}
	return expandSkillReferencesWithLoader(text, skills, cache.SkillBody)
}

func expandSkillReferencesWithLoader(text string, skills []Skill, loadBody func(skill Skill) (string, error)) (string, []Skill, error) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" || len(skills) == 0 {
		return text, nil, nil
	}

	if strings.HasPrefix(trimmed, "/skill:") {
		rest := strings.TrimPrefix(trimmed, "/skill:")
		parts := strings.SplitN(rest, " ", 2)
		skillName := strings.TrimSpace(parts[0])
		args := ""
		if len(parts) > 1 {
			args = strings.TrimSpace(parts[1])
		}
		skill, ok := findSkillByName(skills, skillName)
		if !ok {
			return text, nil, nil
		}
		block, err := buildSkillInvocationBlock(skill, loadBody)
		if err != nil {
			return "", nil, err
		}
		if args != "" {
			return block + "\n\n" + args, []Skill{skill}, nil
		}
		return block, []Skill{skill}, nil
	}

	mentionRE := regexp.MustCompile(`(^|\s)@([a-z0-9][a-z0-9-]{0,63})`)
	matches := mentionRE.FindAllStringSubmatch(trimmed, -1)
	if len(matches) == 0 {
		return text, nil, nil
	}

	seen := map[string]struct{}{}
	used := make([]Skill, 0)
	for _, match := range matches {
		name := strings.TrimSpace(match[2])
		if name == "" {
			continue
		}
		if _, exists := seen[name]; exists {
			continue
		}
		skill, ok := findSkillByName(skills, name)
		if !ok {
			continue
		}
		seen[name] = struct{}{}
		used = append(used, skill)
	}
	if len(used) == 0 {
		return text, nil, nil
	}

	blocks := make([]string, 0, len(used))
	for _, skill := range used {
		block, err := buildSkillInvocationBlock(skill, loadBody)
		if err != nil {
			return "", nil, err
		}
		blocks = append(blocks, block)
	}
	return strings.Join(blocks, "\n\n") + "\n\n" + text, used, nil
}

func buildSkillInvocationBlock(skill Skill, loadBody func(skill Skill) (string, error)) (string, error) {
	body, err := loadBody(skill)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString("<skill name=\"")
	b.WriteString(escapeXML(skill.Name))
	b.WriteString("\" location=\"")
	b.WriteString(escapeXML(skill.FilePath))
	b.WriteString("\">\n")
	b.WriteString("References are relative to ")
	b.WriteString(skill.BaseDir)
	b.WriteString(".\n\n")
	b.WriteString(body)
	b.WriteString("\n</skill>")
	return b.String(), nil
}

func findSkillByName(skills []Skill, name string) (Skill, bool) {
	for _, skill := range skills {
		if skill.Name == name {
			return skill, true
		}
	}
	return Skill{}, false
}

func validateSkillName(name, parentDir string) []string {
	errors := make([]string, 0)
	if name != parentDir {
		errors = append(errors, fmt.Sprintf("name %q does not match parent directory %q", name, parentDir))
	}
	if len(name) > maxSkillNameLength {
		errors = append(errors, fmt.Sprintf("name exceeds %d characters (%d)", maxSkillNameLength, len(name)))
	}
	if !regexp.MustCompile(`^[a-z0-9-]+$`).MatchString(name) {
		errors = append(errors, "name contains invalid characters (must be lowercase a-z, 0-9, hyphens only)")
	}
	if strings.HasPrefix(name, "-") || strings.HasSuffix(name, "-") {
		errors = append(errors, "name must not start or end with a hyphen")
	}
	if strings.Contains(name, "--") {
		errors = append(errors, "name must not contain consecutive hyphens")
	}
	return errors
}

func validateSkillDescription(description string) []string {
	errors := make([]string, 0)
	description = strings.TrimSpace(description)
	if description == "" {
		errors = append(errors, "description is required")
	} else if len(description) > maxSkillDescriptionLength {
		errors = append(errors, fmt.Sprintf("description exceeds %d characters (%d)", maxSkillDescriptionLength, len(description)))
	}
	return errors
}

func resolveSkillPath(cwd, p string) string {
	trimmed := strings.TrimSpace(p)
	if trimmed == "" {
		return ""
	}
	if strings.HasPrefix(trimmed, "~/") || trimmed == "~" {
		home, _ := os.UserHomeDir()
		if trimmed == "~" {
			trimmed = home
		} else {
			trimmed = filepath.Join(home, strings.TrimPrefix(trimmed, "~/"))
		}
	}
	if filepath.IsAbs(trimmed) {
		return filepath.Clean(trimmed)
	}
	return filepath.Clean(filepath.Join(cwd, trimmed))
}

func escapeXML(s string) string {
	r := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		`"`, "&quot;",
		"'", "&apos;",
	)
	return r.Replace(s)
}
