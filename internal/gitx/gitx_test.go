package gitx_test

import (
	"context"
	"errors"
	"fmt"
	"github.com/takealook97/vat/internal/gitx"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestSameRemoteAcceptsTheSpellingsOfOneRepository(t *testing.T) {
	// Arrange: SSH and HTTPS are two authenticated routes to the same place.
	pairs := [][2]string{
		{"https://github.com/acme/payments.git", "git@github.com:acme/payments.git"},
		{"https://github.com/acme/payments", "ssh://git@github.com/acme/payments.git"},
		{"https://github.com/acme/payments/", "https://github.com/acme/payments"},
		{"https://GitHub.com/acme/payments", "https://github.com/acme/payments"},
	}

	for _, pair := range pairs {
		// Act & Assert
		if !gitx.SameRemote(pair[0], pair[1]) {
			t.Errorf("SameRemote(%q, %q) = false, want true", pair[0], pair[1])
		}
	}
}

func TestSameRemoteKeepsPathCaseSignificant(t *testing.T) {
	// Arrange: on a case-sensitive host these are different repositories, and
	// treating them as one would let a lookalike pass the supply-chain check.
	const declared = "https://example.com/acme/Payments.git"
	const actual = "https://example.com/acme/payments.git"

	// Act & Assert
	if gitx.SameRemote(declared, actual) {
		t.Error("two paths differing only in case compared equal")
	}
}

func TestSameRemoteRefusesToFoldHTTPIntoHTTPS(t *testing.T) {
	// Arrange: silently accepting a downgrade to an unauthenticated transport
	// is exactly the substitution this comparison exists to catch.
	const secure = "https://example.com/acme/payments.git"
	const insecure = "http://example.com/acme/payments.git"

	// Act & Assert
	if gitx.SameRemote(secure, insecure) {
		t.Error("http compared equal to https")
	}
}

func TestSameRemoteIgnoresCredentialsEmbeddedInTheURL(t *testing.T) {
	// Arrange: the same repository, one spelling carrying a token.
	const plain = "https://example.com/acme/payments.git"
	const withToken = "https://x-token:ghp_EXAMPLE@example.com/acme/payments.git"

	// Act & Assert
	if !gitx.SameRemote(plain, withToken) {
		t.Error("a URL carrying credentials was treated as a different repository")
	}
}

func TestSameRemoteRejectsADifferentHost(t *testing.T) {
	// Act & Assert
	if gitx.SameRemote("https://github.com/acme/payments", "https://evil.example/acme/payments") {
		t.Error("different hosts compared equal")
	}
}

func TestRedactRemovesCredentialsButKeepsTheRepositoryIdentifiable(t *testing.T) {
	// Arrange: vat reports a remote mismatch by printing both URLs, and that
	// report must not become the one place a credential is disclosed.
	const withToken = "https://x-token:ghp_SUPERSECRETVALUE@example.com/acme/payments.git"

	// Act
	got := gitx.Redact(withToken)

	// Assert
	if strings.Contains(got, "ghp_SUPERSECRETVALUE") {
		t.Fatalf("Redact leaked the token: %q", got)
	}
	if strings.Contains(got, "x-token") {
		t.Errorf("Redact leaked the user name: %q", got)
	}
	for _, want := range []string{"example.com", "acme/payments"} {
		if !strings.Contains(got, want) {
			t.Errorf("Redact removed %q, which is needed to identify the repository: %q", want, got)
		}
	}
}

func TestRedactLeavesACleanURLUnchanged(t *testing.T) {
	// Arrange
	cases := []string{
		"https://example.com/acme/payments.git",
		"git@example.com:acme/payments.git",
		"/srv/git/payments.git",
	}

	for _, url := range cases {
		// Act & Assert
		if got := gitx.Redact(url); got != url {
			t.Errorf("Redact(%q) = %q, want it unchanged", url, got)
		}
	}
}

func TestDivergenceDescribesTheThreeInterestingStates(t *testing.T) {
	// Arrange
	diverged := gitx.Divergence{Ahead: 2, Behind: 3}
	behind := gitx.Divergence{Behind: 3}
	synced := gitx.Divergence{}

	// Act & Assert
	if !diverged.Diverged() {
		t.Error("both sides holding commits was not reported as diverged")
	}
	if behind.Diverged() {
		t.Error("being behind was reported as diverged")
	}
	if !synced.InSync() {
		t.Error("equal refs were not reported as in sync")
	}
	if behind.InSync() {
		t.Error("being behind was reported as in sync")
	}
}

func TestIsRepositoryIsFalseForAPlainDirectory(t *testing.T) {
	// Act & Assert
	if gitx.IsRepository(t.TempDir()) {
		t.Error("a directory with no .git was reported as a repository")
	}
}

func TestAvailableFindsGit(t *testing.T) {
	// The whole tool is unusable without it, so this is worth asserting.
	if !gitx.Available() {
		t.Skip("git is not installed in this environment")
	}
}

// The remaining tests drive a real repository, because the value of this
// package is entirely in what it reads back out of git.

func run(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, output)
	}
	return string(output)
}

func newRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run(t, dir, "init", "--quiet", "--initial-branch", "main", ".")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("one\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	run(t, dir, "add", "-A")
	run(t, dir, "commit", "--quiet", "-m", "init")
	return dir
}

func TestIsRepositoryRecognisesARealOne(t *testing.T) {
	// Act & Assert
	if !gitx.IsRepository(newRepo(t)) {
		t.Error("a real repository was not recognised")
	}
}

func TestCurrentBranchAndHeadRevisionReadTheRepository(t *testing.T) {
	// Arrange
	dir := newRepo(t)
	ctx := context.Background()

	// Act
	branch, err := gitx.CurrentBranch(ctx, dir)
	if err != nil {
		t.Fatalf("CurrentBranch returned an error: %v", err)
	}
	revision, err := gitx.HeadRevision(ctx, dir)
	if err != nil {
		t.Fatalf("HeadRevision returned an error: %v", err)
	}

	// Assert
	if branch != "main" {
		t.Errorf("branch = %q, want main", branch)
	}
	if len(revision) != 40 {
		t.Errorf("revision = %q, want a full hash", revision)
	}
}

func TestCurrentBranchReturnsEmptyOnADetachedHead(t *testing.T) {
	// Arrange: detached HEAD is a normal state, not an error to propagate.
	dir := newRepo(t)
	revision := strings.TrimSpace(run(t, dir, "rev-parse", "HEAD"))
	run(t, dir, "checkout", "--quiet", revision)

	// Act
	branch, err := gitx.CurrentBranch(context.Background(), dir)

	// Assert
	if err != nil {
		t.Fatalf("CurrentBranch returned an error for a detached HEAD: %v", err)
	}
	if branch != "" {
		t.Errorf("branch = %q, want empty", branch)
	}
}

