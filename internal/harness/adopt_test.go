package harness_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/takealook97/vat/internal/harness"
)

func writeAt(t *testing.T, root string, rel, content string) string {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir for %s: %v", rel, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", rel, err)
	}
	return path
}

func TestAdoptableFindsAHandWrittenAdapterAndSkipsAGeneratedOne(t *testing.T) {
	// Arrange
	root := t.TempDir()
	writeAt(t, root, ".claude/agents/reviewer.md",
		"---\nname: reviewer\ndescription: Reviews a diff.\n---\n\nRead the diff.\n")
	writeAt(t, root, ".claude/skills/deploy/SKILL.md",
		"---\nname: deploy\ndescription: Ships one service.\n---\n\n1. Build.\n")
	writeAt(t, root, ".claude/agents/generated.md",
		"---\nname: generated\n---\n\n<!-- "+harness.GeneratedMarker+" -->\n\nPointer.\n")
	// A skill directory holds references beside its procedure; only SKILL.md is
	// a definition.
	writeAt(t, root, ".claude/skills/deploy/references/notes.md", "Notes.\n")

	// Act
	found, err := harness.Adoptable(root)
	if err != nil {
		t.Fatalf("Adoptable: %v", err)
	}

	// Assert
	byAdapter := map[string]harness.Adoption{}
	for _, adoption := range found {
		byAdapter[adoption.Adapter] = adoption
	}
	if len(found) != 2 {
		t.Fatalf("expected the two hand-written definitions, got %+v", found)
	}
	role := byAdapter[".claude/agents/reviewer.md"]
	if role.Kind != "role" || role.Name != "reviewer" || role.Canonical != ".agents/roles/reviewer.md" {
		t.Errorf("the role was planned wrongly: %+v", role)
	}
	skill := byAdapter[".claude/skills/deploy/SKILL.md"]
	if skill.Kind != "skill" || skill.Name != "deploy" || skill.Canonical != ".agents/skills/deploy/SKILL.md" {
		t.Errorf("the skill was planned wrongly: %+v", skill)
	}
	for _, adoption := range found {
		if adoption.Refusal != "" {
			t.Errorf("%s was refused for %q", adoption.Adapter, adoption.Refusal)
		}
	}
}

func TestAdoptableRefusesWhereACanonicalDefinitionAlreadyExists(t *testing.T) {
	// Arrange: the canonical file is the source. Overwriting it would destroy
	// the one copy this tool treats as authoritative.
	root := t.TempDir()
	writeAt(t, root, ".agents/roles/reviewer.md",
		"---\nname: reviewer\ndescription: The real one.\n---\n\nCanonical.\n")
	writeAt(t, root, ".claude/agents/reviewer.md",
		"---\nname: reviewer\ndescription: An impostor.\n---\n\nDifferent.\n")

	// Act
	found, err := harness.Adoptable(root)
	if err != nil {
		t.Fatalf("Adoptable: %v", err)
	}
	written, err := harness.Adopt(root, found)
	if err != nil {
		t.Fatalf("Adopt: %v", err)
	}

	// Assert
	if len(found) != 1 || !strings.Contains(found[0].Refusal, "already exists") {
		t.Fatalf("expected one refusal naming the existing file, got %+v", found)
	}
	if len(written) != 0 {
		t.Errorf("a refused adoption was written: %+v", written)
	}
	canonical, err := os.ReadFile(filepath.Join(root, ".agents", "roles", "reviewer.md"))
	if err != nil || !strings.Contains(string(canonical), "Canonical.") {
		t.Errorf("the canonical definition was disturbed: %v %s", err, canonical)
	}
}

func TestAdoptableRefusesANameThatCannotBecomeADirectory(t *testing.T) {
	// Arrange: the name is pasted into a path in .agents and in every runtime's
	// adapter directory, so it is checked before anything is planned.
	root := t.TempDir()
	writeAt(t, root, ".claude/skills/not a name/SKILL.md",
		"---\nname: whatever\ndescription: x\n---\n\nBody.\n")

	// Act
	found, err := harness.Adoptable(root)
	if err != nil {
		t.Fatalf("Adoptable: %v", err)
	}

	// Assert
	if len(found) != 1 || found[0].Refusal == "" {
		t.Fatalf("an unusable name was accepted: %+v", found)
	}
	if found[0].Canonical != "" {
		t.Errorf("a refused adoption still names a destination: %+v", found[0])
	}
}

