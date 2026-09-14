package harness_test

import (
	"strings"
	"testing"

	"github.com/takealook97/vat/internal/harness"
)

// vat generated a skill adapter for Claude and none for Codex, on the stated
// assumption that Codex discovers a skill through the canonical directory
// itself. Two workspaces disproved it independently: each wrote the same
// render-codex-skills.sh, mirroring .agents/skills into .codex/skills, and each
// wired a --check of that script into its own workspace checks. Their comments
// say so outright — that vat generates no Codex adapter for a skill, and that
// the script stands in the empty place.
//
// A gap two teams fill by hand, the same way, twice, is vat's to fill. Filling
// it also brings those files under harness/adapter-drift, so the enforcement
// those scripts were written to provide comes from the machine that already
// does it for every other adapter.

func TestASkillIsDiscoverableFromCodexToo(t *testing.T) {
	// Arrange
	root := t.TempDir()
	writeSkill(t, root, "consult-the-brain-first", `---
name: consult-the-brain-first
description: Check the knowledge layer before stating something as true.
---

# Consult the brain first
`)
	skills, _, err := harness.LoadSkills(root)
	if err != nil {
		t.Fatalf("LoadSkills: %v", err)
	}

	// Act
	adapters := harness.RenderSkillAdapters(skills[0])

	// Assert
	byRuntime := map[string]harness.Adapter{}
	for _, adapter := range adapters {
		byRuntime[adapter.Runtime] = adapter
	}
	codex, ok := byRuntime["codex"]
	if !ok {
		t.Fatalf("a skill targeting every runtime renders no Codex adapter: %+v", adapters)
	}
	if !strings.HasPrefix(codex.Path, harness.CodexSkillDir) {
		t.Errorf("path = %q; want it under %s", codex.Path, harness.CodexSkillDir)
	}
	if _, ok := byRuntime["claude"]; !ok {
		t.Error("the Claude adapter was lost while adding the Codex one")
	}
}

func TestACodexSkillAdapterPointsAtTheCanonicalProcedure(t *testing.T) {
	// Arrange: an adapter that copied the procedure would be a second copy to
	// keep in step, and two procedures differing by one line are worse than one
	// nobody read.
	root := t.TempDir()
	writeSkill(t, root, "close-the-shipping-gate", `---
name: close-the-shipping-gate
description: Judge whether the gate may close.
---

# Close the shipping gate

Step one is a sentence that must not be duplicated into an adapter.
`)
	skills, _, err := harness.LoadSkills(root)
	if err != nil {
		t.Fatalf("LoadSkills: %v", err)
	}

	// Act
	var codex harness.Adapter
	for _, adapter := range harness.RenderSkillAdapters(skills[0]) {
		if adapter.Runtime == "codex" {
			codex = adapter
		}
	}

	// Assert
	if strings.Contains(codex.Content, "must not be duplicated") {
		t.Errorf("the adapter carries the procedure itself:\n%s", codex.Content)
	}
	if !strings.Contains(codex.Content, harness.SkillsDir) {
		t.Errorf("the adapter does not point at the canonical directory:\n%s", codex.Content)
	}
	if !strings.Contains(codex.Content, "close-the-shipping-gate") {
		t.Errorf("the adapter does not name the skill:\n%s", codex.Content)
	}
}

func TestASkillCanStillTargetOneRuntime(t *testing.T) {
	// Arrange: the list got longer, not unconditional. A skill that names one
	// runtime must still get that one and no other.
	root := t.TempDir()
	writeSkill(t, root, "claude-only", `---
name: claude-only
description: A procedure only Claude should discover.
runtimes: [claude]
---

# Claude only
`)
	skills, _, err := harness.LoadSkills(root)
	if err != nil {
		t.Fatalf("LoadSkills: %v", err)
	}

	// Act
	adapters := harness.RenderSkillAdapters(skills[0])

	// Assert
	if len(adapters) != 1 || adapters[0].Runtime != "claude" {
		t.Errorf("adapters = %+v; want the one runtime the skill named", adapters)
	}
}