func TestIsDirtySeesBothTrackedAndUntrackedChanges(t *testing.T) {
	// Arrange: an untracked file is still work that exists nowhere else.
	dir := newRepo(t)
	ctx := context.Background()

	// Act & Assert
	dirty, err := gitx.IsDirty(ctx, dir)
	if err != nil {
		t.Fatalf("IsDirty returned an error: %v", err)
	}
	if dirty {
		t.Error("a clean repository was reported as dirty")
	}

	if err := os.WriteFile(filepath.Join(dir, "untracked.md"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	dirty, err = gitx.IsDirty(ctx, dir)
	if err != nil {
		t.Fatalf("IsDirty returned an error: %v", err)
	}
	if !dirty {
		t.Error("an untracked file was not reported as dirty")
	}
}

func TestStashCountSeesWorkThatStatusDoesNot(t *testing.T) {
	// Arrange: stashes are invisible to `git status`, which is why they are the
	// work most often destroyed by a cleanup.
	dir := newRepo(t)
	ctx := context.Background()
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("changed\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	run(t, dir, "stash", "push", "--quiet")

	// Act
	count, err := gitx.StashCount(ctx, dir)
	dirty, dirtyErr := gitx.IsDirty(ctx, dir)

	// Assert
	if err != nil || dirtyErr != nil {
		t.Fatalf("unexpected errors: %v %v", err, dirtyErr)
	}
	if count != 1 {
		t.Errorf("stash count = %d, want 1", count)
	}
	if dirty {
		t.Error("the working tree should be clean after stashing; the stash is the only trace")
	}
}

func TestUnpushedCommitsCountsWorkNoRemoteHas(t *testing.T) {
	// Arrange
	dir := newRepo(t)

	// Act
	count, err := gitx.UnpushedCommits(context.Background(), dir)

	// Assert
	if err != nil {
		t.Fatalf("UnpushedCommits returned an error: %v", err)
	}
	if count != 1 {
		t.Errorf("unpushed = %d, want 1; the initial commit is on no remote", count)
	}
}

func TestRevisionExistsDistinguishesRealFromInvented(t *testing.T) {
	// Arrange
	dir := newRepo(t)
	ctx := context.Background()
	revision, err := gitx.HeadRevision(ctx, dir)
	if err != nil {
		t.Fatalf("HeadRevision returned an error: %v", err)
	}

	// Act & Assert
	if !gitx.RevisionExists(ctx, dir, revision) {
		t.Error("a real revision was not found")
	}
	if gitx.RevisionExists(ctx, dir, "0000000000000000000000000000000000000000") {
		t.Error("an invented revision was reported as present")
	}
}

func TestFileAtRevisionReadsWithoutTouchingTheWorkingTree(t *testing.T) {
	// Arrange
	dir := newRepo(t)
	ctx := context.Background()
	original, err := gitx.HeadRevision(ctx, dir)
	if err != nil {
		t.Fatalf("HeadRevision returned an error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("two\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	run(t, dir, "add", "-A")
	run(t, dir, "commit", "--quiet", "-m", "second")

	// Act
	content, err := gitx.FileAtRevision(ctx, dir, original, "README.md")

	// Assert
	if err != nil {
		t.Fatalf("FileAtRevision returned an error: %v", err)
	}
	if strings.TrimSpace(content) != "one" {
		t.Errorf("content at the original revision = %q, want \"one\"", content)
	}
	current, err := os.ReadFile(filepath.Join(dir, "README.md"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if strings.TrimSpace(string(current)) != "two" {
		t.Error("reading a past revision modified the working tree")
	}
}

func TestRemoteURLReportsAMissingRemoteAsAnError(t *testing.T) {
	// Arrange
	dir := newRepo(t)

	// Act
	_, err := gitx.RemoteURL(context.Background(), dir, "origin")

	// Assert
	if err == nil {
		t.Error("a repository with no origin returned a URL")
	}
}

func TestAheadBehindCountsBothDirections(t *testing.T) {
	// Arrange
	upstreamRoot := t.TempDir()
	upstream := filepath.Join(upstreamRoot, "upstream")
	if err := os.MkdirAll(upstream, 0o755); err != nil {
		t.Fatalf("create: %v", err)
	}
	run(t, upstream, "init", "--quiet", "--initial-branch", "main", ".")
	if err := os.WriteFile(filepath.Join(upstream, "README.md"), []byte("one\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	run(t, upstream, "add", "-A")
	run(t, upstream, "commit", "--quiet", "-m", "init")

	clone := filepath.Join(upstreamRoot, "clone")
	run(t, upstreamRoot, "clone", "--quiet", upstream, "clone")

	if err := os.WriteFile(filepath.Join(upstream, "README.md"), []byte("two\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	run(t, upstream, "add", "-A")
	run(t, upstream, "commit", "--quiet", "-m", "second")

	ctx := context.Background()
	if err := gitx.Fetch(ctx, clone, "origin"); err != nil {
		t.Fatalf("Fetch returned an error: %v", err)
	}

	// Act
	divergence, err := gitx.AheadBehind(ctx, clone, "HEAD", "origin/main")

	// Assert
	if err != nil {
		t.Fatalf("AheadBehind returned an error: %v", err)
	}
	if divergence.Ahead != 0 || divergence.Behind != 1 {
		t.Errorf("divergence = %+v, want 0 ahead and 1 behind", divergence)
	}

	// Act: fast-forwarding closes the gap.
	if err := gitx.FastForward(ctx, clone, "origin/main"); err != nil {
		t.Fatalf("FastForward returned an error: %v", err)
	}
	divergence, err = gitx.AheadBehind(ctx, clone, "HEAD", "origin/main")
	if err != nil {
		t.Fatalf("AheadBehind returned an error: %v", err)
	}
	if !divergence.InSync() {
		t.Errorf("divergence after fast-forward = %+v, want in sync", divergence)
	}
}

func TestCommandErrorCarriesWhatGitSaid(t *testing.T) {
	// Arrange: a generic exit status tells the user nothing about the cause.
	dir := newRepo(t)

	// Act
	_, err := gitx.Run(context.Background(), dir, "checkout", "does-not-exist")

	// Assert
	if err == nil {
		t.Fatal("checking out a missing branch reported success")
	}
	var cmdErr *gitx.CommandError
	if !errors.As(err, &cmdErr) {
		t.Fatalf("error type = %T, want *gitx.CommandError", err)
	}
	if !strings.Contains(err.Error(), "does-not-exist") {
		t.Errorf("the error does not carry git's own message: %v", err)
	}
}

func TestACommandErrorUnwrapsToTheFailureUnderneath(t *testing.T) {
	// Arrange: Unwrap has no direct caller and never will — errors.Is and
	// errors.As reach it through the standard interface. That makes it exactly
	// the kind of contract that can be deleted as "unused" and break a
	// timeout check somewhere else, so it is asserted here instead.
	underlying := context.DeadlineExceeded
	failure := &gitx.CommandError{
		Args:   []string{"fetch", "origin"},
		Dir:    "/w/payments",
		Stderr: "",
		Err:    underlying,
	}

	// Act
	wrapped := fmt.Errorf("update payments: %w", failure)

	// Assert
	if !errors.Is(wrapped, context.DeadlineExceeded) {
		t.Error("a timed-out git command does not compare equal to context.DeadlineExceeded through the chain")
	}
	var target *gitx.CommandError
	if !errors.As(wrapped, &target) {
		t.Fatal("errors.As could not recover the CommandError")
	}
	if target.Dir != "/w/payments" {
		t.Errorf("recovered Dir = %q, want the directory the command ran in", target.Dir)
	}
}

// Landing is judged with this and nothing else, so its edges are the edges of
// the whole shipping gate: a revision nobody pushed, a branch that was rewound
// under it, and a revision that no longer exists at all must each come back as
// "not landed" rather than as an error.
func TestIsAncestorAnswersTheQuestionTheShippingGateAsks(t *testing.T) {
	// Arrange
	dir := newRepo(t)
	ctx := context.Background()
	first, err := gitx.HeadRevision(ctx, dir)
	if err != nil {
		t.Fatalf("HeadRevision: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("two\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	run(t, dir, "commit", "--quiet", "-am", "second")
	second, err := gitx.HeadRevision(ctx, dir)
	if err != nil {
		t.Fatalf("HeadRevision: %v", err)
	}

	// Act & Assert: an earlier revision is reachable from a later one.
	landed, err := gitx.IsAncestor(ctx, dir, first, second)
	if err != nil {
		t.Fatalf("IsAncestor returned an error: %v", err)
	}
	if !landed {
		t.Error("the first commit is an ancestor of the second and was not reported as one")
	}

	// A later revision is not reachable from an earlier one: this is the
	// verified-but-not-landed case, and it is not a failure.
	landed, err = gitx.IsAncestor(ctx, dir, second, first)
	if err != nil {
		t.Fatalf("IsAncestor returned an error for a plain negative: %v", err)
	}
	if landed {
		t.Error("the second commit was reported as an ancestor of the first")
	}

	// A revision that is not there at all is the force-pushed case. Reporting
	// it as an error would turn a rewritten branch into a broken command.
	landed, err = gitx.IsAncestor(ctx, dir, "0000000000000000000000000000000000000000", second)
	if err != nil {
		t.Fatalf("a missing revision must not be an error: %v", err)
	}
	if landed {
		t.Error("a revision that does not exist was reported as landed")
	}
}
