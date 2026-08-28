package syncx_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/takealook97/vat/internal/manifest"
	"github.com/takealook97/vat/internal/syncx"
	"github.com/takealook97/vat/internal/workspace"
)

// git runs a git command in dir and fails the test if it errors.
func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, output)
	}
	return string(output)
}

// readNormalised reads a file with line endings collapsed to LF. On Windows,
// git's autocrlf setting rewrites them on checkout, and the behaviour under test
// is that the tree advanced — not which newline convention the platform uses.
func readNormalised(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return strings.ReplaceAll(string(content), "\r\n", "\n")
}

func commit(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-m", "change "+name)
}

// fixture builds a workspace with one repository cloned from a local upstream,
// which makes fetch and fast-forward real operations without a network.
func fixture(t *testing.T) (*workspace.Workspace, string, string) {
	t.Helper()
	base := t.TempDir()
	upstream := filepath.Join(base, "upstream")
	if err := os.MkdirAll(upstream, 0o755); err != nil {
		t.Fatalf("create: %v", err)
	}
	git(t, upstream, "init", "--quiet", "--initial-branch", "main", ".")
	commit(t, upstream, "README.md", "one\n")

	root := filepath.Join(base, "workspace")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("create: %v", err)
	}
	clone := filepath.Join(root, "service")
	git(t, root, "clone", "--quiet", upstream, "service")

	built := manifest.Default("test")
	built = manifest.WithRepo(built, manifest.Repo{
		Name: "service", Origin: upstream, Role: manifest.RoleProduct, Required: true,
	})
	if err := manifest.Save(filepath.Join(root, manifest.FileName), built); err != nil {
		t.Fatalf("Save: %v", err)
	}
	ws, err := workspace.OpenAt(root)
	if err != nil {
		t.Fatalf("OpenAt: %v", err)
	}
	return ws, upstream, clone
}

func runSync(t *testing.T, ws *workspace.Workspace, opts syncx.Options) syncx.Result {
	t.Helper()
	report := syncx.Run(context.Background(), ws, ws.Manifest.Active(), opts)
	if len(report.Results) != 1 {
		t.Fatalf("result count = %d, want 1", len(report.Results))
	}
	return report.Results[0]
}

func TestACleanBranchBehindUpstreamIsFastForwarded(t *testing.T) {
	// Arrange
	ws, upstream, clone := fixture(t)
	commit(t, upstream, "README.md", "two\n")

	// Act
	result := runSync(t, ws, syncx.Options{})

	// Assert
	if result.State != syncx.StateUpdated {
		t.Fatalf("state = %s, want UPDATED (detail: %s)", result.State, result.Detail)
	}
	if got := readNormalised(t, filepath.Join(clone, "README.md")); got != "two\n" {
		t.Errorf("working tree was not advanced: %q", got)
	}
}

