package harness_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/takealook97/vat/internal/harness"
)

func writeSkill(t *testing.T, root, name, content string) {
	t.Helper()
	dir := filepath.Join(root, harness.SkillsDir, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create %s: %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, harness.SkillFile), []byte(content), 0o644); err != nil {
		t.Fatalf("write skill: %v", err)
	}
}

// A skill copied into each runtime directory diverges exactly the way a role
// body does, and nobody diffs a procedure either. The adapter must therefore
// carry discovery metadata and a pointer — never the steps.
func TestASkillAdapterPointsAtTheProcedureRatherThanRepeatingIt(t *testing.T) {
	// Arrange
	root := t.TempDir()
	writeSkill(t, root, "release-a-service", `---
name: release-a-service
description: Take one service from a green build to a verified deployment.
---

# Release a service

1. Confirm the build is green.
2. Tag the revision and push the tag.
`)
	skills, err := harness.LoadSkills(root)
	if err != nil {
		t.Fatalf("LoadSkills: %v", err)
	}
	if len(skills) != 1 {
		t.Fatalf("loaded %d skills, want 1", len(skills))
	}

	// Act
	adapters := harness.RenderSkillAdapters(skills[0])

	// Assert
	if len(adapters) != 1 {
		t.Fatalf("rendered %d adapters, want 1", len(adapters))
	}
	adapter := adapters[0]
	if want := filepath.Join(".claude", "skills", "release-a-service", "SKILL.md"); adapter.Path != want {
		t.Errorf("path = %q, want %q", adapter.Path, want)
	}
	if strings.Contains(adapter.Content, "Tag the revision") {
		t.Errorf("the adapter copied the procedure, which is the drift it exists to prevent:\n%s", adapter.Content)
	}
	if !strings.Contains(adapter.Content, ".agents/skills/release-a-service/SKILL.md") {
		t.Errorf("the adapter does not point at the canonical file:\n%s", adapter.Content)
	}
	// Pointing a skill adapter at .agents/roles is how a generated file starts
	// lying about where its own source lives.
	if strings.Contains(adapter.Content, ".agents/roles") {
		t.Errorf("the adapter names the wrong canonical directory:\n%s", adapter.Content)
	}
}

// A directory of references with no SKILL.md is not a skill, and reporting it
// as one would make the rule noise in every repository that keeps shared notes
// beside its procedures.
func TestADirectoryWithNoSkillFileIsNotASkill(t *testing.T) {
	// Arrange
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, harness.SkillsDir, "shared-references"), 0o755); err != nil {
		t.Fatalf("create: %v", err)
	}

	// Act
	skills, err := harness.LoadSkills(root)

	// Assert
	if err != nil {
		t.Fatalf("LoadSkills returned an error: %v", err)
	}
	if len(skills) != 0 {
		t.Errorf("loaded %+v, want nothing", skills)
	}
}

// The name becomes a directory vat writes into, so it is validated the same way
// a role name is: an unchecked "../.." would place a generated file outside the
// runtime directory entirely.
func TestASkillNameThatCouldEscapeIsRefused(t *testing.T) {
	// Arrange
	root := t.TempDir()
	writeSkill(t, root, "escaping", "---\nname: ../../escape\ndescription: no.\n---\n")

	// Act
	_, err := harness.LoadSkills(root)

	// Assert
	if err == nil {
		t.Fatal("a name that escapes the skills directory was accepted")
	}
}

// Writing the adapters must be idempotent, or every render reports change and
// the drift rule stops meaning anything.
func TestWritingSkillAdaptersTwiceReportsChangeOnlyOnce(t *testing.T) {
	// Arrange
	root := t.TempDir()
	writeSkill(t, root, "release-a-service", "---\nname: release-a-service\ndescription: Ship it.\n---\n\nSteps.\n")
	skills, err := harness.LoadSkills(root)
	if err != nil {
		t.Fatalf("LoadSkills: %v", err)
	}

	// Act
	first, err := harness.WriteSkillAdapters(root, skills)
	if err != nil {
		t.Fatalf("WriteSkillAdapters: %v", err)
	}
	second, err := harness.WriteSkillAdapters(root, skills)
	if err != nil {
		t.Fatalf("WriteSkillAdapters: %v", err)
	}

	// Assert
	if len(first) != 1 {
		t.Errorf("first render changed %v, want one adapter", first)
	}
	if len(second) != 0 {
		t.Errorf("second render rewrote %v; rendering is not idempotent", second)
	}
	drifted, err := harness.SkillAdapterDrift(root, skills)
	if err != nil {
		t.Fatalf("SkillAdapterDrift: %v", err)
	}
	if len(drifted) != 0 {
		t.Errorf("freshly written adapters reported as drifted: %v", drifted)
	}
}

// The name decides the adapter path, so two definitions claiming one name
// render to a single file with different content. Rendering then never
// converges: `vat lint --fix` rewrites, the loser mismatches again, and which
// one loses is not even stable between runs.
func TestTwoSkillsClaimingOneNameAreRefused(t *testing.T) {
	// Arrange
	root := t.TempDir()
	writeSkill(t, root, "deploy-api", "---\nname: deploy\ndescription: One.\n---\n\nSteps.\n")
	writeSkill(t, root, "deploy-web", "---\nname: deploy\ndescription: Two.\n---\n\nSteps.\n")

	// Act
	_, err := harness.LoadSkills(root)

	// Assert
	if err == nil {
		t.Fatal("two skills claiming one name were accepted")
	}
	if !errors.Is(err, harness.ErrDuplicateName) {
		t.Errorf("err = %v, want ErrDuplicateName", err)
	}
	for _, want := range []string{"deploy-api", "deploy-web"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error does not name %s, so it cannot be acted on: %v", want, err)
		}
	}
}
