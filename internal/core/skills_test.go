package core

import (
	"os"
	"strings"
	"testing"
)

func TestLoadSkills_CollisionAndInvalidFiles(t *testing.T) {
	cwd := t.TempDir()
	mustDir := func(path string) {
		t.Helper()
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", path, err)
		}
	}
	mustFile := func(path, body string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}

	mustDir(cwd + "/.agents/skills/one")
	mustDir(cwd + "/.agents/skills/two")
	mustDir(cwd + "/.agents/skills/invalid")

	dupA := "---\nname: shared-skill\ndescription: first\n---\n# one\n"
	dupB := "---\nname: shared-skill\ndescription: second\n---\n# two\n"
	invalid := "---\nname: invalid\n---\n# no description\n"
	mustFile(cwd+"/.agents/skills/one/SKILL.md", dupA)
	mustFile(cwd+"/.agents/skills/two/SKILL.md", dupB)
	mustFile(cwd+"/.agents/skills/invalid/SKILL.md", invalid)

	res := LoadSkills(LoadSkillsOptions{CWD: cwd, IncludeDefaults: true})
	if len(res.Skills) != 1 {
		t.Fatalf("expected one surviving skill after collision/invalid filtering, got %d (%#v)", len(res.Skills), res.Skills)
	}

	joined := make([]string, 0, len(res.Diagnostics))
	for _, d := range res.Diagnostics {
		joined = append(joined, d.Type+":"+d.Message)
	}
	diag := strings.Join(joined, "\n")
	if !strings.Contains(diag, "collision") {
		t.Fatalf("expected collision diagnostic, got:\n%s", diag)
	}
	if !strings.Contains(diag, "description is required") {
		t.Fatalf("expected invalid-file diagnostic for missing description, got:\n%s", diag)
	}
}