func TestAnAdoptedRoleClaimsOnlyTheRuntimeItCameFromAndNoWriteAccess(t *testing.T) {
	// Arrange: a bare model is honoured only by a role targeting one runtime.
	// Adopting a Claude file as though it also targeted Codex would name a model
	// Codex cannot resolve, and model-ambiguous would fire on the first lint.
	root := t.TempDir()
	writeAt(t, root, ".claude/agents/reviewer.md",
		"---\nname: reviewer\ndescription: Reviews a diff.\nmodel: opus\n---\n\n# Reviewer\n\nRead it.\n")

	// Act
	found, err := harness.Adoptable(root)
	if err != nil {
		t.Fatalf("Adoptable: %v", err)
	}
	if _, err := harness.Adopt(root, found); err != nil {
		t.Fatalf("Adopt: %v", err)
	}
	roles, _, err := harness.LoadRoles(root)
	if err != nil {
		t.Fatalf("LoadRoles: %v", err)
	}

	// Assert
	if len(roles) != 1 {
		t.Fatalf("expected the adopted role, got %+v", roles)
	}
	role := roles[0]
	if role.ModelIsAmbiguous() {
		t.Error("the adopted role names one model for several runtimes")
	}
	if len(role.Writes) != 0 {
		t.Errorf("adoption granted write access the definition never stated: %v", role.Writes)
	}
	if runtimes := role.TargetedRuntimes(); len(runtimes) != 1 || runtimes[0] != "claude" {
		t.Errorf("expected the runtime it came from alone, got %v", runtimes)
	}
	if !strings.Contains(role.Body, "Read it.") {
		t.Errorf("the prose did not survive adoption:\n%s", role.Body)
	}
}

func TestAnAdoptedDefinitionWithNoBodyGetsSomewhereToWrite(t *testing.T) {
	// Arrange: front matter and nothing else is a real state, and an empty
	// canonical file is one nobody notices is empty.
	root := t.TempDir()
	writeAt(t, root, ".claude/skills/deploy/SKILL.md",
		"---\nname: deploy\ndescription: Ships one service.\n---\n")

	// Act
	found, err := harness.Adoptable(root)
	if err != nil {
		t.Fatalf("Adoptable: %v", err)
	}
	if _, err := harness.Adopt(root, found); err != nil {
		t.Fatalf("Adopt: %v", err)
	}

	// Assert
	skills, _, err := harness.LoadSkills(root)
	if err != nil {
		t.Fatalf("LoadSkills: %v", err)
	}
	if len(skills) != 1 || strings.TrimSpace(skills[0].Body) == "" {
		t.Fatalf("an empty body produced an empty definition: %+v", skills)
	}
}

func TestAdoptableIsEmptyWhenNoRuntimeDirectoryExists(t *testing.T) {
	// Arrange & Act
	found, err := harness.Adoptable(t.TempDir())

	// Assert
	if err != nil {
		t.Fatalf("a workspace with no adapters is not an error: %v", err)
	}
	if len(found) != 0 {
		t.Errorf("found something to adopt in an empty workspace: %+v", found)
	}
}

func TestAdoptableRefusesTwoAdaptersThatWouldBecomeOneFile(t *testing.T) {
	// Arrange: two hand-written files whose names collapse to one destination.
	// Adopting both would have the second silently replace the first, and what
	// it destroys is a body somebody wrote by hand.
	root := t.TempDir()
	writeAt(t, root, ".claude/agents/reviewer.md",
		"---\nname: reviewer\ndescription: First.\n---\n\nFirst body.\n")
	writeAt(t, root, ".claude/agents/nested/reviewer.md",
		"---\nname: reviewer\ndescription: Second.\n---\n\nSecond body.\n")

	// Act
	found, err := harness.Adoptable(root)
	if err != nil {
		t.Fatalf("Adoptable: %v", err)
	}
	if _, err := harness.Adopt(root, found); err != nil {
		t.Fatalf("Adopt: %v", err)
	}

	// Assert
	var refused int
	for _, adoption := range found {
		if adoption.Refusal != "" {
			refused++
		}
	}
	if len(found) != 2 || refused != 1 {
		t.Fatalf("expected one of the two to be refused, got %+v", found)
	}
	// Sorted order decides which keeps the destination, so the outcome does not
	// depend on the order a directory happened to be walked in.
	if found[0].Refusal != "" || found[1].Refusal == "" {
		t.Fatalf("the refusal did not fall on the later adapter: %+v", found)
	}
	if !strings.Contains(found[1].Refusal, found[0].Adapter) {
		t.Errorf("the refusal does not name what claimed the destination: %q", found[1].Refusal)
	}
	body, err := os.ReadFile(filepath.Join(root, ".agents", "roles", "reviewer.md"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(body), "Second body.") {
		t.Errorf("the definition written is not the one that claimed the name:\n%s", body)
	}
}
