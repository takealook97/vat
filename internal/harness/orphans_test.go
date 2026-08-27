package harness_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/takealook97/vat/internal/harness"
)

// directoryOf returns the adapter directory a rendered path lives under. A
// skill adapter is one level deeper than a role adapter, so the shared prefix
// is what the walk actually needs.
func directoryOf(path string) string {
	parts := strings.Split(filepath.ToSlash(path), "/")
	if len(parts) < 2 {
		return path
	}
	return strings.Join(parts[:2], "/")
}

func TestOrphanedAdaptersFindsWhatADeletedDefinitionLeftBehind(t *testing.T) {
	// Arrange: the adapters of a role and a skill that no longer exist, beside
	// the adapters of one that does.
	root := t.TempDir()
	writeAt(t, root, ".agents/roles/kept.md",
		"---\nname: kept\ndescription: Still here.\nruntimes: [claude]\n---\n\nBody.\n")
	roles, _, err := harness.LoadRoles(root)
	if err != nil {
		t.Fatalf("LoadRoles: %v", err)
	}
	if _, err := harness.WriteAdapters(root, roles); err != nil {
		t.Fatalf("WriteAdapters: %v", err)
	}
	marker := "<!-- " + harness.GeneratedMarker + " -->\n"
	writeAt(t, root, ".claude/agents/gone.md", "---\nname: gone\n---\n\n"+marker)
	writeAt(t, root, ".codex/agents/gone.toml", "# "+harness.GeneratedMarker+"\nname = \"gone\"\n")
	writeAt(t, root, ".claude/skills/gone/SKILL.md", "---\nname: gone\n---\n\n"+marker)

	// Act
	orphans, err := harness.OrphanedAdapters(root, roles, nil)
	if err != nil {
		t.Fatalf("OrphanedAdapters: %v", err)
	}

	// Assert
	want := map[string]bool{
		".claude/agents/gone.md":       true,
		".codex/agents/gone.toml":      true,
		".claude/skills/gone/SKILL.md": true,
	}
	if len(orphans) != len(want) {
		t.Fatalf("expected %d orphans, got %v", len(want), orphans)
	}
	for _, path := range orphans {
		if !want[path] {
			t.Errorf("%s is generated from a definition that exists and was reported anyway", path)
		}
	}
}

func TestOrphanedAdaptersLeavesWhatVatDidNotWriteAlone(t *testing.T) {
	// Arrange: a rule that fires on a correct workspace is a rule that gets
	// turned off, and an agent file somebody wrote by hand is not vat's to
	// report. The marker is what separates them, not the directory.
	root := t.TempDir()
	writeAt(t, root, ".claude/agents/mine.md", "---\nname: mine\n---\n\nMy own agent.\n")
	writeAt(t, root, ".claude/skills/mine/SKILL.md", "---\nname: mine\n---\n\nMy own procedure.\n")

	// Act
	orphans, err := harness.OrphanedAdapters(root, nil, nil)
	if err != nil {
		t.Fatalf("OrphanedAdapters: %v", err)
	}

	// Assert
	if len(orphans) != 0 {
		t.Errorf("hand-written files were reported as orphans: %v", orphans)
	}
}

func TestOrphanedAdaptersIsEmptyWhenNoRuntimeDirectoryExists(t *testing.T) {
	// Arrange & Act
	orphans, err := harness.OrphanedAdapters(t.TempDir(), nil, nil)

	// Assert
	if err != nil {
		t.Fatalf("a workspace with no adapters is not an error: %v", err)
	}
	if len(orphans) != 0 {
		t.Errorf("found orphans in an empty workspace: %v", orphans)
	}
}

func TestAdapterRootsAreTheDirectoriesAdaptersAreWrittenInto(t *testing.T) {
	// Arrange: the rule walks these, so a runtime directory added to the
	// renderers and not to this list would go unchecked forever.
	written := map[string]bool{}
	role := harness.Role{Name: "r", Description: "d"}
	for _, adapter := range harness.RenderAdapters(role) {
		written[directoryOf(adapter.Path)] = true
	}
	for _, adapter := range harness.RenderSkillAdapters(harness.Skill{Name: "s", Description: "d"}) {
		written[directoryOf(adapter.Path)] = true
	}

	// Act
	roots := map[string]bool{}
	for _, root := range harness.AdapterRoots() {
		roots[root] = true
	}

	// Assert
	for dir := range written {
		if !roots[dir] {
			t.Errorf("adapters are written into %s and AdapterRoots does not list it", dir)
		}
	}
}

func TestTheMarkerScanDoesNotReadAWholeAssetToRejectIt(t *testing.T) {
	// Arrange: a skill directory holds references and scripts beside its
	// procedure, and this walk visits all of them on every lint. A large asset
	// must not be read in full to decide it is not a generated adapter.
	root := t.TempDir()
	writeAt(t, root, ".claude/skills/deploy/SKILL.md",
		"---\nname: deploy\n---\n\n<!-- "+harness.GeneratedMarker+" -->\n")
	// The marker sits far past any sane header, so a bounded read must miss it —
	// and missing it is correct: this is not an adapter.
	asset := strings.Repeat("a", 4<<20) + harness.GeneratedMarker
	writeAt(t, root, ".claude/skills/deploy/references/asset.bin", asset)

	// Act
	orphans, err := harness.OrphanedAdapters(root, nil, nil)
	if err != nil {
		t.Fatalf("OrphanedAdapters: %v", err)
	}

	// Assert
	for _, path := range orphans {
		if strings.Contains(path, "asset.bin") {
			t.Errorf("a reference file was scanned in full and reported: %v", orphans)
		}
	}
	if len(orphans) != 1 || orphans[0] != ".claude/skills/deploy/SKILL.md" {
		t.Errorf("the adapter itself was not reported: %v", orphans)
	}
}
