package harness_test

import (
	"errors"
	"fmt"
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

func TestBrainRepositoryDoesNotRouteToItselfAsWiderContext(t *testing.T) {
	// Arrange
	built := demoManifest()
	brain, _ := built.Find("brain")

	// Act
	rendered := harness.RenderRepo(built, brain)

	// Assert: telling a session in ../brain to go to ../brain and then not
	// write there contradicts the write permit rendered a few lines above.
	if strings.Contains(rendered, "live in `../brain`") {
		t.Errorf("the brain repository routes to itself as another repository:\n%s", rendered)
	}
	if !strings.Contains(rendered, "This repository is the organisation-wide knowledge layer") {
		t.Errorf("the brain repository does not identify its own role:\n%s", rendered)
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

// The oversized rule warns that a bloated root contract silently truncates the
// per-repository contracts an agent needs, and says "keep this file a map, not
// a copy". A workspace of 120 repositories generated 15,195 bytes of map: every
// byte written by vat, none of it removable, and `vat lint --fix` could not
// help. A rule that fires on a correct workspace is a rule that gets turned off.
//
// So the roster is bounded by the budget the rule enforces, and says how many it
// did not list — an agent that reads a partial roster must not conclude the rest
// do not exist.
func TestTheGeneratedContractStaysInsideTheBudgetItsOwnRuleEnforces(t *testing.T) {
	// Arrange
	for _, count := range []int{1, 40, 120, 500} {
		m := manifest.Default("acme")
		for i := 1; i <= count; i++ {
			m = manifest.WithRepo(m, manifest.Repo{
				Name:   fmt.Sprintf("service-number-%d", i),
				Origin: fmt.Sprintf("https://example.invalid/acme/service-number-%d.git", i),
				Role:   manifest.RoleProduct,
			})
		}

		// Act
		rendered := harness.RenderWorkspace(m)

		// Assert
		if len(rendered) > 12*1024 {
			t.Errorf("%d repositories render %d bytes, past the budget the rule enforces",
				count, len(rendered))
		}
		listed := strings.Count(rendered, "| `service-number-")
		if listed == count {
			continue
		}
		if listed > count {
			t.Errorf("%d repositories produced %d rows", count, listed)
		}
		// Whatever was left out has to be said, or a reader concludes it is not
		// there.
		if !strings.Contains(rendered, fmt.Sprintf("%d more", count-listed)) {
			t.Errorf("%d repositories rendered %d rows and the contract does not say how many it left out",
				count, listed)
		}
		if !strings.Contains(rendered, "vat repo list") {
			t.Errorf("%d repositories rendered a partial roster with no way to read the rest", count)
		}
	}
}

// git on Windows converts LF to CRLF on checkout under its default
// core.autocrlf, so a contract this tool wrote and committed comes back with
// different line endings. Comparing bytes reported drift on a file nobody had
// touched, `vat lint --fix` rewrote it with LF, git converted it back, and the
// finding returned on the next run — for as long as anybody kept looking.
//
// This project pins its own working tree to LF in .gitattributes. A user's
// workspace has no such file unless they write one, and vat does not.
func TestALineEndingIsNotDrift(t *testing.T) {
	// Arrange
	m := demoManifest()
	region := harness.RenderWorkspace(m)
	document := harness.ApplyRegion("# acme\n\nHand-written.\n", region)

	// Act
	windows := strings.ReplaceAll(document, "\n", "\r\n")

	// Assert
	if !harness.RegionMatches(document, region) {
		t.Fatal("a document this tool just wrote does not match the region it wrote")
	}
	if !harness.RegionMatches(windows, region) {
		t.Error("the same contract with CRLF endings is reported as drifted")
	}
}

// The same failure in the adapters, which are compared whole rather than by
// region: on Windows a committed adapter comes back with CRLF and every one of
// them is reported as drifted, on every run, for as long as anybody keeps
// looking. The self-contract test in this package already normalises for the
// same reason; the product did not.
func TestALineEndingIsNotAdapterDrift(t *testing.T) {
	// Arrange
	root := t.TempDir()
	writeAt(t, root, ".agents/roles/planner.md",
		"---\nname: planner\ndescription: Plans work.\n---\n\nBody.\n")
	writeAt(t, root, ".agents/skills/deploy/SKILL.md",
		"---\nname: deploy\ndescription: Ships one service.\n---\n\nSteps.\n")
	roles, _, err := harness.LoadRoles(root)
	if err != nil {
		t.Fatalf("LoadRoles: %v", err)
	}
	skills, _, err := harness.LoadSkills(root)
	if err != nil {
		t.Fatalf("LoadSkills: %v", err)
	}
	if _, err := harness.WriteAdapters(root, roles); err != nil {
		t.Fatalf("WriteAdapters: %v", err)
	}
	if _, err := harness.WriteSkillAdapters(root, skills); err != nil {
		t.Fatalf("WriteSkillAdapters: %v", err)
	}

	// Act: git's default on Windows, applied to every adapter on the way out.
	converted := 0
	for _, adapter := range harness.RenderAdapters(roles[0]) {
		toCRLF(t, filepath.Join(root, adapter.Path))
		converted++
	}
	for _, adapter := range harness.RenderSkillAdapters(skills[0]) {
		toCRLF(t, filepath.Join(root, adapter.Path))
		converted++
	}
	if converted != 3 {
		t.Fatalf("expected two role adapters and one skill adapter, converted %d", converted)
	}

	// Assert
	drifted, err := harness.AdapterDrift(root, roles)
	if err != nil {
		t.Fatalf("AdapterDrift: %v", err)
	}
	if len(drifted) != 0 {
		t.Errorf("role adapters reported as drifted for their line endings: %v", drifted)
	}
	skillDrift, err := harness.SkillAdapterDrift(root, skills)
	if err != nil {
		t.Fatalf("SkillAdapterDrift: %v", err)
	}
	if len(skillDrift) != 0 {
		t.Errorf("skill adapters reported as drifted for their line endings: %v", skillDrift)
	}
}

// toCRLF rewrites a file the way git does on Windows under its default
// core.autocrlf.
func toCRLF(t *testing.T, path string) {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	crlf := strings.ReplaceAll(string(content), "\n", "\r\n")
	if err := os.WriteFile(path, []byte(crlf), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// `vat harness render` reported every adapter as written on every run under
// git's default on Windows, because the write was skipped only on an exact byte
// match. Rendering that is not idempotent is rendering nobody can put in CI.
func TestRenderingIsIdempotentAcrossLineEndings(t *testing.T) {
	// Arrange
	root := t.TempDir()
	writeAt(t, root, ".agents/roles/planner.md",
		"---\nname: planner\ndescription: Plans work.\n---\n\nBody.\n")
	roles, _, err := harness.LoadRoles(root)
	if err != nil {
		t.Fatalf("LoadRoles: %v", err)
	}
	if _, err := harness.WriteAdapters(root, roles); err != nil {
		t.Fatalf("WriteAdapters: %v", err)
	}
	for _, adapter := range harness.RenderAdapters(roles[0]) {
		toCRLF(t, filepath.Join(root, adapter.Path))
	}

	// Act
	written, err := harness.WriteAdapters(root, roles)
	if err != nil {
		t.Fatalf("WriteAdapters: %v", err)
	}

	// Assert
	if len(written) != 0 {
		t.Errorf("adapters were rewritten for their line endings: %v", written)
	}
}

// The roster is what tells a session which repository owns what and which
// branch it ships from. A description carrying a pipe split the row into six
// cells, so the Branch column showed the tail of somebody's sentence — an agent
// reading it is told the wrong branch for that repository. A newline split the
// row across two lines and ended the table there.
func TestADescriptionCannotBreakTheRosterRow(t *testing.T) {
	// Arrange
	m := manifest.Default("acme")
	m = manifest.WithRepo(m, manifest.Repo{
		Name: "payments", Origin: "https://example.invalid/acme/payments.git",
		Role: manifest.RoleProduct, Description: "Owns orders | and refunds",
	})
	m = manifest.WithRepo(m, manifest.Repo{
		Name: "orders", Origin: "https://example.invalid/acme/orders.git",
		Role: manifest.RoleProduct, Description: "line one\nline two",
	})

	// Act
	rendered := harness.RenderWorkspace(m)

	// Assert: every row of the roster has exactly the columns its header does.
	var inRoster bool
	for _, line := range strings.Split(rendered, "\n") {
		switch {
		case strings.HasPrefix(line, "| Repository |"):
			inRoster = true
			continue
		case inRoster && !strings.HasPrefix(line, "|"):
			inRoster = false
			continue
		case !inRoster:
			continue
		}
		if cells := unescapedPipes(line); cells != 5 {
			t.Errorf("a roster row has %d separators, not 5: %s", cells, line)
		}
	}
	for _, repo := range []string{"payments", "orders"} {
		if !strings.Contains(rendered, "`"+repo+"`") {
			t.Errorf("%s is missing from the roster", repo)
		}
	}
	// The branch is the last cell of every row, and it is the one an agent acts
	// on.
	if strings.Count(rendered, "| `main` |") != 2 {
		t.Errorf("a row lost its branch column:\n%s", rendered)
	}
}

// unescapedPipes counts the separators a Markdown parser would see, which is
// every pipe not preceded by a backslash.
func unescapedPipes(line string) int {
	count := 0
	for i := 0; i < len(line); i++ {
		if line[i] != '|' {
			continue
		}
		if i > 0 && line[i-1] == '\\' {
			continue
		}
		count++
	}
	return count
}

// The trust table is the boundary this whole layer exists to state, and its
// cells come from lists a user edits in vat.yaml. A pipe in one of them breaks
// the row that says what untrusted content may do.
func TestATrustSourceCannotBreakItsRow(t *testing.T) {
	// Arrange
	m := manifest.Default("acme")
	m.Policy.Trust.Untrusted = []string{"web | anything", "model\noutput"}

	// Act
	rendered := harness.RenderWorkspace(m)

	// Assert
	for _, line := range strings.Split(rendered, "\n") {
		if !strings.HasPrefix(line, "| Untrusted |") {
			continue
		}
		if cells := unescapedPipes(line); cells != 4 {
			t.Errorf("the untrusted row has %d separators, not 4: %s", cells, line)
		}
		if !strings.Contains(line, "never instruction") {
			t.Errorf("the row lost the statement it exists to make: %s", line)
		}
		return
	}
	t.Fatalf("no untrusted row was rendered:\n%s", rendered)
}
