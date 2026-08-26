package gitx_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/takealook97/vat/internal/gitx"
)

// Everything here shells out to the real git, because the point of this package
// is that it does. A fake would agree with whatever the code already believed.

func TestGitVersionIsReadableFromTheEnvironmentEverythingElseAssumes(t *testing.T) {
	// Arrange: every other function here assumes git is on the path, and doctor
	// reports the version it found. A blank answer reads as an environment that
	// is fine.

	// Act
	version, err := gitx.Version(t.Context())

	// Assert
	if err != nil {
		t.Fatalf("git version: %v", err)
	}
	if strings.TrimSpace(version) == "" {
		t.Error("git reported an empty version")
	}
}

func TestShortRevisionAbbreviatesTheSameCommitHeadReports(t *testing.T) {
	// Arrange: the short form is what every table prints, so it has to name the
	// same commit the long form does rather than an abbreviation of something
	// else.
	dir := newRepo(t)
	full, err := gitx.HeadRevision(t.Context(), dir)
	if err != nil {
		t.Fatalf("head revision: %v", err)
	}

	// Act
	short, err := gitx.ShortRevision(t.Context(), dir, "HEAD")

	// Assert
	if err != nil {
		t.Fatalf("short revision: %v", err)
	}
	if short == "" {
		t.Fatal("the short revision is empty")
	}
	if !strings.HasPrefix(full, short) {
		t.Errorf("short revision %q is not a prefix of %q", short, full)
	}
}

func TestHasRefDistinguishesARefThatExistsFromOneThatDoesNot(t *testing.T) {
	// Arrange: this is how status decides whether a branch has an upstream at
	// all. Answering yes for a missing ref would make it compare against nothing
	// and report agreement.
	dir := newRepo(t)

	// Act & Assert
	if !gitx.HasRef(t.Context(), dir, "refs/heads/main") {
		t.Error("the branch just committed to is reported as missing")
	}
	if gitx.HasRef(t.Context(), dir, "refs/remotes/origin/never-existed") {
		t.Error("a ref that was never created is reported as present")
	}
}

func TestAddRemoteAndSetRemoteURLAgreeWithWhatGitReports(t *testing.T) {
	// Arrange: the remote is the supply-chain claim every other check compares
	// against, so reading it back has to be exact.
	dir := newRepo(t)
	const first = "https://example.invalid/acme/payments.git"
	const second = "https://example.invalid/acme/payments-renamed.git"

	// Act
	if err := gitx.AddRemote(t.Context(), dir, "origin", first); err != nil {
		t.Fatalf("add remote: %v", err)
	}
	added, err := gitx.RemoteURL(t.Context(), dir, "origin")
	if err != nil {
		t.Fatalf("read remote: %v", err)
	}
	if err := gitx.SetRemoteURL(t.Context(), dir, "origin", second); err != nil {
		t.Fatalf("set remote: %v", err)
	}
	updated, err := gitx.RemoteURL(t.Context(), dir, "origin")
	if err != nil {
		t.Fatalf("read remote: %v", err)
	}

	// Assert
	if added != first {
		t.Errorf("the remote came back as %q, want %q", added, first)
	}
	if updated != second {
		t.Errorf("the updated remote is %q, want %q", updated, second)
	}
}

func TestInitCreatesARepositoryOnTheBranchItWasAskedFor(t *testing.T) {
	// Arrange: `repo new` names the initial branch explicitly rather than
	// inheriting whatever the machine's git defaults to, because the manifest
	// records that branch and the two must agree.
	dir := t.TempDir()

	// Act
	if err := gitx.Init(t.Context(), dir, "trunk"); err != nil {
		t.Fatalf("init: %v", err)
	}

	// Assert
	if !gitx.IsRepository(dir) {
		t.Fatal("Init did not leave a git repository behind")
	}
	branch, err := gitx.CurrentBranch(t.Context(), dir)
	if err != nil {
		t.Fatalf("current branch: %v", err)
	}
	if branch != "trunk" {
		t.Errorf("the initial branch is %q, want trunk", branch)
	}
}

func TestCloneProducesAWorkingCopyPointingAtItsSource(t *testing.T) {
	// Arrange: cloning is the one write `vat sync` is allowed to make, so what
	// it leaves behind has to be a repository the rest of the tool recognises.
	source := newRepo(t)
	target := filepath.Join(t.TempDir(), "clone")

	// Act
	if err := gitx.Clone(t.Context(), source, target); err != nil {
		t.Fatalf("clone: %v", err)
	}

	// Assert
	if !gitx.IsRepository(target) {
		t.Error("the clone is not recognised as a git repository")
	}
	remote, err := gitx.RemoteURL(t.Context(), target, "origin")
	if err != nil {
		t.Fatalf("read remote: %v", err)
	}
	if remote != source {
		t.Errorf("the clone's origin is %q, want the source it came from", remote)
	}
}

