package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/takealook97/vat/internal/ui"
)

var testNow = time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)

// workspaceFixture drives real commands against a real workspace on disk. The CLI is the
// contract users depend on, so these exercise it end to end rather than
// asserting on the packages underneath.
type workspaceFixture struct {
	t    *testing.T
	root string
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, output)
	}
}

// newHarness creates a workspace root with the named repositories already
// cloned from local upstreams, so fetch and fast-forward are real operations.
func newFixture(t *testing.T, repos ...string) *workspaceFixture {
	t.Helper()
	base := t.TempDir()
	root := filepath.Join(base, "workspace")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("create: %v", err)
	}
	git(t, root, "init", "--quiet", "--initial-branch", "main", ".")

	for _, name := range repos {
		upstream := filepath.Join(base, "upstream", name)
		if err := os.MkdirAll(upstream, 0o755); err != nil {
			t.Fatalf("create: %v", err)
		}
		git(t, upstream, "init", "--quiet", "--initial-branch", "main", ".")
		if err := os.WriteFile(filepath.Join(upstream, "README.md"), []byte("# "+name+"\n"), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
		git(t, upstream, "add", "-A")
		git(t, upstream, "commit", "--quiet", "-m", "init")
		git(t, root, "clone", "--quiet", upstream, name)
	}
	return &workspaceFixture{t: t, root: root}
}

// run executes one vat invocation and returns its exit code and output.
func (h *workspaceFixture) run(args ...string) (int, string) {
	h.t.Helper()
	var out, errOut bytes.Buffer
	env := &Env{
		Printer: ui.NewWith(&out, &errOut, false),
		Now:     testNow,
		Cwd:     h.root,
		Root:    h.root,
		Yes:     true,
	}
	code := dispatch(context.Background(), env, Root(), args, nil)
	return code, out.String() + errOut.String()
}

// runJSON executes an invocation with --json and decodes the payload.
func (h *workspaceFixture) runJSON(target any, args ...string) int {
	h.t.Helper()
	var out, errOut bytes.Buffer
	env := &Env{
		Printer: ui.NewWith(&out, &errOut, false),
		Now:     testNow,
		Cwd:     h.root,
		Root:    h.root,
		JSON:    true,
		Yes:     true,
	}
	code := dispatch(context.Background(), env, Root(), args, nil)
	if strings.TrimSpace(out.String()) == "" {
		h.t.Fatalf("--json produced no output for %v (stderr: %s)", args, errOut.String())
	}
	if err := json.Unmarshal(out.Bytes(), target); err != nil {
		h.t.Fatalf("--json output for %v is not valid JSON: %v\n%s", args, err, out.String())
	}
	return code
}

func (h *workspaceFixture) mustRun(args ...string) string {
	h.t.Helper()
	code, output := h.run(args...)
	if code != ExitOK {
		h.t.Fatalf("`vat %s` exited %d, want 0:\n%s", strings.Join(args, " "), code, output)
	}
	return output
}

func (h *workspaceFixture) path(parts ...string) string {
	return filepath.Join(append([]string{h.root}, parts...)...)
}

