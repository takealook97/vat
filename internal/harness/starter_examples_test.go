package harness

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/takealook97/vat/internal/fsx"
)

func TestExampleSkillsMatchTheStarterDefinitionsAndAdapters(t *testing.T) {
	root := filepath.Join("..", "..", "examples", "workspace")
	skills := StarterSkills()
	expected := make(map[string]string)
	for _, skill := range skills {
		skill.Dir = skill.Name
		expected[filepath.Join(SkillsDir, skill.Name, SkillFile)] = renderStarter(skill)
		for _, adapter := range RenderSkillAdapters(skill) {
			expected[adapter.Path] = adapter.Content
		}
	}
	for _, dir := range []string{SkillsDir, ClaudeSkillDir, CodexSkillDir} {
		paths, err := filepath.Glob(filepath.Join(root, dir, "*", SkillFile))
		if err != nil {
			t.Fatal(err)
		}
		if len(paths) != len(skills) {
			t.Errorf("%s has %d examples, want %d", dir, len(paths), len(skills))
		}
		for _, path := range paths {
			rel, err := filepath.Rel(root, path)
			if err != nil {
				t.Fatal(err)
			}
			if _, ok := expected[rel]; !ok {
				t.Errorf("unexpected example %s", rel)
			}
		}
	}
	for rel, want := range expected {
		data, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Error(err)
			continue
		}
		if fsx.NormaliseNewlines(string(data)) != fsx.NormaliseNewlines(want) {
			t.Errorf("%s differs from the generated starter; regenerate the example", rel)
		}
	}
}