func TestLastCommitDateReportsAParseableTimestamp(t *testing.T) {
	// Arrange: doctor reports how long ago a repository last moved, and an
	// unparseable answer would be rendered as an age of zero.
	dir := newRepo(t)

	// Act
	when, err := gitx.LastCommitDate(t.Context(), dir)

	// Assert
	if err != nil {
		t.Fatalf("last commit date: %v", err)
	}
	if strings.TrimSpace(when) == "" {
		t.Fatal("the last commit date came back empty")
	}
	// RFC3339 leads with a four-digit year and separates date from time with T.
	if len(when) < len("2006-01-02T15:04:05Z") || !strings.Contains(when, "T") {
		t.Errorf("the timestamp %q is not RFC3339, so every age derived from it is wrong", when)
	}
}

func TestToplevelOfFindsTheRepositoryRootFromInsideIt(t *testing.T) {
	// Arrange: this is how a command run from a subdirectory works out which
	// repository it is in.
	dir := newRepo(t)
	nested := filepath.Join(dir, "internal", "deep")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("create: %v", err)
	}

	// Act
	top, err := gitx.ToplevelOf(t.Context(), nested)

	// Assert
	if err != nil {
		t.Fatalf("toplevel: %v", err)
	}
	// The temporary directory is reached through a symlink on macOS, so both
	// sides are resolved before they are compared.
	want, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	got, err := filepath.EvalSymlinks(top)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got != want {
		t.Errorf("toplevel reported %q, want %q", got, want)
	}
}

func TestToplevelOfFailsOutsideARepository(t *testing.T) {
	// Arrange: a wrong answer here would make a command act on a directory that
	// is not governed at all.

	// Act
	_, err := gitx.ToplevelOf(t.Context(), t.TempDir())

	// Assert
	if err == nil {
		t.Error("a directory that is not in a repository reported a toplevel")
	}
}

func TestRedactWorksOnAMessageWithAURLInsideIt(t *testing.T) {
	// Arrange: the strings that leak are not bare URLs. git's stderr quotes the
	// remote it failed to reach, and that line is printed as-is by sync.
	const stderr = "fatal: unable to access " +
		"'https://x-token:ghp_SUPERSECRET@example.com/acme/payments.git/': Could not resolve host"

	// Act
	got := gitx.Redact(stderr)

	// Assert
	if strings.Contains(got, "ghp_SUPERSECRET") {
		t.Fatalf("Redact left the token in a message: %q", got)
	}
	for _, want := range []string{"unable to access", "example.com", "acme/payments", "Could not resolve"} {
		if !strings.Contains(got, want) {
			t.Errorf("Redact removed %q, which the reader needs: %q", want, got)
		}
	}
}

func TestRedactClearsEveryCredentialInAMessageNotJustTheFirst(t *testing.T) {
	// Arrange: a remote mismatch is reported by naming both URLs in one line.
	// Scanning only as far as the first one left the second token in place.
	const mismatch = "remote https://u:ghp_FIRSTSECRET@example.com/acme/a.git " +
		"does not match https://v:ghp_SECONDSECRET@example.com/acme/b.git"

	// Act
	got := gitx.Redact(mismatch)

	// Assert
	for _, leaked := range []string{"ghp_FIRSTSECRET", "ghp_SECONDSECRET"} {
		if strings.Contains(got, leaked) {
			t.Errorf("Redact left %s in the message: %q", leaked, got)
		}
	}
	if !strings.Contains(got, "acme/a.git") || !strings.Contains(got, "acme/b.git") {
		t.Errorf("Redact lost the repositories the reader is comparing: %q", got)
	}
}

func TestWithoutCredentialsRemovesRatherThanMasks(t *testing.T) {
	// Arrange: recording is not printing. The manifest holds a URL that has to
	// still work when git reads it, so the userinfo is dropped, not starred out.
	const withToken = "https://user:ghp_SUPERSECRET@example.com/acme/payments.git"

	// Act
	got := gitx.WithoutCredentials(withToken)

	// Assert
	if got != "https://example.com/acme/payments.git" {
		t.Errorf("WithoutCredentials(%q) = %q", withToken, got)
	}
	const clean = "https://example.com/acme/payments.git"
	if got := gitx.WithoutCredentials(clean); got != clean {
		t.Errorf("a URL with no credential was changed to %q", got)
	}
}
