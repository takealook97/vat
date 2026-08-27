package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/takealook97/vat/internal/ui"
)

// The commands a workspace runs every day, driven end to end against real git
// repositories. The harness they share lives in fixture_test.go.

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
		{"harness", "skills"},
		{"harness", "adopt"},
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
	landOnUpstream(t, h, "payments")
	h.mustRun("ship", "CS-0001", "--offline")
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

// A repository with no canonical checks was counted as a failed check, and the
// summary said those checks were "recorded against the revisions they ran on".
// Nothing ran. That phrase is the evidentiary claim the whole record rests on,
// so a summary that makes it about checks which never executed is not a wording
// problem.
func TestChangesetVerifySeparatesAFailedCheckFromNoCheckAtAll(t *testing.T) {
	// Arrange: one repository that can be verified and fails, one that declares
	// nothing to run.
	h := adoptedFixture(t, "payments", "orders")
	addCheck(t, h, "payments", "exit 1")
	h.mustRun("changeset", "new", "Both at once", "--repos", "payments,orders")

	// Act
	code, output := h.run("changeset", "verify", "CS-0001")

	// Assert
	if code == ExitOK {
		t.Fatalf("verification passed with a failing check:\n%s", output)
	}
	if !strings.Contains(output, "1 check") {
		t.Errorf("the summary does not count the one check that actually ran:\n%s", output)
	}
	if strings.Contains(output, "2 check") {
		t.Errorf("a repository with nothing to run was counted as a failed check:\n%s", output)
	}
	if !strings.Contains(output, "canonical checks") {
		t.Errorf("the summary does not say a repository declared nothing to run:\n%s", output)
	}
}

func TestChangesetVerifySaysWhenNothingCouldBeVerifiedAtAll(t *testing.T) {
	// Arrange: neither repository declares a check, so no check failed — there
	// were none.
	h := adoptedFixture(t, "payments", "orders")
	h.mustRun("changeset", "new", "Nothing to run", "--repos", "payments,orders")

	// Act
	code, output := h.run("changeset", "verify", "CS-0001")

	// Assert
	if code == ExitOK {
		t.Fatalf("an unverifiable changeset was reported as verified:\n%s", output)
	}
	if strings.Contains(output, "check(s) failed") || strings.Contains(output, "checks failed") {
		t.Errorf("the summary reports failed checks where none ran:\n%s", output)
	}
	if !strings.Contains(output, "2 repositories") {
		t.Errorf("the summary does not count the repositories that could not be verified:\n%s", output)
	}
}

// The same fact as the no-checks case, in the other three conditions that stop
// a repository being verified at all: a dirty tree, a clone that is not there,
// and a participant no longer in the manifest. Each was counted as a failed
// check, and the summary said those checks were "recorded against the revisions
// they ran on" when nothing had run.
func TestChangesetVerifyCountsARepositoryItCouldNotEnterAsUnverified(t *testing.T) {
	// Arrange
	h := adoptedFixture(t, "payments")
	addCheck(t, h, "payments", "true")
	h.mustRun("changeset", "new", "Dirty tree", "--repos", "payments")
	writeFile(t, h.path("payments", "scratch.txt"), "uncommitted\n")

	// Act
	code, output := h.run("changeset", "verify", "CS-0001")

	// Assert
	if code == ExitOK {
		t.Fatalf("a dirty tree was verified:\n%s", output)
	}
	if strings.Contains(output, "check") && strings.Contains(output, "failed; recorded") {
		t.Errorf("the summary claims a check ran and was recorded:\n%s", output)
	}
	if !strings.Contains(output, "could not be verified") {
		t.Errorf("the summary does not say the repository could not be verified:\n%s", output)
	}
}

// The summary under `vat sync` counted what advanced, what was left alone, and
// what needed attention — and nothing else. A workspace where every repository
// was already current printed three zeros under a table of rows, so the numbers
// did not add up to what was above them, exactly when the run had gone
// perfectly.
func TestTheSyncSummaryAccountsForEveryRepositoryInTheTable(t *testing.T) {
	// Arrange
	h := adoptedFixture(t, "payments")

	// Act
	output := h.mustRun("sync", "--offline")

	// Assert
	if strings.Contains(output, "0 advanced · 0 left alone on purpose · 0 need attention") {
		t.Errorf("the summary accounts for no repository at all:\n%s", output)
	}
	if !strings.Contains(output, "current") {
		t.Errorf("a repository that needed nothing is not accounted for:\n%s", output)
	}
}