func TestInitAdoptEnrolsWhatIsAlreadyPresent(t *testing.T) {
	// Arrange
	h := newFixture(t, "payments", "console")

	// Act
	output := h.mustRun("init", "--name", "acme", "--adopt")

	// Assert
	for _, want := range []string{"payments", "console", "vat.yaml"} {
		if !strings.Contains(output, want) {
			t.Errorf("init output does not mention %q:\n%s", want, output)
		}
	}
	for _, file := range []string{"vat.yaml", "AGENTS.md", "CLAUDE.md", ".gitignore"} {
		if _, err := os.Stat(h.path(file)); err != nil {
			t.Errorf("init did not create %s: %v", file, err)
		}
	}
	ignore, err := os.ReadFile(h.path(".gitignore"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	for _, name := range []string{"payments/", "console/"} {
		if !strings.Contains(string(ignore), name) {
			t.Errorf(".gitignore does not exclude %s; a root commit would swallow it", name)
		}
	}
}

func TestInitRefusesToOverwriteAnExistingWorkspace(t *testing.T) {
	// Arrange
	h := newFixture(t, "payments")
	h.mustRun("init", "--name", "acme", "--adopt")

	// Act
	code, output := h.run("init", "--name", "other")

	// Assert
	if code != ExitUsage {
		t.Errorf("exit code = %d, want %d (usage)", code, ExitUsage)
	}
	if !strings.Contains(output, "already exists") {
		t.Errorf("the error does not say why it refused:\n%s", output)
	}
}

func TestStatusReportsEveryRepositoryAndItsBranch(t *testing.T) {
	// Arrange
	h := newFixture(t, "payments", "console")
	h.mustRun("init", "--name", "acme", "--adopt")

	// Act
	output := h.mustRun("status")

	// Assert
	for _, want := range []string{"payments", "console", "main"} {
		if !strings.Contains(output, want) {
			t.Errorf("status omits %q:\n%s", want, output)
		}
	}
}

func TestStatusJSONIsMachineReadable(t *testing.T) {
	// Arrange
	h := newFixture(t, "payments")
	h.mustRun("init", "--name", "acme", "--adopt")

	// Act
	var statuses []struct {
		Name    string `json:"name"`
		Present bool   `json:"present"`
		Branch  string `json:"branch"`
	}
	code := h.runJSON(&statuses, "status")

	// Assert
	if code != ExitOK {
		t.Errorf("exit code = %d, want 0", code)
	}
	if len(statuses) != 1 || statuses[0].Name != "payments" || !statuses[0].Present {
		t.Errorf("status JSON = %+v, want one present repository", statuses)
	}
}

func TestSyncLeavesADirtyRepositoryAlone(t *testing.T) {
	// Arrange: a sync that stashed here would destroy work that exists nowhere
	// else.
	h := newFixture(t, "payments")
	h.mustRun("init", "--name", "acme", "--adopt")
	edited := h.path("payments", "README.md")
	if err := os.WriteFile(edited, []byte("local edit\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Act
	code, output := h.run("sync")

	// Assert
	if code != ExitOK {
		t.Errorf("a dirty tree is a normal working state, not a failure; exit code = %d\n%s", code, output)
	}
	if !strings.Contains(output, "DIRTY") {
		t.Errorf("sync did not report the dirty repository:\n%s", output)
	}
	after, err := os.ReadFile(edited)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(after) != "local edit\n" {
		t.Errorf("sync destroyed the local edit: %q", after)
	}
}

func TestSyncDryRunChangesNothing(t *testing.T) {
	// Arrange
	h := newFixture(t, "payments")
	h.mustRun("init", "--name", "acme", "--adopt")

	// Act
	output := h.mustRun("sync", "--dry-run")

	// Assert
	if !strings.Contains(output, "PLANNED") {
		t.Errorf("a dry run did not report a plan:\n%s", output)
	}
}

func TestLintFindsGitignoreDriftAndFixRepairsIt(t *testing.T) {
	// Arrange
	h := newFixture(t, "payments")
	h.mustRun("init", "--name", "acme", "--adopt")
	if err := os.WriteFile(h.path(".gitignore"), []byte("# emptied\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Act
	code, output := h.run("lint", "--offline")

	// Assert
	if code != ExitFindings {
		t.Errorf("exit code = %d, want %d (found problems)", code, ExitFindings)
	}
	if !strings.Contains(output, "gitignore-drift") {
		t.Errorf("lint did not report the drift:\n%s", output)
	}

	// Act again: --fix should repair it and the next run should be clean.
	h.mustRun("lint", "--fix", "--offline")
	code, output = h.run("lint", "--offline")

	// Assert
	if code != ExitOK {
		t.Errorf("lint still fails after --fix (exit %d):\n%s", code, output)
	}
}

func TestLintListNamesEveryRule(t *testing.T) {
	// Arrange: an unlisted rule cannot be selected with --only or documented.
	h := newFixture(t)
	h.mustRun("init", "--name", "acme")

	// Act
	output := h.mustRun("lint", "--list")

	// Assert
	if count := strings.Count(strings.TrimSpace(output), "\n") + 1; count < 15 {
		t.Errorf("--list printed %d rules, expected the full set:\n%s", count, output)
	}
}

func TestRepoAddThenRemoveKeepsTheManifestAndGitignoreInStep(t *testing.T) {
	// Arrange
	h := newFixture(t, "payments")
	h.mustRun("init", "--name", "acme", "--adopt")

	// Act
	h.mustRun("repo", "add", "console", "--origin", "https://example.com/acme/console.git", "--no-clone")

	// Assert
	manifestText, err := os.ReadFile(h.path("vat.yaml"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(manifestText), "console") {
		t.Fatalf("the manifest does not list the added repository:\n%s", manifestText)
	}
	ignore, err := os.ReadFile(h.path(".gitignore"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(ignore), "console/") {
		t.Errorf(".gitignore was not updated alongside the manifest:\n%s", ignore)
	}

	// Act: removing takes both entries away again.
	h.mustRun("repo", "remove", "console")

	// Assert
	manifestText, err = os.ReadFile(h.path("vat.yaml"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if strings.Contains(string(manifestText), "console") {
		t.Errorf("the manifest still lists the removed repository:\n%s", manifestText)
	}
}

func TestRepoRemoveRefusesWhileUnpushedWorkExists(t *testing.T) {
	// Arrange: unpushed commits exist nowhere else.
	h := newFixture(t, "payments")
	h.mustRun("init", "--name", "acme", "--adopt")
	if err := os.WriteFile(h.path("payments", "new.md"), []byte("work\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	git(t, h.path("payments"), "add", "-A")
	git(t, h.path("payments"), "commit", "--quiet", "-m", "local work")

	// Act
	code, output := h.run("repo", "remove", "payments")

	// Assert
	if code != ExitFindings {
		t.Errorf("exit code = %d, want %d; removal must refuse", code, ExitFindings)
	}
	if !strings.Contains(output, "not on any remote") {
		t.Errorf("the refusal does not say what would be lost:\n%s", output)
	}
	manifestText, err := os.ReadFile(h.path("vat.yaml"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(manifestText), "payments") {
		t.Error("the repository was removed from the manifest despite the refusal")
	}
}

func TestRepoArchiveExcludesFromDailyCommands(t *testing.T) {
	// Arrange
	h := newFixture(t, "payments", "console")
	h.mustRun("init", "--name", "acme", "--adopt")

	// Act
	h.mustRun("repo", "archive", "console")
	output := h.mustRun("status")

	// Assert
	if strings.Contains(output, "console") {
		t.Errorf("an archived repository appeared in status:\n%s", output)
	}

	// Act: and comes back.
	h.mustRun("repo", "unarchive", "console")
	output = h.mustRun("status")

	// Assert
	if !strings.Contains(output, "console") {
		t.Errorf("unarchive did not restore the repository:\n%s", output)
	}
}

func TestRepoRenameMovesTheDirectoryAndTheManifestTogether(t *testing.T) {
	// Arrange
	h := newFixture(t, "payments")
	h.mustRun("init", "--name", "acme", "--adopt")

	// Act
	h.mustRun("repo", "rename", "payments", "billing")

	// Assert
	if _, err := os.Stat(h.path("billing")); err != nil {
		t.Errorf("the directory was not renamed: %v", err)
	}
	if _, err := os.Stat(h.path("payments")); err == nil {
		t.Error("the old directory still exists")
	}
	manifestText, err := os.ReadFile(h.path("vat.yaml"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(manifestText), "billing") {
		t.Errorf("the manifest was not renamed:\n%s", manifestText)
	}
}

func TestHarnessRenderIsIdempotentAndCheckAgrees(t *testing.T) {
	// Arrange
	h := newFixture(t, "payments")
	h.mustRun("init", "--name", "acme", "--adopt")

	// Act
	output := h.mustRun("harness", "render")

	// Assert
	if !strings.Contains(output, "already current") {
		t.Errorf("init should have left the harness current:\n%s", output)
	}
	h.mustRun("harness", "check")
}

func TestHarnessCheckReportsDriftAfterTheManifestChanges(t *testing.T) {
	// Arrange
	h := newFixture(t, "payments")
	h.mustRun("init", "--name", "acme", "--adopt")
	h.mustRun("repo", "add", "console", "--origin", "https://example.com/acme/console.git", "--no-clone")

	// Corrupt the generated region by hand.
	contract, err := os.ReadFile(h.path("AGENTS.md"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	edited := strings.Replace(string(contract), "payments", "not-a-repository", 1)
	if err := os.WriteFile(h.path("AGENTS.md"), []byte(edited), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Act
	code, output := h.run("harness", "check")

	// Assert
	if code != ExitFindings {
		t.Errorf("exit code = %d, want %d; drift must be reported", code, ExitFindings)
	}
	if !strings.Contains(output, "drift") {
		t.Errorf("the finding does not mention drift:\n%s", output)
	}
}

func TestHarnessRoleNewGeneratesReadOnlyAdapters(t *testing.T) {
	// Arrange: a role that declares no write target must not be handed write
	// capability by any runtime.
	h := newFixture(t)
	h.mustRun("init", "--name", "acme")

	// Act
	h.mustRun("harness", "role", "new", "auditor", "--description", "Audits contracts.")

	// Assert
	for _, path := range []string{
		filepath.Join(".agents", "roles", "auditor.md"),
		filepath.Join(".claude", "agents", "auditor.md"),
		filepath.Join(".codex", "agents", "auditor.toml"),
	} {
		content, err := os.ReadFile(h.path(path))
		if err != nil {
			t.Errorf("%s was not generated: %v", path, err)
			continue
		}
		if !strings.Contains(string(content), "read-only") {
			t.Errorf("%s does not declare the role read-only:\n%s", path, content)
		}
	}
}

func TestFitRecommendsNothingAtASmallScale(t *testing.T) {
	// Arrange
	h := newFixture(t, "payments")
	h.mustRun("init", "--name", "acme", "--adopt")

	// Act
	output := h.mustRun("fit", "--repos", "2", "--contracts", "0", "--people", "1")

	// Assert
	if !strings.Contains(output, "None of this pays for itself") {
		t.Errorf("fit recommended adoption at a scale where it is pure overhead:\n%s", output)
	}
}

func TestMetricsRecordsASnapshotAndReadsItBack(t *testing.T) {
	// Arrange
	h := newFixture(t, "payments")
	h.mustRun("init", "--name", "acme", "--adopt")

	// Act
	h.mustRun("metrics", "--record")
	output := h.mustRun("metrics", "--history")

	// Assert
	if !strings.Contains(output, "2026-08-25") {
		t.Errorf("the recorded snapshot does not appear in the history:\n%s", output)
	}
}

func TestVersionReportsSomethingForEveryField(t *testing.T) {
	// Arrange
	h := newFixture(t)

	// Act
	output := h.mustRun("version")

	// Assert
	if !strings.HasPrefix(strings.TrimSpace(output), "vat ") {
		t.Errorf("version output = %q", output)
	}
}

func TestAnUnknownCommandExitsWithAUsageCode(t *testing.T) {
	// Arrange
	h := newFixture(t)

	// Act
	code, output := h.run("nonsense")

	// Assert
	if code != ExitUsage {
		t.Errorf("exit code = %d, want %d", code, ExitUsage)
	}
	if !strings.Contains(output, "unknown command") {
		t.Errorf("the error does not name the problem:\n%s", output)
	}
}

func TestHelpIsAvailableForEveryCommand(t *testing.T) {
	// Arrange: help text that fails to render is a first-impression defect.
	h := newFixture(t)

	var walk func(command *Command, path []string)
	walk = func(command *Command, path []string) {
		if len(path) > 0 {
			code, output := h.run(append(append([]string{}, path...), "--help")...)
			if code != ExitOK {
				t.Errorf("`vat %s --help` exited %d:\n%s", strings.Join(path, " "), code, output)
			}
			if strings.TrimSpace(output) == "" {
				t.Errorf("`vat %s --help` printed nothing", strings.Join(path, " "))
			}
		}
		for _, sub := range command.Subcommands {
			walk(sub, append(append([]string{}, path...), sub.Name))
		}
	}

	// Act & Assert
	walk(Root(), nil)
}

func TestCompletionRendersForEverySupportedShell(t *testing.T) {
	// Arrange
	h := newFixture(t)

	for _, shell := range []string{"bash", "zsh", "fish"} {
		// Act
		output := h.mustRun("completion", shell)

		// Assert
		if !strings.Contains(output, "vat") {
			t.Errorf("%s completion does not mention the command:\n%s", shell, output)
		}
	}
}

func TestEveryReportingCommandEmitsAnArrayNotNull(t *testing.T) {
	// Arrange: --json is a documented interface. A consumer iterating the
	// result should never have to special-case a null where an empty list is
	// what actually happened.
	h := newFixture(t, "payments")
	h.mustRun("init", "--name", "acme", "--adopt")

	commands := [][]string{
		{"status"},
		{"repo", "list"},
		{"lint", "--offline"},
		{"changeset", "list"},
		{"evidence", "list"},
		{"harness", "roles"},
		{"harness", "check"},
	}

	for _, args := range commands {
		// Act
		var out, errOut bytes.Buffer
		env := &Env{
			Printer: ui.NewWith(&out, &errOut, false),
			Now:     testNow, Cwd: h.root, Root: h.root, JSON: true, Yes: true,
		}
		dispatch(context.Background(), env, Root(), args, nil)

		// Assert
		payload := strings.TrimSpace(out.String())
		if payload == "" {
			t.Errorf("`vat %s --json` printed nothing", strings.Join(args, " "))
			continue
		}
		if payload == "null" || strings.Contains(payload, ": null") {
			t.Errorf("`vat %s --json` emitted null where an empty list belongs:\n%s",
				strings.Join(args, " "), payload)
		}
		var decoded any
		if err := json.Unmarshal([]byte(payload), &decoded); err != nil {
			t.Errorf("`vat %s --json` is not valid JSON: %v\n%s",
				strings.Join(args, " "), err, payload)
		}
	}
}

func TestRepoListJSONUsesTheDocumentedFieldNames(t *testing.T) {
	// Arrange: the manifest is written in snake_case, and its JSON projection
	// has to match — otherwise the same field has two names depending on which
	// way you read it.
	h := newFixture(t, "payments")
	h.mustRun("init", "--name", "acme", "--adopt")

	// Act
	var repos []map[string]any
	h.runJSON(&repos, "repo", "list")

	// Assert
	if len(repos) != 1 {
		t.Fatalf("repository count = %d, want 1", len(repos))
	}
	for _, key := range []string{"name", "origin", "role"} {
		if _, found := repos[0][key]; !found {
			t.Errorf("the JSON payload has no %q field: %v", key, repos[0])
		}
	}
	if _, found := repos[0]["Name"]; found {
		t.Error("the payload exposes Go field names rather than the documented ones")
	}
}

func TestARepositoryPathCannotBeTheWorkspaceRoot(t *testing.T) {
	// Arrange: accepting "." makes every operation on that repository an
	// operation on the whole workspace, and `repo remove --delete` a way to
	// delete every governed repository's working tree at once.
	h := newFixture(t, "payments")
	h.mustRun("init", "--name", "acme", "--adopt")

	// Act
	code, output := h.run("repo", "add", "danger",
		"--origin", "https://example.com/acme/danger.git", "--path", ".", "--no-clone")

	// Assert
	if code == ExitOK {
		t.Fatalf("a repository rooted at the workspace was accepted:\n%s", output)
	}
	manifestText, err := os.ReadFile(h.path("vat.yaml"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if strings.Contains(string(manifestText), "danger") {
		t.Error("the rejected repository was written to the manifest anyway")
	}
}

func TestStrictlyBelowRefusesTheRootAndAnythingOutsideIt(t *testing.T) {
	// Arrange: this is the guard standing between `repo remove --delete` and
	// os.RemoveAll, so it is asserted directly rather than only through the
	// manifest that normally prevents reaching it.
	root := t.TempDir()
	inside := filepath.Join(root, "payments")
	if err := os.MkdirAll(inside, 0o755); err != nil {
		t.Fatalf("create: %v", err)
	}
	outside := t.TempDir()

	cases := []struct {
		path  string
		below bool
	}{
		{inside, true},
		{filepath.Join(inside, "nested"), true},
		{root, false},
		{outside, false},
		{filepath.Dir(root), false},
	}

	for _, testCase := range cases {
		// Act & Assert
		if got := strictlyBelow(root, testCase.path); got != testCase.below {
			t.Errorf("strictlyBelow(%q, %q) = %v, want %v",
				root, testCase.path, got, testCase.below)
		}
	}
}

func TestAGroupThatMatchesNothingIsAnErrorNotAnEmptyRun(t *testing.T) {
	// Arrange: `vat exec --group backedn -- make test` used to print "Nothing
	// to run." and exit 0, which in CI is a green build that tested nothing.
	h := newFixture(t, "payments")
	h.mustRun("init", "--name", "acme", "--adopt")

	// Act
	code, output := h.run("exec", "--group", "backedn", "--", "true")

	// Assert
	if code != ExitUsage {
		t.Errorf("exit code = %d, want %d; an unmatched group is a typo", code, ExitUsage)
	}
	if !strings.Contains(output, "backedn") {
		t.Errorf("the error does not name the group that matched nothing:\n%s", output)
	}
}

func TestExecDoesNotReinterpretTheCallersQuoting(t *testing.T) {
	// Arrange: joining argv back into a string handed the caller's quoting to a
	// second shell, so `-- git commit -m "wip; rm -rf build"` ran the rm.
	h := newFixture(t, "payments")
	h.mustRun("init", "--name", "acme", "--adopt")
	canary := h.path("payments", "canary.txt")
	if err := os.WriteFile(canary, []byte("intact\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Act
	output := h.mustRun("exec", "--only", "payments", "--", "echo", "safe; rm "+canary)

	// Assert
	if _, err := os.Stat(canary); err != nil {
		t.Fatalf("the second command ran: the canary was deleted (%v)\n%s", err, output)
	}
	if !strings.Contains(output, "safe; rm ") {
		t.Errorf("the argument was not passed through literally:\n%s", output)
	}
}

func TestChangesetVerifyRefusesOnADirtyTree(t *testing.T) {
	// Arrange: recording a pass would file results against a revision that does
	// not describe what was tested, which is the one claim a changeset makes.
	h := newFixture(t, "payments")
	h.mustRun("init", "--name", "acme", "--adopt")
	addCheck(t, h, "payments", "true")
	h.mustRun("changeset", "new", "Do a thing", "--repos", "payments")
	if err := os.WriteFile(h.path("payments", "dirty.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Act
	code, output := h.run("changeset", "verify", "CS-0001")

	// Assert
	if code != ExitFindings {
		t.Errorf("exit code = %d, want %d; a dirty tree must not verify", code, ExitFindings)
	}
	if !strings.Contains(output, "dirty") {
		t.Errorf("the refusal does not say why:\n%s", output)
	}
}

func TestChangesetVerifyRefusesOnAClosedChangeset(t *testing.T) {
	// Arrange: re-verifying rewrote status back to "verified" while the closing
	// evidence stayed in the file, leaving a record claiming both at once.
	h := newFixture(t, "payments")
	h.mustRun("init", "--name", "acme", "--adopt")
	addCheck(t, h, "payments", "true")
	commitAll(t, h, "payments")
	h.mustRun("changeset", "new", "Do a thing", "--repos", "payments")
	h.mustRun("changeset", "verify", "CS-0001")
	h.mustRun("changeset", "close", "CS-0001", "--acceptance", "it works end to end")

	// Act
	code, output := h.run("changeset", "verify", "CS-0001")

	// Assert
	if code == ExitOK {
		t.Errorf("verifying a closed changeset succeeded:\n%s", output)
	}
}

func TestEvidenceNewRefusesToOverwriteAPacket(t *testing.T) {
	// Arrange: overwriting silently replaced the acceptance criterion, which is
	// the one thing the packet exists to fix before work starts.
	h := newFixture(t, "payments")
	h.mustRun("init", "--name", "acme", "--adopt")
	h.mustRun("evidence", "new", "EP-001", "First", "--repos", "payments",
		"--acceptance", "the original criterion")

	// Act
	code, _ := h.run("evidence", "new", "EP-001", "Second", "--repos", "payments")

	// Assert
	if code != ExitUsage {
		t.Errorf("exit code = %d, want %d", code, ExitUsage)
	}
	content, err := os.ReadFile(h.path("evidence", "EP-001.yaml"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(content), "the original criterion") {
		t.Error("the original acceptance criterion was overwritten")
	}
}

func TestARoleNameCannotEscapeTheAdapterDirectories(t *testing.T) {
	// Arrange: an adapter is written whole with no markers, so a traversing
	// name would replace a hand-written file — reachable from `lint --fix`.
	h := newFixture(t)
	h.mustRun("init", "--name", "acme")
	rolePath := h.path(".agents", "roles", "escape.md")
	if err := os.MkdirAll(filepath.Dir(rolePath), 0o755); err != nil {
		t.Fatalf("create: %v", err)
	}
	body := "---\nname: ../../AGENTS\ndescription: escape\n---\n\n# escape\n"
	if err := os.WriteFile(rolePath, []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	contractBefore, err := os.ReadFile(h.path("AGENTS.md"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	// Act
	code, output := h.run("harness", "render")

	// Assert
	if code == ExitOK {
		t.Errorf("a traversing role name was accepted:\n%s", output)
	}
	contractAfter, err := os.ReadFile(h.path("AGENTS.md"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(contractAfter) != string(contractBefore) {
		t.Error("the workspace contract was overwritten by a generated adapter")
	}
}

func TestBrainSupersedeRefusesARecordSupersedingItself(t *testing.T) {
	// Arrange: it wrote a chain pointing nowhere, and `brain check` then failed
	// permanently with no vat command able to clear it.
	h := newFixture(t, "brain")
	h.mustRun("init", "--name", "acme", "--adopt")
	h.mustRun("brain", "init")
	h.mustRun("brain", "new", "decision", "--title", "Use SQLite")

	// Act
	code, output := h.run("brain", "supersede", "D-0001", "D-0001")

	// Assert
	if code != ExitUsage {
		t.Errorf("exit code = %d, want %d:\n%s", code, ExitUsage, output)
	}
	h.mustRun("brain", "check")
}

// commitAll commits whatever `init --adopt` left in a repository, so a test
// about something else does not trip the dirty-tree guard.
func commitAll(t *testing.T, h *workspaceFixture, name string) {
	t.Helper()
	git(t, h.path(name), "add", "-A")
	git(t, h.path(name), "commit", "--quiet", "-m", "adopt")
}

// addCheck gives a repository a canonical check so a changeset can verify it.
func addCheck(t *testing.T, h *workspaceFixture, name, command string) {
	t.Helper()
	path := h.path("vat.yaml")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	updated := strings.Replace(string(content),
		"    - name: "+name+"\n",
		"    - name: "+name+"\n      checks:\n        - "+command+"\n", 1)
	if err := os.WriteFile(path, []byte(updated), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}
