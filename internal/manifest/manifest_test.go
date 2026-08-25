package manifest_test

import (
	"strings"
	"testing"

	"github.com/takealook97/vat/internal/manifest"
)

func TestParseAppliesDefaultsWhenPolicyIsOmitted(t *testing.T) {
	// Arrange
	source := []byte(`
version: 1
workspace:
  name: acme
repos:
  - name: payments
    origin: https://example.com/payments.git
`)

	// Act
	parsed, err := manifest.Parse(source)

	// Assert
	if err != nil {
		t.Fatalf("Parse returned an error: %v", err)
	}
	if parsed.Workspace.DefaultBranch != "main" {
		t.Errorf("default_branch = %q, want main", parsed.Workspace.DefaultBranch)
	}
	if parsed.Policy.Brain.StaleAfterDays != 90 {
		t.Errorf("stale_after_days = %d, want 90", parsed.Policy.Brain.StaleAfterDays)
	}
	if parsed.Policy.Gates.Deploy != "manual" {
		t.Errorf("gates.deploy = %q, want manual", parsed.Policy.Gates.Deploy)
	}
	if parsed.Repos[0].Role != manifest.RoleProduct {
		t.Errorf("role = %q, want product", parsed.Repos[0].Role)
	}
}

func TestParseRejectsUnknownFieldsSoATypoCannotDisableARule(t *testing.T) {
	// Arrange: "stale_after_day" is a plausible typo that would otherwise be
	// ignored, silently leaving the default in place.
	source := []byte(`
version: 1
workspace:
  name: acme
policy:
  brain:
    stale_after_day: 30
`)

	// Act
	_, err := manifest.Parse(source)

	// Assert
	if err == nil {
		t.Fatal("Parse accepted an unknown field; a typo would silently disable the rule")
	}
}

func TestValidateReportsEveryProblemAtOnce(t *testing.T) {
	// Arrange
	broken := manifest.Manifest{
		Version:   1,
		Workspace: manifest.Workspace{DefaultBranch: "main"},
		Policy:    manifest.Default("x").Policy,
		Repos: []manifest.Repo{
			{Name: "a", Origin: "", Role: manifest.RoleProduct},
			{Name: "a", Origin: "u", Role: manifest.RoleProduct},
			{Name: "b", Origin: "u", Role: "nonsense"},
		},
	}

	// Act
	err := manifest.Validate(broken)

	// Assert
	if err == nil {
		t.Fatal("Validate accepted an invalid manifest")
	}
	for _, want := range []string{"workspace.name", "origin is required", "duplicate name", "unknown role"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error is missing %q:\n%v", want, err)
		}
	}
}

func TestValidateRejectsMoreThanOneBrainRepository(t *testing.T) {
	// Arrange
	built := manifest.Default("acme")
	built = manifest.WithRepo(built, manifest.Repo{Name: "a", Origin: "u", Role: manifest.RoleBrain})
	built = manifest.WithRepo(built, manifest.Repo{Name: "b", Origin: "u", Role: manifest.RoleBrain})

	// Act
	err := manifest.Validate(built)

	// Assert
	if err == nil || !strings.Contains(err.Error(), "at most one") {
		t.Fatalf("want a single-brain-repository error, got %v", err)
	}
}

func TestValidateRejectsEveryPathThatEscapesTheWorkspace(t *testing.T) {
	// Arrange: a leading ".." is the obvious case. The others are the ones a
	// naive prefix check waves through, and everything downstream then operates
	// outside the root — cloning, writing a harness, and deleting a directory.
	escapes := []string{
		"../outside",
		"sub/../../outside",
		"a/b/../../../outside",
		"..",
		// Root-relative and volume-relative forms. These must be rejected on
		// every platform, not only the one where filepath.IsAbs happens to
		// recognise them: a manifest is read on every machine.
		"/etc",
		`\Windows`,
		`C:\Windows`,
		"C:temp",
		`..\outside`,
		"   ",
		// "." resolves to the workspace root. A repository whose directory is
		// the root turns `repo remove --delete` into deleting the entire
		// workspace, every governed repository's working tree with it.
		".",
		"./",
		"a/..",
	}

	for _, path := range escapes {
		built := manifest.Default("acme")
		built = manifest.WithRepo(built, manifest.Repo{
			Name: "escape", Origin: "u", Role: manifest.RoleProduct, Path: path,
		})

		// Act
		err := manifest.Validate(built)

		// Assert
		if err == nil || !strings.Contains(err.Error(), "inside the workspace") {
			t.Errorf("path %q was accepted; want a containment error, got %v", path, err)
		}
	}
}

