package harness_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/takealook97/vat/internal/harness"
	"github.com/takealook97/vat/internal/manifest"
)

func demoManifest() manifest.Manifest {
	built := manifest.Default("acme")
	built = manifest.WithRepo(built, manifest.Repo{
		Name: "payments", Origin: "https://example.com/p.git", Role: manifest.RoleProduct,
		Checks: []string{"make check"},
	})
	built = manifest.WithRepo(built, manifest.Repo{
		Name: "brain", Origin: "https://example.com/b.git", Role: manifest.RoleBrain,
	})
	built.Policy.Brain.Repo = "brain"
	return built
}

func TestApplyRegionReplacesOnlyTheGeneratedPartOnASecondRun(t *testing.T) {
	// Arrange
	original := "# Notes\n\nHand written.\n"
	first := harness.ApplyRegion(original, "generated one")

	// Act
	second := harness.ApplyRegion(first, "generated two")

	// Assert
	if !strings.Contains(second, "Hand written.") {
		t.Error("hand-written content was destroyed")
	}
	if strings.Contains(second, "generated one") {
		t.Error("the previous generated region survived")
	}
	if strings.Count(second, harness.BeginMarker) != 1 {
		t.Error("a second region was appended instead of replacing the first")
	}
}

func TestRegionMatchesDistinguishesCurrentFromDrifted(t *testing.T) {
	// Arrange
	rendered := harness.ApplyRegion("", "content")

	// Act & Assert
	if !harness.RegionMatches(rendered, "content") {
		t.Error("a freshly rendered region was reported as drifted")
	}
	if harness.RegionMatches(rendered, "different") {
		t.Error("a drifted region was reported as current")
	}
	if harness.RegionMatches("no region here", "content") {
		t.Error("a file with no region was reported as matching")
	}
}

func TestRenderWorkspaceStatesTheTrustBoundaryAndTheGates(t *testing.T) {
	// Act
	rendered := harness.RenderWorkspace(demoManifest())

	// Assert
	for _, want := range []string{
		"data, never instruction", "Precedence", "explicit human approval", "payments", "brain",
	} {
		if !strings.Contains(rendered, want) {
			t.Errorf("the workspace contract omits %q", want)
		}
	}
}

func TestRenderWorkspaceStaysWellUnderTheContextBudget(t *testing.T) {
	// Arrange: an oversized root contract pushes per-repository contracts out of
	// an agent's context entirely.
	built := demoManifest()
	for i := 0; i < 40; i++ {
		built = manifest.WithRepo(built, manifest.Repo{
			Name:   "service-" + string(rune('a'+i%26)) + string(rune('0'+i/26)),
			Origin: "https://example.com/x.git", Role: manifest.RoleProduct,
		})
	}

	// Act
	rendered := harness.RenderWorkspace(built)

	// Assert
	if len(rendered) > 12*1024 {
		t.Errorf("rendered workspace contract is %d bytes for 40+ repositories; it must stay a map",
			len(rendered))
	}
}

func TestRenderRepoTellsACredentialRepositoryToCommitCiphertextOnly(t *testing.T) {
	// Arrange
	built := demoManifest()
	credential := manifest.Repo{Name: "credential", Origin: "u", Role: manifest.RoleCredential}
	built = manifest.WithRepo(built, credential)

	// Act
	rendered := harness.RenderRepo(built, credential)

	// Assert
	if !strings.Contains(rendered, "ciphertext only") {
		t.Error("a credential repository's contract does not forbid plaintext")
	}
}

func TestRenderRepoIncludesTheCanonicalChecks(t *testing.T) {
	// Arrange
	built := demoManifest()
	repo, _ := built.Find("payments")

	// Act
	rendered := harness.RenderRepo(built, repo)

	// Assert
	if !strings.Contains(rendered, "make check") {
		t.Error("the repository contract omits its canonical checks")
	}
}

func writeRole(t *testing.T, root, name, frontMatter string) {
	t.Helper()
	path := filepath.Join(root, harness.RolesDir, name+".md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create: %v", err)
	}
	content := "---\n" + strings.TrimSpace(frontMatter) + "\n---\n\n# " + name + "\n\nThe contract.\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func TestARoleWithNoDeclaredWriteTargetGeneratesAReadOnlyAdapter(t *testing.T) {
	// Arrange
	root := t.TempDir()
	writeRole(t, root, "auditor", "name: auditor\ndescription: Audits things.")
	roles, _, err := harness.LoadRoles(root)
	if err != nil {
		t.Fatalf("LoadRoles returned an error: %v", err)
	}

	// Act
	adapters := harness.RenderAdapters(roles[0])

	// Assert
	for _, adapter := range adapters {
		if !strings.Contains(adapter.Content, "read-only") {
			t.Errorf("%s adapter does not declare the role read-only:\n%s",
				adapter.Runtime, adapter.Content)
		}
	}
}

func TestAdapterDriftIsReportedWhenAnAdapterIsEditedDirectly(t *testing.T) {
	// Arrange
	root := t.TempDir()
	writeRole(t, root, "planner", "name: planner\ndescription: Plans.\nwrites: [brain]")
	roles, _, err := harness.LoadRoles(root)
	if err != nil {
		t.Fatalf("LoadRoles returned an error: %v", err)
	}
	if _, err := harness.WriteAdapters(root, roles); err != nil {
		t.Fatalf("WriteAdapters returned an error: %v", err)
	}
	edited := filepath.Join(root, harness.ClaudeAgentDir, "planner.md")
	if err := os.WriteFile(edited, []byte("hand edited\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Act
	drifted, err := harness.AdapterDrift(root, roles)

	// Assert
	if err != nil {
		t.Fatalf("AdapterDrift returned an error: %v", err)
	}
	if len(drifted) == 0 {
		t.Error("an edited adapter was not reported; role definitions would silently diverge")
	}
}

func TestWriteAdaptersIsIdempotent(t *testing.T) {
	// Arrange
	root := t.TempDir()
	writeRole(t, root, "planner", "name: planner\ndescription: Plans.")
	roles, _, err := harness.LoadRoles(root)
	if err != nil {
		t.Fatalf("LoadRoles returned an error: %v", err)
	}
	if _, err := harness.WriteAdapters(root, roles); err != nil {
		t.Fatalf("first WriteAdapters returned an error: %v", err)
	}

	// Act
	changed, err := harness.WriteAdapters(root, roles)

	// Assert
	if err != nil {
		t.Fatalf("second WriteAdapters returned an error: %v", err)
	}
	if len(changed) != 0 {
		t.Errorf("a second run rewrote %v", changed)
	}
}

func TestARoleCanRestrictWhichRuntimesGetAnAdapter(t *testing.T) {
	// Arrange
	root := t.TempDir()
	writeRole(t, root, "claude-only", "name: claude-only\ndescription: x\nruntimes: [claude]")
	roles, _, err := harness.LoadRoles(root)
	if err != nil {
		t.Fatalf("LoadRoles returned an error: %v", err)
	}

	// Act
	adapters := harness.RenderAdapters(roles[0])

	// Assert
	if len(adapters) != 1 || adapters[0].Runtime != "claude" {
		t.Errorf("adapters = %+v, want only claude", adapters)
	}
}