// `vat exec` printed the same fact twice: a summary line and an error line
// restating it inversely, both on stdout. The rows above already name each
// failure.
func TestExecStatesTheOutcomeOnceRatherThanTwice(t *testing.T) {
	// Arrange
	h := adoptedFixture(t, "payments")

	// Act
	code, output := h.run("exec", "--", "false")

	// Assert
	if code == ExitOK {
		t.Fatalf("a failing command exited zero:\n%s", output)
	}
	if strings.Contains(output, "commands failed") && strings.Contains(output, "succeeded") {
		t.Errorf("the outcome is stated twice:\n%s", output)
	}
}

// git converts LF to CRLF on checkout under its default configuration on
// Windows, so every file this tool generates comes back different from what it
// wrote. Comparing bytes made the whole harness layer unusable there: a
// permanently red `vat lint`, a `--fix` that fixed nothing because git undid it,
// and a `vat harness render` that reported every file written on every run.
//
// Driven end to end rather than per comparison, because the failure was that
// four packages each answered this question and one of them was enough to keep
// the workspace red.
func TestNothingIsDriftedAfterACheckoutRewritesEveryLineEnding(t *testing.T) {
	// Arrange
	h := adoptedFixture(t, "payments", "brain")
	h.mustRun("brain", "init")
	h.mustRun("brain", "build")
	h.mustRun("harness", "role", "new", "planner", "--description", "Plans work.")
	rewritten := rewriteEveryGeneratedFileToCRLF(t, h.root)
	if rewritten == 0 {
		t.Fatal("no generated file was rewritten; the layout changed and this test stopped checking anything")
	}

	// Act & Assert
	report := h.lint(t, "--offline")
	for _, finding := range report.Findings {
		if strings.Contains(finding.Rule, "drift") {
			t.Errorf("%s reported after a checkout rewrote line endings: %s", finding.Rule, finding.Subject)
		}
	}
	if output := h.mustRun("harness", "render"); !strings.Contains(output, "already current") {
		t.Errorf("rendering rewrote files for their line endings:\n%s", output)
	}
	if output := h.mustRun("brain", "build"); !strings.Contains(output, "already current") {
		t.Errorf("building rewrote projections for their line endings:\n%s", output)
	}
}

// rewriteEveryGeneratedFileToCRLF does to the workspace what a checkout does,
// and returns how many files it touched.
func rewriteEveryGeneratedFileToCRLF(t *testing.T, root string) int {
	t.Helper()
	touched := 0
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if entry.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		switch filepath.Ext(path) {
		case ".md", ".json", ".toml", ".yaml":
		default:
			if entry.Name() != ".gitignore" && entry.Name() != ".brain" {
				return nil
			}
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if !bytes.Contains(content, []byte("\n")) || bytes.Contains(content, []byte("\r\n")) {
			return nil
		}
		touched++
		return os.WriteFile(path, bytes.ReplaceAll(content, []byte("\n"), []byte("\r\n")), 0o644)
	})
	if err != nil {
		t.Fatalf("walk the workspace: %v", err)
	}
	return touched
}

// A dry run reports what it would do. Counting those rows as "already current"
// says the opposite of what they mean — the repository is absent and would be
// cloned — in the summary somebody reads to decide whether to run it for real.
func TestADryRunSummarisesThePlanRatherThanAnOutcome(t *testing.T) {
	// Arrange
	h := newFixture(t)
	h.mustRun("init", "--name", "acme")
	h.mustRun("repo", "add", "absent",
		"--origin", "https://example.invalid/acme/absent.git", "--no-clone")

	// Act
	output := h.mustRun("sync", "--dry-run")

	// Assert
	if strings.Contains(output, "already current") {
		t.Errorf("a dry run counted a repository it would clone as already current:\n%s", output)
	}
	if !strings.Contains(output, "would") {
		t.Errorf("the summary does not say the run was a plan:\n%s", output)
	}
	if !strings.Contains(output, "Nothing was written") {
		t.Errorf("the summary does not say nothing happened:\n%s", output)
	}
}
