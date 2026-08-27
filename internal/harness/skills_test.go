package harness_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/takealook97/vat/internal/harness"
	"gopkg.in/yaml.v3"
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
	skills, _, err := harness.LoadSkills(root)
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
	skills, _, err := harness.LoadSkills(root)

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
	_, _, err := harness.LoadSkills(root)

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
	skills, _, err := harness.LoadSkills(root)
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
	_, _, err := harness.LoadSkills(root)

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

// One unparseable file used to withdraw the adapters of every definition beside
// it, and reported only the first problem — so a second typo stayed invisible
// until the first was fixed. A file vat cannot read is a finding, not a reason
// to do nothing.
func TestOneUnreadableSkillDoesNotWithdrawTheOthers(t *testing.T) {
	// Arrange
	root := t.TempDir()
	writeSkill(t, root, "broken", "---\nname: [this is not\n  valid: yaml\n---\n")
	writeSkill(t, root, "sound", "---\nname: sound\ndescription: Works.\n---\n\nSteps.\n")

	// Act
	skills, malformed, err := harness.LoadSkills(root)

	// Assert
	if err != nil {
		t.Fatalf("one bad file stopped the load: %v", err)
	}
	if len(skills) != 1 || skills[0].Name != "sound" {
		t.Errorf("loaded %+v, want only the sound skill", skills)
	}
	if len(malformed) != 1 {
		t.Fatalf("the unreadable file was not reported: %+v", malformed)
	}
	if !strings.Contains(malformed[0].Path, "broken") {
		t.Errorf("the finding does not name the file: %+v", malformed[0])
	}
	written, err := harness.WriteSkillAdapters(root, skills)
	if err != nil {
		t.Fatalf("WriteSkillAdapters: %v", err)
	}
	if len(written) != 1 {
		t.Errorf("the sound skill's adapter was not written: %v", written)
	}
}

// The exception: a name that could escape is a file asking to be written
// somewhere vat must not write, and that stops the load rather than being
// skipped past with a note.
func TestAnEscapingNameStopsTheLoadRatherThanBeingSkipped(t *testing.T) {
	// Arrange
	root := t.TempDir()
	writeSkill(t, root, "escaping", "---\nname: ../../escape\ndescription: no.\n---\n")
	writeSkill(t, root, "sound", "---\nname: sound\ndescription: Works.\n---\n")

	// Act
	_, _, err := harness.LoadSkills(root)

	// Assert
	if err == nil {
		t.Fatal("an escaping name was skipped rather than refused")
	}
	// And it must say skill, not role: the file is under .agents/skills, and
	// naming the wrong directory sends the reader to the wrong place.
	if !errors.Is(err, harness.ErrInvalidSkillName) {
		t.Errorf("err = %v, want ErrInvalidSkillName", err)
	}
	if strings.Contains(err.Error(), "role name") {
		t.Errorf("a skill problem was reported as a role problem: %v", err)
	}
}

// SkillRuntimeNames is a list, and a list is a claim about behaviour that
// nothing enforces on its own. It exists so `vat lint` can tell a reader that
// `runtimes: [codex]` on a skill selects nothing; if it ever disagrees with
// what RenderSkillAdapters actually produces, the rule built on it starts
// reporting a healthy definition as inert, or an inert one as healthy.
func TestSkillRuntimeNamesAreExactlyTheRuntimesThatRenderAnAdapter(t *testing.T) {
	// Arrange
	root := t.TempDir()
	writeSkill(t, root, "targets-everything", `---
name: targets-everything
description: Names no runtimes, so it targets all of them.
---

# Targets everything
`)
	skills, _, err := harness.LoadSkills(root)
	if err != nil {
		t.Fatalf("LoadSkills: %v", err)
	}

	// Act
	rendered := map[string]bool{}
	for _, adapter := range harness.RenderSkillAdapters(skills[0]) {
		rendered[adapter.Runtime] = true
	}

	// Assert
	listed := map[string]bool{}
	for _, name := range harness.SkillRuntimeNames() {
		listed[name] = true
	}
	for name := range listed {
		if !rendered[name] {
			t.Errorf("SkillRuntimeNames lists %q, but a skill targeting every runtime renders no adapter for it", name)
		}
	}
	for name := range rendered {
		if !listed[name] {
			t.Errorf("a skill adapter is rendered for %q, which SkillRuntimeNames does not list", name)
		}
	}
}

// Codex is a runtime vat knows and generates a role adapter for, so the name is
// not a typo and nothing about the file looks wrong. It still produces no skill
// adapter, which is precisely why the case needs holding down: the definition
// is inert and every other signal reads healthy.
func TestASkillTargetingOnlyCodexRendersNothing(t *testing.T) {
	// Arrange
	root := t.TempDir()
	writeSkill(t, root, "codex-only", `---
name: codex-only
description: A procedure this workspace wants only Codex to discover.
runtimes: [codex]
---

# Codex only
`)
	skills, _, err := harness.LoadSkills(root)
	if err != nil {
		t.Fatalf("LoadSkills: %v", err)
	}
	if len(skills) != 1 {
		t.Fatalf("loaded %d skills, want 1", len(skills))
	}

	// Act
	adapters := harness.RenderSkillAdapters(skills[0])

	// Assert
	if len(adapters) != 0 {
		t.Fatalf("rendered %d adapters for a skill no runtime generates one for, want 0: %+v",
			len(adapters), adapters)
	}
}

// The skill adapter's front matter is the only thing that makes the procedure
// discoverable, and it is built by the same renderer the role adapter is.
func TestAGeneratedSkillHeaderSurvivesWhateverADescriptionContains(t *testing.T) {
	// Arrange
	for _, description := range []string{
		`He said "hi" and a back\slash`,
		"line one\nline two",
		"  indented",
		"*anchor &ref !tag |block",
	} {
		skill := harness.Skill{Name: "deploy", Description: description}

		// Act
		adapter := harness.RenderSkillAdapters(skill)[0]
		header, _, found := strings.Cut(strings.TrimPrefix(adapter.Content, "---\n"), "\n---")
		if !found {
			t.Fatalf("the adapter has no front matter:\n%s", adapter.Content)
		}

		// Assert
		var parsed struct {
			Description string `yaml:"description"`
		}
		if err := yaml.Unmarshal([]byte(header), &parsed); err != nil {
			t.Errorf("the generated header does not parse for %q: %v", description, err)
			continue
		}
		if parsed.Description != description {
			t.Errorf("description round-tripped as %q, want %q", parsed.Description, description)
		}
	}
}