func TestValidateAcceptsAPathThatStaysInside(t *testing.T) {
	// Arrange: normalising must not reject a legitimate nested path.
	for _, path := range []string{"payments", "services/payments", "./payments", "a/b/../c"} {
		built := manifest.Default("acme")
		built = manifest.WithRepo(built, manifest.Repo{
			Name: "ok", Origin: "u", Role: manifest.RoleProduct, Path: path,
		})

		// Act & Assert
		if err := manifest.Validate(built); err != nil {
			t.Errorf("path %q was rejected: %v", path, err)
		}
	}
}

func TestWithRepoReplacesByNameAndLeavesTheOriginalUntouched(t *testing.T) {
	// Arrange
	original := manifest.Default("acme")
	original = manifest.WithRepo(original, manifest.Repo{Name: "a", Origin: "one", Role: manifest.RoleProduct})

	// Act
	updated := manifest.WithRepo(original, manifest.Repo{Name: "a", Origin: "two", Role: manifest.RoleProduct})

	// Assert
	if len(updated.Repos) != 1 {
		t.Fatalf("repository count = %d, want 1", len(updated.Repos))
	}
	if updated.Repos[0].Origin != "two" {
		t.Errorf("origin = %q, want two", updated.Repos[0].Origin)
	}
	if original.Repos[0].Origin != "one" {
		t.Errorf("the original manifest was mutated: origin = %q", original.Repos[0].Origin)
	}
}

func TestBranchFallsBackToTheWorkspaceDefault(t *testing.T) {
	// Arrange
	declared := manifest.Repo{DefaultBranch: "develop"}
	inherited := manifest.Repo{}

	// Act & Assert
	if got := declared.Branch("main"); got != "develop" {
		t.Errorf("declared branch = %q, want develop", got)
	}
	if got := inherited.Branch("trunk"); got != "trunk" {
		t.Errorf("inherited branch = %q, want trunk", got)
	}
	if got := inherited.Branch(""); got != "main" {
		t.Errorf("final fallback = %q, want main", got)
	}
}

func TestSelectReturnsAnErrorNamingRepositoriesThatDoNotExist(t *testing.T) {
	// Arrange
	built := manifest.Default("acme")
	built = manifest.WithRepo(built, manifest.Repo{Name: "a", Origin: "u", Role: manifest.RoleProduct})

	// Act
	_, err := built.Select(manifest.Selector{Names: []string{"a", "ghost"}})

	// Assert
	if err == nil || !strings.Contains(err.Error(), "ghost") {
		t.Fatalf("want an error naming the missing repository, got %v", err)
	}
}

func TestSelectExcludesArchivedRepositoriesUnlessNamed(t *testing.T) {
	// Arrange
	built := manifest.Default("acme")
	built = manifest.WithRepo(built, manifest.Repo{Name: "live", Origin: "u", Role: manifest.RoleProduct})
	built = manifest.WithRepo(built, manifest.Repo{
		Name: "retired", Origin: "u", Role: manifest.RoleProduct, Archived: true,
	})

	// Act
	all, err := built.Select(manifest.Selector{})
	named, namedErr := built.Select(manifest.Selector{Names: []string{"retired"}})

	// Assert
	if err != nil || namedErr != nil {
		t.Fatalf("Select returned errors: %v %v", err, namedErr)
	}
	if len(all) != 1 || all[0].Name != "live" {
		t.Errorf("default selection = %v, want only live", all)
	}
	if len(named) != 1 || named[0].Name != "retired" {
		t.Errorf("naming an archived repository should still select it, got %v", named)
	}
}

func TestMarshalRoundTripsThroughParse(t *testing.T) {
	// Arrange
	built := manifest.Default("acme")
	built = manifest.WithRepo(built, manifest.Repo{
		Name: "payments", Origin: "https://example.com/p.git", Role: manifest.RoleProduct,
		Group: "backend", Checks: []string{"make check"}, Required: true,
	})

	// Act
	encoded, err := manifest.Marshal(built)
	if err != nil {
		t.Fatalf("Marshal returned an error: %v", err)
	}
	parsed, err := manifest.Parse(encoded)

	// Assert
	if err != nil {
		t.Fatalf("Parse of marshalled output failed: %v", err)
	}
	if len(parsed.Repos) != 1 || parsed.Repos[0].Group != "backend" {
		t.Errorf("round trip lost data: %+v", parsed.Repos)
	}
	if parsed.Repos[0].Checks[0] != "make check" {
		t.Errorf("checks lost in round trip: %v", parsed.Repos[0].Checks)
	}
}
