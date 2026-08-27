package harness_test

import (
	"errors"
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

// The skills loader has this test and the roles loader did not, which is how
// the two drift: one unreadable file must not withdraw the definitions beside
// it, and a name that could escape the adapter directories must still stop
// everything.
func TestOneUnreadableRoleDoesNotWithdrawTheOthers(t *testing.T) {
	// Arrange
	root := t.TempDir()
	dir := filepath.Join(root, harness.RolesDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create: %v", err)
	}
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	write("broken.md", "---\nname: [not valid\n  bad: :\n---\n")
	write("sound.md", "---\nname: sound\ndescription: Works.\n---\n\n# Sound\n")

	// Act
	roles, malformed, err := harness.LoadRoles(root)

	// Assert
	if err != nil {
		t.Fatalf("one bad file stopped the load: %v", err)
	}
	if len(roles) != 1 || roles[0].Name != "sound" {
		t.Errorf("loaded %+v, want only the sound role", roles)
	}
	if len(malformed) != 1 {
		t.Fatalf("the unreadable file was not reported: %+v", malformed)
	}
	// The path is repo-relative with forward slashes, matching what the brain
	// package records and what docs/SPEC.md specifies.
	if want := ".agents/roles/broken.md"; malformed[0].Path != want {
		t.Errorf("Path = %q, want %q", malformed[0].Path, want)
	}
	// And the problem does not repeat the location, absolute or otherwise.
	if strings.Contains(malformed[0].Problem, root) {
		t.Errorf("the problem discloses the machine's layout: %q", malformed[0].Problem)
	}
	if strings.HasPrefix(malformed[0].Problem, ".agents/") {
		t.Errorf("the problem repeats the path it is printed beside: %q", malformed[0].Problem)
	}
}

// The exception, asserted for roles as it is for skills.
func TestAnEscapingRoleNameStopsTheLoadRatherThanBeingSkipped(t *testing.T) {
	// Arrange
	root := t.TempDir()
	dir := filepath.Join(root, harness.RolesDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "escaping.md"),
		[]byte("---\nname: ../../escape\ndescription: no.\n---\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Act
	roles, malformed, err := harness.LoadRoles(root)

	// Assert
	if err == nil {
		t.Fatal("an escaping name was recorded as malformed rather than refused")
	}
	if !errors.Is(err, harness.ErrInvalidRoleName) {
		t.Errorf("err = %v, want ErrInvalidRoleName", err)
	}
	// Nothing partial comes back with a refusal: every caller discards both
	// values when err is non-nil, and returning data nobody reads invites
	// somebody to start reading it.
	if roles != nil || malformed != nil {
		t.Errorf("a refusal returned partial data: roles=%+v malformed=%+v", roles, malformed)
	}
}

// The generated contract is the file every session loads, and it named the
// boundary, the precedence order, the trust tiers, and the commands — and never
// said that procedures exist or where they live. An agent that knows what it
// may not do and not how the job is done guesses at the how, which is the gap
// the skills half of this package was built to close.
func TestTheWorkspaceContractPointsAtWhereProceduresLive(t *testing.T) {
	// Arrange
	m := manifest.Manifest{Version: 1}
	m.Workspace.Name = "acme"

	// Act
	rendered := harness.RenderWorkspace(m)

	// Assert
	if !strings.Contains(rendered, harness.SkillsDir) {
		t.Errorf("the contract never names %s:\n%s", harness.SkillsDir, rendered)
	}
	// Named as a pointer, not restated. The root file has a byte budget and a
	// copy of a procedure is the drift this package exists to prevent.
	if strings.Contains(rendered, "SKILL.md") {
		t.Errorf("the contract reaches past a pointer into the procedures themselves:\n%s", rendered)
	}
}
