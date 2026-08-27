package manifest_test

import (
	"os"
	"path/filepath"
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

// Two repositories whose directories differ only in case are one directory on
// macOS and on Windows. Both entries then govern the same tree: `vat status`
// counts two, every rule fires twice, the generated AGENTS.md is written twice
// with different names so one of them is always drifted, and `vat repo remove
// --delete` on either takes the other's working tree with it.
//
// Refused on every platform, not only where the filesystem collides. A manifest
// is the shared truth of a workspace, and one that validates on the author's
// Linux box and destroys a colleague's checkout on macOS is not shared truth.
func TestTwoRepositoriesDifferingOnlyInCaseAreRefused(t *testing.T) {
	// Arrange
	m := manifest.Default("acme")
	m = manifest.WithRepo(m, manifest.Repo{
		Name: "payments", Origin: "https://example.invalid/acme/payments.git", Role: manifest.RoleProduct,
	})
	m = manifest.WithRepo(m, manifest.Repo{
		Name: "Payments", Origin: "https://example.invalid/acme/payments-two.git", Role: manifest.RoleProduct,
	})

	// Act
	err := manifest.Validate(m)

	// Assert
	if err == nil {
		t.Fatal("a manifest naming one directory twice was accepted")
	}
	if !strings.Contains(err.Error(), "case") {
		t.Errorf("the refusal does not say what the collision is: %v", err)
	}
}

// Names that differ by more than case still describe different directories.
func TestTwoRepositoriesWithGenuinelyDifferentNamesAreAccepted(t *testing.T) {
	// Arrange
	m := manifest.Default("acme")
	m = manifest.WithRepo(m, manifest.Repo{
		Name: "payments", Origin: "https://example.invalid/acme/payments.git", Role: manifest.RoleProduct,
	})
	m = manifest.WithRepo(m, manifest.Repo{
		Name: "payments-api", Origin: "https://example.invalid/acme/payments-api.git", Role: manifest.RoleProduct,
	})

	// Act & Assert
	if err := manifest.Validate(m); err != nil {
		t.Errorf("two distinct repositories were refused: %v", err)
	}
}

// vat ships Windows binaries and runs Windows CI, and a manifest is the shared
// truth of a workspace. A name that cannot become a directory on Windows is one
// a colleague can never clone into, so it is refused everywhere rather than on
// the machine that happens to notice.
func TestNamesThatCannotBecomeADirectoryOnEveryPlatformAreRefused(t *testing.T) {
	// Arrange & Act & Assert
	for _, name := range []string{
		// Windows device names, which cannot be directories at all.
		"con", "CON", "nul", "aux", "prn", "com1", "COM9", "lpt1", "lpt9",
		// Reserved with any extension, because the device is matched first.
		"con.api", "NUL.service",
		// Windows silently strips a trailing dot, so this becomes "foo" and
		// collides with a repository actually called foo.
		"foo.", "foo..",
	} {
		if err := manifest.ValidateRepoName(name); err == nil {
			t.Errorf("%q was accepted as a repository name", name)
		}
	}
	for _, name := range []string{
		// Not devices: the reservation is on the exact stem, not a prefix.
		"console", "connect", "com", "com10", "lpt", "auxiliary", "printer",
		"payments", "payments-api", "a.b", "v1.2.3",
	} {
		if err := manifest.ValidateRepoName(name); err != nil {
			t.Errorf("%q is a usable directory name and was refused: %v", name, err)
		}
	}
}

// The path is already in the error the filesystem returns, so wrapping it with
// the path again reported it twice: "read /long/path/vat.yaml: read
// /long/path/vat.yaml: is a directory". That is the first thing somebody sees
// when their workspace is in a state they did not expect.
func TestReadingAnUnreadableManifestSaysThePathOnce(t *testing.T) {
	// Arrange
	root := t.TempDir()
	path := filepath.Join(root, manifest.FileName)
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Act
	_, err := manifest.Load(path)

	// Assert
	if err == nil {
		t.Fatal("a directory was read as a manifest")
	}
	if strings.Count(err.Error(), path) > 1 {
		t.Errorf("the path is reported more than once: %v", err)
	}
	if !strings.Contains(err.Error(), "is a directory") {
		t.Errorf("the error does not say what went wrong: %v", err)
	}
}

// An origin and a branch are handed to git as arguments. A value beginning with
// a dash is not a remote or a branch under any convention, and git reads it as
// an option wherever the call site has no `--` in front of it — which is most of
// them, and which the manifest is in no position to know.
//
// The manifest is committed and shared, so a value that reaches git as an option
// arrives on every colleague's machine. Refused where it enters rather than
// defended at each of a dozen call sites.
func TestAnOriginOrBranchThatGitWouldReadAsAnOptionIsRefused(t *testing.T) {
	// Arrange & Act & Assert
	for _, origin := range []string{
		"--upload-pack=touch /tmp/x",
		"-o",
		"--config=core.pager=x",
	} {
		m := manifest.Default("acme")
		m = manifest.WithRepo(m, manifest.Repo{
			Name: "payments", Origin: origin, Role: manifest.RoleProduct,
		})
		if err := manifest.Validate(m); err == nil {
			t.Errorf("origin %q was accepted", origin)
		}
	}
	for _, branch := range []string{"--upload-pack=x", "-b"} {
		m := manifest.Default("acme")
		m = manifest.WithRepo(m, manifest.Repo{
			Name: "payments", Origin: "https://example.invalid/acme/payments.git",
			Role: manifest.RoleProduct, DefaultBranch: branch,
		})
		if err := manifest.Validate(m); err == nil {
			t.Errorf("branch %q was accepted", branch)
		}
	}
}

// Real origins and branches keep working, including the local placeholder and
// the scp-like form.
func TestOrdinaryOriginsAndBranchesAreAccepted(t *testing.T) {
	// Arrange & Act & Assert
	for _, origin := range []string{
		"https://example.invalid/acme/payments.git",
		"git@example.invalid:acme/payments.git",
		"ssh://git@example.invalid/acme/payments.git",
		"/srv/git/payments.git",
		"../payments.git",
		manifest.LocalOrigin("payments"),
	} {
		m := manifest.Default("acme")
		m = manifest.WithRepo(m, manifest.Repo{
			Name: "payments", Origin: origin, Role: manifest.RoleProduct,
			DefaultBranch: "release-1.0",
		})
		if err := manifest.Validate(m); err != nil {
			t.Errorf("origin %q was refused: %v", origin, err)
		}
	}
}

// `ssh://git@github.com/acme/x.git` is the SSH URL every forge publishes, and
// `git` there is the login name, not a credential. It was refused as one — while
// `git@github.com:acme/x.git`, which means exactly the same thing, was accepted.
//
// Worse than the refusal: `vat repo adopt` strips userinfo rather than refusing,
// so adopting a repository cloned over SSH recorded an origin with the login
// name removed. That URL does not authenticate.
//
// A credential is a secret. A bare user name over SSH is an address.
func TestAnSSHLoginNameIsNotACredential(t *testing.T) {
	// Act & Assert
	for _, url := range []string{
		"ssh://git@github.com/acme/payments.git",
		"ssh://git@gitlab.example.invalid:2222/acme/payments.git",
		"git+ssh://git@example.invalid/acme/payments.git",
	} {
		if manifest.HasEmbeddedCredential(url) {
			t.Errorf("%q was read as embedding a credential", url)
		}
	}

	// A password is a credential whatever the scheme, and userinfo over http is
	// a token however it is spelled.
	for _, url := range []string{
		"ssh://git:hunter2@example.invalid/acme/payments.git",
		"https://token@github.com/acme/payments.git",
		"https://user:token@github.com/acme/payments.git",
		"http://user:token@example.invalid/acme/payments.git",
	} {
		if !manifest.HasEmbeddedCredential(url) {
			t.Errorf("%q was not read as embedding a credential", url)
		}
	}
}