func TestADirtyWorkingTreeIsReportedAndLeftUntouched(t *testing.T) {
	// Arrange: a sync that stashed or reset here would destroy work that exists
	// nowhere else.
	ws, upstream, clone := fixture(t)
	commit(t, upstream, "README.md", "two\n")
	if err := os.WriteFile(filepath.Join(clone, "README.md"), []byte("local edit\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Act
	result := runSync(t, ws, syncx.Options{})

	// Assert
	if result.State != syncx.StateDirty {
		t.Fatalf("state = %s, want DIRTY", result.State)
	}
	if result.State.Failure() {
		t.Error("a dirty tree is a normal working state, not a failure")
	}
	if got := readNormalised(t, filepath.Join(clone, "README.md")); got != "local edit\n" {
		t.Errorf("the local edit was destroyed: %q", got)
	}
}

func TestAFeatureBranchIsReportedAndNeverCheckedOutAway(t *testing.T) {
	// Arrange
	ws, upstream, clone := fixture(t)
	commit(t, upstream, "README.md", "two\n")
	git(t, clone, "checkout", "--quiet", "-b", "feature")

	// Act
	result := runSync(t, ws, syncx.Options{})

	// Assert
	if result.State != syncx.StateBranch {
		t.Fatalf("state = %s, want BRANCH", result.State)
	}
	if current := git(t, clone, "rev-parse", "--abbrev-ref", "HEAD"); current[:7] != "feature" {
		t.Errorf("the branch was changed to %q", current)
	}
}

func TestLocalCommitsAheadAreReportedAndNeverPushed(t *testing.T) {
	// Arrange
	ws, _, clone := fixture(t)
	commit(t, clone, "local.md", "mine\n")

	// Act
	result := runSync(t, ws, syncx.Options{})

	// Assert
	if result.State != syncx.StateAhead {
		t.Fatalf("state = %s, want AHEAD", result.State)
	}
	if result.Ahead != 1 {
		t.Errorf("ahead = %d, want 1", result.Ahead)
	}
}

func TestDivergedHistoryIsAFailureRatherThanAnAutomaticMerge(t *testing.T) {
	// Arrange
	ws, upstream, clone := fixture(t)
	commit(t, upstream, "README.md", "theirs\n")
	commit(t, clone, "README.md", "mine\n")

	// Act
	result := runSync(t, ws, syncx.Options{})

	// Assert
	if result.State != syncx.StateDiverged {
		t.Fatalf("state = %s, want DIVERGED", result.State)
	}
	if !result.State.Failure() {
		t.Error("diverged history must fail the run; merging here would guess at intent")
	}
}

func TestAnOriginPointingElsewhereFailsAndIsNeverRewritten(t *testing.T) {
	// Arrange
	ws, _, clone := fixture(t)
	git(t, clone, "remote", "set-url", "origin", "https://example.com/somewhere-else.git")

	// Act
	result := runSync(t, ws, syncx.Options{})

	// Assert
	if result.State != syncx.StateRemoteMismatch {
		t.Fatalf("state = %s, want REMOTE_MISMATCH", result.State)
	}
	after := git(t, clone, "remote", "get-url", "origin")
	if after[:5] != "https" {
		t.Errorf("the remote was rewritten to %q; a mismatch is a supply-chain signal", after)
	}
}

func TestAMissingRepositoryIsCloned(t *testing.T) {
	// Arrange
	ws, _, clone := fixture(t)
	if err := os.RemoveAll(clone); err != nil {
		t.Fatalf("remove: %v", err)
	}

	// Act
	result := runSync(t, ws, syncx.Options{})

	// Assert
	if result.State != syncx.StateCloned {
		t.Fatalf("state = %s, want CLONED (detail: %s)", result.State, result.Detail)
	}
	if _, err := os.Stat(filepath.Join(clone, "README.md")); err != nil {
		t.Errorf("the clone is missing its content: %v", err)
	}
}

func TestADirectoryThatIsNotARepositoryIsNeverOverwritten(t *testing.T) {
	// Arrange: it may hold work that was never committed anywhere.
	ws, _, clone := fixture(t)
	if err := os.RemoveAll(clone); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if err := os.MkdirAll(clone, 0o755); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := os.WriteFile(filepath.Join(clone, "notes.txt"), []byte("unsaved\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Act
	result := runSync(t, ws, syncx.Options{})

	// Assert
	if result.State != syncx.StateNotGit {
		t.Fatalf("state = %s, want NOT_GIT", result.State)
	}
	if _, err := os.Stat(filepath.Join(clone, "notes.txt")); err != nil {
		t.Errorf("the existing directory was destroyed: %v", err)
	}
}

func TestADryRunChangesNothingAndContactsNothing(t *testing.T) {
	// Arrange
	ws, upstream, clone := fixture(t)
	commit(t, upstream, "README.md", "two\n")
	before := git(t, clone, "rev-parse", "HEAD")

	// Act
	result := runSync(t, ws, syncx.Options{DryRun: true})

	// Assert
	if result.State != syncx.StatePlanned {
		t.Fatalf("state = %s, want PLANNED", result.State)
	}
	if after := git(t, clone, "rev-parse", "HEAD"); after != before {
		t.Error("a dry run advanced the repository")
	}
}

func TestARepositoryOnADeclaredNonDefaultBranchIsStillAdvanced(t *testing.T) {
	// Arrange: a repository whose default is "develop" would otherwise be
	// skipped forever with a note nobody reads.
	ws, upstream, clone := fixture(t)
	git(t, upstream, "checkout", "--quiet", "-b", "develop")
	commit(t, upstream, "README.md", "two\n")
	git(t, clone, "fetch", "--quiet", "origin")
	git(t, clone, "checkout", "--quiet", "-b", "develop", "--track", "origin/develop")
	git(t, clone, "reset", "--quiet", "--hard", "HEAD~1")

	repo, _ := ws.Manifest.Find("service")
	repo.DefaultBranch = "develop"
	ws.Manifest = manifest.WithRepo(ws.Manifest, repo)

	// Act
	result := runSync(t, ws, syncx.Options{})

	// Assert
	if result.State != syncx.StateUpdated {
		t.Fatalf("state = %s, want UPDATED (detail: %s)", result.State, result.Detail)
	}
}

func TestAnArchivedRepositoryIsExcludedFromUpdates(t *testing.T) {
	// Arrange
	ws, _, _ := fixture(t)
	repo, _ := ws.Manifest.Find("service")
	repo.Archived = true
	ws.Manifest = manifest.WithRepo(ws.Manifest, repo)

	// Act
	report := syncx.Run(context.Background(), ws, ws.Manifest.Repos, syncx.Options{})

	// Assert
	if report.Results[0].State != syncx.StateArchived {
		t.Errorf("state = %s, want ARCHIVED", report.Results[0].State)
	}
	if report.Failures != 0 {
		t.Errorf("an archived repository counted as a failure")
	}
}

// missingRepoReport runs sync against a manifest entry that has no clone on
// disk, which is the normal state of a fresh machine.
func missingRepoReport(t *testing.T, required bool) syncx.Report {
	t.Helper()
	root := t.TempDir()
	repo := manifest.Repo{
		Name: "absent", Origin: "https://example.invalid/acme/absent.git",
		Role: manifest.RoleProduct, Required: required,
	}
	built := manifest.WithRepo(manifest.Default("test"), repo)
	if err := manifest.Save(filepath.Join(root, manifest.FileName), built); err != nil {
		t.Fatalf("Save: %v", err)
	}
	ws, err := workspace.OpenAt(root)
	if err != nil {
		t.Fatalf("OpenAt: %v", err)
	}
	return syncx.Run(t.Context(), ws, []manifest.Repo{repo},
		syncx.Options{Offline: true, Remote: "origin"})
}

func TestAnOptionalRepositoryThatIsNotClonedDoesNotFailTheRun(t *testing.T) {
	// Arrange: the manifest documents `required: false` as "a missing clone is a
	// warning rather than a failure", and lint has always drawn that
	// distinction. sync never read the field, so an optional repository broke
	// every run it was absent from -- --offline most of all, where it can never
	// be cloned.

	// Act
	report := missingRepoReport(t, false)

	// Assert
	if len(report.Results) != 1 {
		t.Fatalf("expected one result, got %+v", report.Results)
	}
	if report.Results[0].State != syncx.StateMissing {
		t.Errorf("an uncloned repository is %q, want MISSING", report.Results[0].State)
	}
	if report.Failures != 0 {
		t.Errorf("an optional repository nobody promised would be here failed the run: %+v",
			report.Results)
	}
}

func TestARequiredRepositoryThatIsNotClonedStillFailsTheRun(t *testing.T) {
	// Arrange: the distinction only means something if the other side holds.

	// Act
	report := missingRepoReport(t, true)

	// Assert
	if report.Failures == 0 {
		t.Errorf("a required repository that is not on disk passed the run: %+v", report.Results)
	}
}

// `vat repo new --no-remote` deliberately leaves a repository with no remote,
// and `vat init --adopt` records one it finds that way. sync reported both as
// REMOTE_MISMATCH — whose whole meaning is that origin points somewhere the
// manifest does not name, which is a supply-chain signal — and counted them as
// needing attention forever. `vat lint` already made this distinction and says
// in a comment why.

// `vat repo new --no-remote` deliberately leaves a repository with no remote,
// and `vat init --adopt` records one it finds that way as a local placeholder.
// sync reported both as REMOTE_MISMATCH — whose whole meaning is that origin
// points somewhere the manifest does not name, a supply-chain signal — and
// counted them as failing every run forever. `vat lint` already drew this
// distinction and says in a comment why.
func TestARepositoryTheManifestRecordsAsLocalIsNotASupplyChainSignal(t *testing.T) {
	// Arrange
	ws, _, clone := fixture(t)
	git(t, clone, "remote", "remove", "origin")
	local := ws.Manifest.Active()[0]
	local.Origin = manifest.LocalOrigin(local.Name)

	// Act
	report := syncx.Run(context.Background(), ws, []manifest.Repo{local}, syncx.Options{Offline: true})

	// Assert
	result := report.Results[0]
	if result.State != syncx.StateNoRemote {
		t.Fatalf("state = %s, want NO_REMOTE (detail: %s)", result.State, result.Detail)
	}
	if result.Failed() {
		t.Error("a repository the manifest itself records as having no remote failed the run")
	}
}

// A clone that no longer has the remote the manifest names is a different fact.
// Nobody can sync it, and it is still reported.
func TestAMissingRemoteTheManifestNamesIsStillReported(t *testing.T) {
	// Arrange
	ws, _, clone := fixture(t)
	git(t, clone, "remote", "remove", "origin")

	// Act
	result := runSync(t, ws, syncx.Options{Offline: true})

	// Assert
	if result.State != syncx.StateRemoteMismatch {
		t.Fatalf("state = %s, want REMOTE_MISMATCH (detail: %s)", result.State, result.Detail)
	}
	if !result.Failed() {
		t.Error("a repository whose declared origin is not configured did not fail the run")
	}
}

// The reference table and the states the code can report are compared both
// ways, for the reason the lint rule table already is: a state nobody has
// documented is a state nobody can look up when it appears in their terminal.
func TestTheStateNamesAreExactlyWhatTheReferenceDocuments(t *testing.T) {
	// Arrange
	content, err := os.ReadFile("../../docs/COMMANDS.md")
	if err != nil {
		t.Fatalf("read the reference: %v", err)
	}
	reference := string(content)

	// Act & Assert
	documented := map[string]bool{}
	for _, row := range strings.Split(reference, "\n") {
		if match := stateRow.FindStringSubmatch(row); match != nil {
			documented[match[1]] = true
		}
	}
	for _, state := range syncx.StateNames() {
		if !documented[string(state)] {
			t.Errorf("`vat sync` can report %s and docs/COMMANDS.md does not list it", state)
		}
		delete(documented, string(state))
	}
	for state := range documented {
		t.Errorf("docs/COMMANDS.md documents the sync state %s, which the code cannot report", state)
	}
}

var stateRow = regexp.MustCompile("^\\| `([A-Z_]+)` \\|")

// A dirty tree invites committing your way out of it. When the tree is dirty
// because a merge stopped part of the way through, what that commits is a file
// full of conflict markers — so sync says which it is.
func TestADirtyTreeSaysWhenAnOperationWasLeftUnfinished(t *testing.T) {
	// Arrange
	ws, _, clone := fixture(t)
	git(t, clone, "checkout", "--quiet", "-b", "side")
	commit(t, clone, "README.md", "side\n")
	git(t, clone, "checkout", "--quiet", "main")
	commit(t, clone, "README.md", "main\n")
	merge := exec.Command("git", "merge", "side")
	merge.Dir = clone
	// Expected to fail; that failure is the state under test.
	_ = merge.Run()

	// Act
	result := runSync(t, ws, syncx.Options{Offline: true})

	// Assert
	if result.State != syncx.StateDirty {
		t.Fatalf("state = %s, want DIRTY (detail: %s)", result.State, result.Detail)
	}
	if !strings.Contains(result.Detail, "unfinished merge") {
		t.Errorf("detail = %q; it does not say the merge was left unfinished", result.Detail)
	}
}

// Every row reports the branch the repository is actually on. Reporting the one
// the manifest declares makes `vat sync` and `vat status` disagree about the
// same repository at the same moment, and the row that is wrong is the one
// somebody would use to decide the manifest is right.
func TestTheBranchReportedIsTheOneTheRepositoryIsActuallyOn(t *testing.T) {
	// Arrange
	ws, _, clone := fixture(t)
	git(t, clone, "remote", "remove", "origin")
	git(t, clone, "checkout", "--quiet", "-b", "feature")
	local := ws.Manifest.Active()[0]
	local.Origin = manifest.LocalOrigin(local.Name)
	local.DefaultBranch = "declared-elsewhere"

	// Act
	report := syncx.Run(context.Background(), ws, []manifest.Repo{local}, syncx.Options{Offline: true})

	// Assert
	result := report.Results[0]
	if result.State != syncx.StateNoRemote {
		t.Fatalf("state = %s, want NO_REMOTE (detail: %s)", result.State, result.Detail)
	}
	if result.Branch != "feature" {
		t.Errorf("branch = %q, want the branch the clone is on", result.Branch)
	}
}

// A fast-forward cannot destroy an untracked file: git refuses the merge and
// errors, which this package reports. Gating on untracked files instead made
// the first sync of a new workspace report every repository as DIRTY — the only
// uncommitted change being the AGENTS.md vat had just rendered into it.
func TestUntrackedFilesAloneDoNotBlockAFastForward(t *testing.T) {
	// Arrange
	ws, upstream, clone := fixture(t)
	commit(t, upstream, "README.md", "two\n")
	if err := os.WriteFile(filepath.Join(clone, "AGENTS.md"), []byte("# service\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Act
	result := runSync(t, ws, syncx.Options{})

	// Assert
	if result.State != syncx.StateUpdated {
		t.Fatalf("state = %s, want UPDATED (detail: %s)", result.State, result.Detail)
	}
	if got := readNormalised(t, filepath.Join(clone, "AGENTS.md")); got != "# service\n" {
		t.Errorf("the untracked file was not left alone: %q", got)
	}
}

// The gate still holds for the case it exists for.
func TestAModifiedTrackedFileStillBlocksAFastForward(t *testing.T) {
	// Arrange
	ws, upstream, clone := fixture(t)
	commit(t, upstream, "README.md", "two\n")
	if err := os.WriteFile(filepath.Join(clone, "README.md"), []byte("mine\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.WriteFile(filepath.Join(clone, "AGENTS.md"), []byte("# service\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Act
	result := runSync(t, ws, syncx.Options{})

	// Assert
	if result.State != syncx.StateDirty {
		t.Fatalf("state = %s, want DIRTY (detail: %s)", result.State, result.Detail)
	}
	if got := readNormalised(t, filepath.Join(clone, "README.md")); got != "mine\n" {
		t.Errorf("uncommitted work was overwritten: %q", got)
	}
}

func TestARewrittenUpstreamIsNamedRatherThanCalledOrdinaryDivergence(t *testing.T) {
	// Arrange: a force-pushed upstream is not a divergence to reconcile. It is a
	// different history under the same name, and the remedy is a fresh clone —
	// telling somebody to inspect the difference sends them to compare two
	// unrelated trees. This is what a repository whose history was rewritten
	// looks like to everyone who already had a clone.
	ws, upstream, clone := fixture(t)
	git(t, upstream, "checkout", "--quiet", "--orphan", "rewritten")
	commit(t, upstream, "README.md", "rewritten\n")
	git(t, upstream, "branch", "--quiet", "-M", "rewritten", "main")
	commit(t, clone, "LOCAL.md", "mine\n")

	// Act
	result := runSync(t, ws, syncx.Options{})

	// Assert
	if result.State != syncx.StateDiverged {
		t.Fatalf("state = %s, want DIVERGED; nothing here can be fast-forwarded either way",
			result.State)
	}
	if !strings.Contains(result.Detail, "clone again") {
		t.Errorf("the detail does not name the only supported remedy: %q", result.Detail)
	}
}
