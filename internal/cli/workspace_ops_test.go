package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// `status` and `sync` are the two commands a workspace runs constantly, so what
// they report about an awkward repository — dirty, ahead, detached, unreadable —
// is the part most likely to be believed without checking. These build each of
// those states for real and read what comes back.

// gitOutput runs a git command and returns its trimmed output.
func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %v in %s: %v", args, dir, err)
	}
	return strings.TrimSpace(string(output))
}

func headOf(t *testing.T, h *workspaceFixture, name string) string {
	t.Helper()
	return gitOutput(t, h.path(name), "rev-parse", "HEAD")
}

func TestStatusDistinguishesEveryStateARepositoryCanBeIn(t *testing.T) {
	// Arrange
	h := adoptedFixture(t, "payments", "console", "docs")

	// A repository with uncommitted work.
	if err := os.WriteFile(h.path("console", "scratch.txt"), []byte("wip\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	// A repository with a commit its remote has never seen.
	if err := os.WriteFile(h.path("docs", "extra.md"), []byte("more\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	git(t, h.path("docs"), "add", "-A")
	git(t, h.path("docs"), "commit", "--quiet", "-m", "ahead of origin")

	// Act
	var statuses []repoStatus
	h.runJSON(&statuses, "status")

	// Assert
	byName := map[string]repoStatus{}
	for _, status := range statuses {
		byName[status.Name] = status
	}
	if !byName["console"].Dirty {
		t.Errorf("a repository with uncommitted work is not reported dirty: %+v", byName["console"])
	}
	if byName["docs"].Ahead == 0 {
		t.Errorf("a repository holding an unpushed commit reports no divergence: %+v", byName["docs"])
	}
	for name, status := range byName {
		if !status.Present {
			t.Errorf("%s is on disk but reported as not present", name)
		}
		if status.Revision == "" {
			t.Errorf("%s reports no revision, so nothing can be pinned to it", name)
		}
	}
}

func TestStatusReportsADetachedHeadAsItsOwnState(t *testing.T) {
	// Arrange: a detached HEAD is neither a branch nor an error, and calling it
	// either would hide work that is about to be lost.
	h := adoptedFixture(t, "payments")
	git(t, h.path("payments"), "checkout", "--quiet", "--detach", "HEAD")

	// Act
	_, output := h.run("status")
	var statuses []repoStatus
	h.runJSON(&statuses, "status")

	// Assert
	if !strings.Contains(output, "detached") {
		t.Errorf("a detached HEAD is not named in the table:\n%s", output)
	}
	if len(statuses) != 1 || statuses[0].Revision == "" {
		t.Errorf("a detached repository still has a revision to report: %+v", statuses)
	}
}

func TestStatusNeverReportsAnUnreadableRepositoryAsClean(t *testing.T) {
	// Arrange: reporting a repository git could not be questioned about as
	// "clean" is the one answer a status command must never give.
	h := adoptedFixture(t, "payments")
	if err := os.Remove(h.path("payments", ".git", "HEAD")); err != nil {
		t.Fatalf("remove: %v", err)
	}

	// Act
	var statuses []repoStatus
	h.runJSON(&statuses, "status")

	// Assert
	if len(statuses) != 1 {
		t.Fatalf("expected one repository, got %+v", statuses)
	}
	status := statuses[0]
	if status.Present && !status.Unreadable && status.Branch != "" && !status.Dirty {
		t.Errorf("a repository git cannot read came back looking healthy: %+v", status)
	}
}

func TestStatusDirtyNarrowsToWhatHasUncommittedWork(t *testing.T) {
	// Arrange: --dirty is what someone runs before walking away from a machine,
	// so it covers everything that exists nowhere else — uncommitted changes,
	// commits the remote has never seen, and stashes.
	h := adoptedFixture(t, "payments", "console")
	if err := os.WriteFile(h.path("console", "scratch.txt"), []byte("wip\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Act
	var dirty []repoStatus
	h.runJSON(&dirty, "status", "--dirty")

	// Assert
	found := false
	for _, status := range dirty {
		if status.Name == "console" {
			found = true
			if !status.Dirty {
				t.Errorf("console holds uncommitted work but is not marked dirty: %+v", status)
			}
		}
		if !status.Dirty && status.Ahead == 0 && status.Stashes == 0 {
			t.Errorf("--dirty listed %s, which holds nothing that exists only here: %+v",
				status.Name, status)
		}
	}
	if !found {
		t.Errorf("--dirty left out the repository with uncommitted work: %+v", dirty)
	}
}

func TestSyncDryRunReportsWhatItWouldDoAndWritesNothing(t *testing.T) {
	// Arrange: the whole point of a dry run is that its output can be trusted
	// without the state having moved underneath it.
	h := adoptedFixture(t, "payments")
	before := headOf(t, h, "payments")

	// Act
	code, output := h.run("sync", "--dry-run")

	// Assert
	if code != ExitOK {
		t.Errorf("`sync --dry-run` exited %d:\n%s", code, output)
	}
	if after := headOf(t, h, "payments"); after != before {
		t.Errorf("a dry run moved HEAD from %s to %s", before, after)
	}
}

func TestSyncOfflineStillReportsTheLocalState(t *testing.T) {
	// Arrange: offline is what makes the command usable on a train, and it must
	// report rather than refuse.
	h := adoptedFixture(t, "payments")

	// Act
	code, output := h.run("sync", "--offline")

	// Assert
	if code != ExitOK {
		t.Errorf("`sync --offline` exited %d on a healthy workspace:\n%s", code, output)
	}
	if !strings.Contains(output, "payments") {
		t.Errorf("offline sync did not report the repository at all:\n%s", output)
	}
}

func TestSyncNeverRewritesARemoteThatMoved(t *testing.T) {
	// Arrange: rewriting the remote would turn a possible supply-chain problem
	// into a silent redirection of every future fetch.
	h := adoptedFixture(t, "payments")
	git(t, h.path("payments"), "remote", "set-url", "origin",
		"https://example.invalid/somewhere-else/payments.git")

	// Act
	code, output := h.run("sync")

	// Assert
	if code == ExitOK {
		t.Errorf("sync advanced a repository whose remote had moved:\n%s", output)
	}
	remote := gitOutput(t, h.path("payments"), "remote", "get-url", "origin")
	if !strings.Contains(remote, "somewhere-else") {
		t.Errorf("sync rewrote the remote to %q", remote)
	}
}

func TestRepoAddRefusesADuplicateName(t *testing.T) {
	// Arrange: two entries with the same name make every lookup depend on
	// iteration order.
	h := adoptedFixture(t, "payments")

	// Act
	code, output := h.run("repo", "add", "payments",
		"--origin", "https://example.invalid/acme/payments.git", "--no-clone")

	// Assert
	if code == ExitOK {
		t.Errorf("a second entry called payments was accepted:\n%s", output)
	}
}

func TestRepoAddRequiresAnOrigin(t *testing.T) {
	// Arrange: a manifest entry with no remote is one nothing can ever clone.
	h := adoptedFixture(t, "payments")

	// Act
	code, output := h.run("repo", "add", "console", "--no-clone")

	// Assert
	if code == ExitOK {
		t.Errorf("a repository was enrolled with no origin:\n%s", output)
	}
}

func TestRepoAdoptEnrolsADirectoryAlreadyOnDisk(t *testing.T) {
	// Arrange: adoption is the path in for a workspace that already exists, so
	// it has to leave the manifest, the exclusion, and the contracts in step.
	h := adoptedFixture(t, "payments")
	notes := h.path("notes")
	if err := os.MkdirAll(notes, 0o755); err != nil {
		t.Fatalf("create: %v", err)
	}
	git(t, notes, "init", "--quiet", "--initial-branch", "main", ".")
	git(t, notes, "remote", "add", "origin", "https://example.invalid/acme/notes.git")

	// Act
	output := h.mustRun("repo", "adopt", "notes", "--role", "docs", "--group", "internal")

	// Assert
	written, err := os.ReadFile(h.path("vat.yaml"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(written), "notes") {
		t.Errorf("the adopted directory is not in the manifest:\n%s", output)
	}
	ignore, err := os.ReadFile(h.path(".gitignore"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(ignore), "notes") {
		t.Error("the adopted directory is in the manifest but not excluded from the root history")
	}
	if _, err := os.Stat(filepath.Join(notes, "AGENTS.md")); err != nil {
		t.Error("the adopted repository was left without a contract")
	}
}

func TestRepoAdoptRefusesADirectoryThatIsNotARepository(t *testing.T) {
	// Arrange: enrolling a plain directory produces a manifest entry every later
	// command has to special-case.
	h := adoptedFixture(t, "payments")
	if err := os.MkdirAll(h.path("not-a-repo"), 0o755); err != nil {
		t.Fatalf("create: %v", err)
	}

	// Act
	code, output := h.run("repo", "adopt", "not-a-repo")

	// Assert
	if code == ExitOK {
		t.Errorf("a directory that is not a git repository was adopted:\n%s", output)
	}
}

func TestRepoListReportsInBothFormsAndRespectsTheSelectors(t *testing.T) {
	// Arrange
	h := adoptedFixture(t, "payments", "console")

	// Act
	table := h.mustRun("repo", "list")
	var all, narrowed []struct {
		Name string `json:"name"`
	}
	h.runJSON(&all, "repo", "list")
	h.runJSON(&narrowed, "repo", "list", "--only", "payments")

	// Assert
	for _, name := range []string{"payments", "console"} {
		if !strings.Contains(table, name) {
			t.Errorf("the table omits %s:\n%s", name, table)
		}
	}
	if len(all) != 2 {
		t.Errorf("the JSON form lists %d repositories, want 2", len(all))
	}
	if len(narrowed) != 1 || narrowed[0].Name != "payments" {
		t.Errorf("--only payments listed %+v", narrowed)
	}
}

func TestArchivingAndUnarchivingAreReversible(t *testing.T) {
	// Arrange: archiving is how a repository leaves the working set without
	// leaving the record, so it has to be undoable exactly.
	h := adoptedFixture(t, "payments", "legacy-api")

	// Act
	h.mustRun("repo", "archive", "legacy-api")
	var everyday []struct {
		Name string `json:"name"`
	}
	h.runJSON(&everyday, "repo", "list")
	h.mustRun("repo", "unarchive", "legacy-api")
	var restored []struct {
		Name string `json:"name"`
	}
	h.runJSON(&restored, "repo", "list")

	// Assert
	for _, repo := range everyday {
		if repo.Name == "legacy-api" {
			t.Error("an archived repository is still in the everyday list")
		}
	}
	found := false
	for _, repo := range restored {
		if repo.Name == "legacy-api" {
			found = true
		}
	}
	if !found {
		t.Error("unarchiving did not bring the repository back")
	}
}

func TestHarnessRenderIsIdempotentAcrossRepeatedRuns(t *testing.T) {
	// Arrange: generated text that changes between two runs over the same
	// manifest shows as drift in git and trains everyone to ignore the diff.
	h := adoptedFixture(t, "payments")
	h.mustRun("harness", "render")
	first, err := os.ReadFile(h.path("payments", "AGENTS.md"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	// Act
	h.mustRun("harness", "render")
	second, err := os.ReadFile(h.path("payments", "AGENTS.md"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	// Assert
	if string(first) != string(second) {
		t.Error("two renders over the same manifest produced different contracts")
	}
	if code, output := h.run("harness", "check"); code != ExitOK {
		t.Errorf("`harness check` disagrees with the render it just followed:\n%s", output)
	}
}

func TestEmptyListingsSaySoRatherThanPrintingNothing(t *testing.T) {
	// Arrange: an empty table and a silent success look identical, and the
	// second sends someone hunting for a file that was never written. The JSON
	// form has the matching obligation: an array, never null.
	h := adoptedFixture(t, "payments")

	cases := []struct {
		name string
		args []string
	}{
		{"harness roles", []string{"harness", "roles"}},
		{"evidence list", []string{"evidence", "list"}},
		{"changeset list", []string{"changeset", "list"}},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			// Act
			output := h.mustRun(testCase.args...)
			var listed []map[string]any
			h.runJSON(&listed, testCase.args...)

			// Assert
			if strings.TrimSpace(output) == "" {
				t.Errorf("`vat %s` printed nothing at all", strings.Join(testCase.args, " "))
			}
			if listed == nil {
				t.Errorf("`vat %s --json` produced null rather than an empty array",
					strings.Join(testCase.args, " "))
			}
		})
	}
}

func TestRepoNewRefusesANameBeforeItTouchesTheDisk(t *testing.T) {
	// Arrange: the name becomes a directory and a remote URL. Validating it only
	// when the manifest is saved meant `vat repo new ../escaped` initialised a
	// repository outside the workspace, scaffolded it, committed it, and then
	// failed — leaving everything it had written behind, outside the one
	// directory this tool is allowed to write to.
	h := adoptedFixture(t, "payments")
	outside := filepath.Join(filepath.Dir(h.root), "escaped")

	cases := []string{"../escaped", "nested/name", ".hidden"}

	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			// Act
			code, output := h.run("repo", "new", name, "--no-remote")

			// Assert
			if code == ExitOK {
				t.Errorf("`repo new %s` was accepted:\n%s", name, output)
			}
			if _, err := os.Stat(outside); err == nil {
				t.Errorf("`repo new %s` wrote outside the workspace root:\n%s", name, output)
			}
			stray := filepath.Join(h.root, filepath.Base(name))
			if _, err := os.Stat(stray); err == nil {
				t.Errorf("`repo new %s` left %s behind after refusing:\n%s", name, stray, output)
			}
		})
	}
}

func TestHarnessRoleNewRefusesANameThatWouldEscapeItsDirectory(t *testing.T) {
	// Arrange: the name is pasted into a file path in .agents/roles and in every
	// runtime's adapter directory. `harness role new ../../../pwned` wrote the
	// role body outside the workspace entirely and reported success — the check
	// for this already existed in the harness package and this command was the
	// one caller that never asked.
	h := adoptedFixture(t, "payments")
	outside := filepath.Join(filepath.Dir(h.root), "pwned.md")

	for _, name := range []string{"../../../pwned", "../escape", "nested/role"} {
		t.Run(name, func(t *testing.T) {
			// Act
			code, output := h.run("harness", "role", "new", name, "--description", "x")

			// Assert
			if code == ExitOK {
				t.Errorf("`harness role new %s` was accepted:\n%s", name, output)
			}
			if _, err := os.Stat(outside); err == nil {
				t.Errorf("`harness role new %s` wrote outside the workspace:\n%s", name, output)
			}
			stray := filepath.Join(h.root, filepath.Base(name)+".md")
			if _, err := os.Stat(stray); err == nil {
				t.Errorf("`harness role new %s` wrote %s, which no command reads:\n%s",
					name, stray, output)
			}
		})
	}
}

func TestHarnessRoleNewStillAcceptsAnOrdinaryName(t *testing.T) {
	// Arrange: the guard above must not have closed the door on the normal case.
	h := adoptedFixture(t, "payments")

	// Act
	h.mustRun("harness", "role", "new", "planner", "--description", "plans work")

	// Assert
	if _, err := os.Stat(h.path(".agents", "roles", "planner.md")); err != nil {
		t.Errorf("an ordinary role name did not produce a role body: %v", err)
	}
}

func TestHarnessSkillNewRefusesANameThatWouldEscapeItsDirectory(t *testing.T) {
	// Arrange: a skill name becomes a directory under .agents/skills and under
	// every runtime's skill directory, and an adapter is written whole rather
	// than into a marked region. The role command learned this the hard way;
	// the skill command must not have to learn it again.
	h := adoptedFixture(t, "payments")
	outside := filepath.Join(filepath.Dir(h.root), "pwned")

	for _, name := range []string{"../../../pwned", "../escape", "nested/skill"} {
		t.Run(name, func(t *testing.T) {
			// Act
			code, output := h.run("harness", "skill", "new", name, "--description", "x")

			// Assert
			if code == ExitOK {
				t.Errorf("`harness skill new %s` was accepted:\n%s", name, output)
			}
			if !strings.Contains(output, "skill") {
				t.Errorf("`harness skill new %s` reported a problem without saying it was a skill, "+
					"which sends the reader to the wrong directory:\n%s", name, output)
			}
			if _, err := os.Stat(outside); err == nil {
				t.Errorf("`harness skill new %s` wrote outside the workspace:\n%s", name, output)
			}
			stray := filepath.Join(h.root, filepath.Base(name))
			if _, err := os.Stat(stray); err == nil {
				t.Errorf("`harness skill new %s` wrote %s, which no command reads:\n%s",
					name, stray, output)
			}
		})
	}
}

func TestHarnessSkillNewGeneratesAClaudeAdapterAndNoOther(t *testing.T) {
	// Arrange: a skill has an adapter for Claude Code and none for Codex, which
	// reads the canonical directory itself. Generating a .codex entry would
	// invent a file that runtime never looks for.
	h := adoptedFixture(t, "payments")

	// Act
	h.mustRun("harness", "skill", "new", "release-a-service",
		"--description", "Take one service to a verified deployment.")

	// Assert
	if _, err := os.Stat(h.path(".agents", "skills", "release-a-service", "SKILL.md")); err != nil {
		t.Errorf("no canonical skill was written: %v", err)
	}
	if _, err := os.Stat(h.path(".claude", "skills", "release-a-service", "SKILL.md")); err != nil {
		t.Errorf("no Claude adapter was generated: %v", err)
	}
	if _, err := os.Stat(h.path(".codex", "skills")); err == nil {
		t.Error("a Codex skill directory was generated; Codex reads the canonical directory")
	}
}

func TestHarnessSkillNewSaysWhenTheSkillWillGenerateNothing(t *testing.T) {
	// Arrange: `--runtimes codex` is spelled correctly, is right on a role, and
	// selects no skill adapter at all. Left unsaid at creation, the skill sits
	// on disk generating nothing while every other check reads green.
	h := adoptedFixture(t, "payments")

	// Act
	code, output := h.run("harness", "skill", "new", "codex-only",
		"--description", "x", "--runtimes", "codex")

	// Assert
	if code != ExitOK {
		t.Errorf("creating an inert skill failed rather than reporting it:\n%s", output)
	}
	if !strings.Contains(output, "no adapter") {
		t.Errorf("`harness skill new --runtimes codex` did not say it generates nothing:\n%s", output)
	}
}

func TestHarnessSkillsReportsWhatEachSkillAdvertises(t *testing.T) {
	// Arrange: the runtimes column reports the adapters actually rendered, not
	// the runtimes: field, because those differ exactly where it matters.
	h := adoptedFixture(t, "payments")
	h.mustRun("harness", "skill", "new", "release-a-service", "--description", "Ship one service.")

	// Act
	var listed []struct {
		Name        string   `json:"name"`
		Description string   `json:"description"`
		Runtimes    []string `json:"runtimes"`
	}
	h.runJSON(&listed, "harness", "skills")

	// Assert
	var found bool
	for _, entry := range listed {
		if entry.Name != "release-a-service" {
			continue
		}
		found = true
		if entry.Description != "Ship one service." {
			t.Errorf("the listing does not describe the skill that was created: %+v", entry)
		}
		if len(entry.Runtimes) != 1 || entry.Runtimes[0] != "claude" {
			t.Errorf("expected the claude adapter alone, got %v", entry.Runtimes)
		}
	}
	if !found {
		t.Errorf("the created skill is missing from the listing: %+v", listed)
	}
}

func TestDeletingTheSeededSkillsCostsNothing(t *testing.T) {
	// Arrange: `vat init` seeds two procedures and the reference says removing
	// one is without consequence. That is a promise about behaviour, so it is
	// checked rather than asserted in prose: a loader that treated the missing
	// directory as a problem would make the documentation false.
	h := adoptedFixture(t, "payments")
	if err := os.RemoveAll(h.path(".agents", "skills")); err != nil {
		t.Fatalf("remove the seeds: %v", err)
	}

	// Act
	output := h.mustRun("harness", "skills")
	var listed []map[string]any
	h.runJSON(&listed, "harness", "skills")

	// Assert
	if strings.TrimSpace(output) == "" {
		t.Error("`vat harness skills` printed nothing at all once the seeds were gone")
	}
	if listed == nil {
		t.Error("`vat harness skills --json` produced null rather than an empty array")
	}
	if len(listed) != 0 {
		t.Errorf("skills were reported after the directory was removed: %+v", listed)
	}
	if code, out := h.run("lint", "--offline"); code == ExitUsage {
		t.Errorf("`vat lint` could not run with no skills directory:\n%s", out)
	}
}

func TestAChangesetRefusesToRecordAnEmptyObjective(t *testing.T) {
	// Arrange: the objective is the one claim the record makes. A blank one
	// reads as a verified bundle of revisions for no stated reason, which is
	// exactly why --acceptance may not be empty when closing.
	h := adoptedFixture(t, "payments")

	// Act
	code, output := h.run("changeset", "new", "", "--repos", "payments")

	// Assert
	if code == ExitOK {
		t.Errorf("a changeset was opened with no objective:\n%s", output)
	}
	var sets []struct {
		ID string `json:"id"`
	}
	h.runJSON(&sets, "changeset", "list")
	if len(sets) != 0 {
		t.Errorf("the refused changeset was written anyway: %+v", sets)
	}
}

func TestRepoAdoptRefusesADirectoryThatOnlyLooksLikeItIsInside(t *testing.T) {
	// Arrange: a symlink inside the workspace pointing at a repository outside
	// it passed every textual containment check, was adopted, and then had a
	// generated contract written into it — outside the one directory vat is
	// allowed to write to.
	h := adoptedFixture(t, "payments")
	outside := filepath.Join(t.TempDir(), "elsewhere")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatalf("create: %v", err)
	}
	git(t, outside, "init", "--quiet", "--initial-branch", "main", ".")
	if err := os.WriteFile(filepath.Join(outside, "README.md"), []byte("x\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	git(t, outside, "add", "-A")
	git(t, outside, "commit", "--quiet", "-m", "init")
	git(t, outside, "remote", "add", "origin", "https://example.invalid/acme/elsewhere.git")
	if err := os.Symlink(outside, h.path("elsewhere")); err != nil {
		t.Skipf("this platform does not allow symlinks here: %v", err)
	}

	// Act
	code, output := h.run("repo", "adopt", "elsewhere")

	// Assert
	if code == ExitOK {
		t.Errorf("a symlink resolving outside the workspace was adopted:\n%s", output)
	}
	if _, err := os.Stat(filepath.Join(outside, "AGENTS.md")); err == nil {
		t.Error("a contract was written into a repository outside the workspace root")
	}
}

func TestStatusReadsCorrectlyForASingleRepository(t *testing.T) {
	// Arrange: the summary line said "1 repositories". The helper that renders
	// a count with the right noun already existed and this one call site did
	// not use it.
	h := adoptedFixture(t, "payments")

	// Act
	_, output := h.run("status")

	// Assert
	if strings.Contains(output, "1 repositories") {
		t.Errorf("the summary does not agree with itself about how many there are:\n%s", output)
	}
	if !strings.Contains(output, "1 repository") {
		t.Errorf("the summary does not state the count at all:\n%s", output)
	}
}

func TestStatusOnlySuggestsSyncWhenSyncCouldActuallyAdvanceSomething(t *testing.T) {
	// Arrange: a diverged repository is behind its remote as well as ahead of
	// it, and sync refuses to touch one. Testing "behind" sent the reader to a
	// command that could only tell them no.
	h := adoptedFixture(t, "payments")
	diverge(t, h, "payments")

	// Act
	_, output := h.run("status", "--fetch")

	// Assert
	if !strings.Contains(output, "diverged") {
		t.Fatalf("the fixture did not actually diverge, so this proves nothing:\n%s", output)
	}
	if strings.Contains(output, "vat sync") {
		t.Errorf("sync was suggested for a repository it refuses to touch:\n%s", output)
	}
}

// diverge advances a repository and its upstream along different histories, so
// the clone is both ahead and behind.
func diverge(t *testing.T, h *workspaceFixture, name string) {
	t.Helper()
	upstream := filepath.Join(filepath.Dir(h.root), "upstream", name)
	if err := os.WriteFile(filepath.Join(upstream, "theirs.md"), []byte("theirs\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	git(t, upstream, "add", "-A")
	git(t, upstream, "commit", "--quiet", "-m", "remote side")

	if err := os.WriteFile(h.path(name, "mine.md"), []byte("mine\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	git(t, h.path(name), "add", "-A")
	git(t, h.path(name), "commit", "--quiet", "-m", "local side")
}

func TestStatusSuggestsSyncWhenARepositoryIsNotClonedYet(t *testing.T) {
	// Arrange: cloning what is missing is the one thing sync can always do, so
	// the hint has to survive the narrowing above.
	h := adoptedFixture(t, "payments")
	if err := os.RemoveAll(h.path("payments")); err != nil {
		t.Fatalf("remove: %v", err)
	}

	// Act
	_, output := h.run("status")

	// Assert
	if !strings.Contains(output, "vat sync") {
		t.Errorf("sync was not suggested for a repository that is not on disk:\n%s", output)
	}
}

func TestBrainInitRefusesADirectoryOutsideTheWorkspace(t *testing.T) {
	// Arrange: the argument is a path and this scaffolds a directory of
	// documents at it, so `brain init ../../outside` built a whole brain
	// repository outside the one directory vat may write to.
	h := adoptedFixture(t, "payments")
	outside := filepath.Join(filepath.Dir(h.root), "escaped-brain")

	// Act
	code, output := h.run("brain", "init", "../../escaped-brain")

	// Assert
	if code == ExitOK {
		t.Errorf("`brain init ../../escaped-brain` was accepted:\n%s", output)
	}
	if _, err := os.Stat(outside); err == nil {
		t.Errorf("a brain repository was scaffolded outside the workspace:\n%s", output)
	}
}

func TestAnOriginCarryingACredentialIsRefused(t *testing.T) {
	// Arrange: vat.yaml is committed. A token pasted into an origin would be
	// published by the next push of the workspace root, and the only place vat
	// could report it is a message it must not print.
	h := adoptedFixture(t, "payments")

	// Act
	code, output := h.run("repo", "add", "console",
		"--origin", "https://user:ghp_EXAMPLETOKEN@example.invalid/acme/console.git", "--no-clone")

	// Assert
	if code != ExitUsage {
		t.Errorf("`repo add` with a credential in --origin exited %d, want %d (called wrong):\n%s",
			code, ExitUsage, output)
	}
	written, err := os.ReadFile(h.path("vat.yaml"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if strings.Contains(string(written), "ghp_EXAMPLETOKEN") {
		t.Error("the credential was written into the manifest")
	}
	if strings.Contains(output, "ghp_EXAMPLETOKEN") {
		t.Errorf("the refusal quoted the credential back:\n%s", output)
	}
}

func TestAdoptRecordsARemoteWithoutItsCredential(t *testing.T) {
	// Arrange: adoption records what it found rather than what someone typed,
	// so a remote that already carries a token must still be adoptable — with
	// the token left in git's credential helper, where it belongs.
	h := adoptedFixture(t, "payments")
	notes := h.path("notes")
	if err := os.MkdirAll(notes, 0o755); err != nil {
		t.Fatalf("create: %v", err)
	}
	git(t, notes, "init", "--quiet", "--initial-branch", "main", ".")
	git(t, notes, "remote", "add", "origin",
		"https://user:ghp_EXAMPLETOKEN@example.invalid/acme/notes.git")

	// Act
	code, output := h.run("repo", "adopt", "notes")

	// Assert
	if code != ExitOK {
		t.Fatalf("a repository with a credential-bearing remote could not be adopted:\n%s", output)
	}
	written, err := os.ReadFile(h.path("vat.yaml"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if strings.Contains(string(written), "ghp_EXAMPLETOKEN") {
		t.Error("the credential was recorded in the manifest")
	}
	if !strings.Contains(string(written), "example.invalid/acme/notes.git") {
		t.Errorf("stripping the credential also lost the repository identity:\n%s", written)
	}
	if strings.Contains(output, "ghp_EXAMPLETOKEN") {
		t.Errorf("the credential was printed:\n%s", output)
	}
}

func TestRepoNewRefusesACredentialBeforeItCreatesOrPushesAnything(t *testing.T) {
	// Arrange: --remote was never checked. The command created the directory,
	// scaffolded it, committed, wrote the credential-bearing URL into
	// .git/config, pushed to it over the network, printed it, and only then had
	// the manifest refuse. Every one of those happened before the refusal.
	h := adoptedFixture(t, "payments")
	const withToken = "https://user:ghp_EXAMPLETOKEN@example.invalid/acme/console.git"

	// Act
	code, output := h.run("repo", "new", "console", "--remote", withToken)

	// Assert
	if code != ExitUsage {
		t.Errorf("`repo new --remote <credential>` exited %d, want %d:\n%s", code, ExitUsage, output)
	}
	if strings.Contains(output, "ghp_EXAMPLETOKEN") {
		t.Errorf("the refusal quoted the credential back:\n%s", output)
	}
	if _, err := os.Stat(h.path("console")); err == nil {
		t.Error("the repository was created before the remote was checked")
	}
	written, err := os.ReadFile(h.path("vat.yaml"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if strings.Contains(string(written), "ghp_EXAMPLETOKEN") {
		t.Error("the credential reached the manifest")
	}
}

func TestRepoNewStillAcceptsAnOrdinaryRemote(t *testing.T) {
	// Arrange: the guard must not have closed the door on the normal case. The
	// push fails because the host does not resolve, which `repo new` reports as
	// a warning rather than a failure.
	h := adoptedFixture(t, "payments")

	// Act
	code, output := h.run("repo", "new", "console",
		"--remote", "https://example.invalid/acme/console.git")

	// Assert
	if code != ExitOK {
		t.Errorf("an ordinary remote was refused:\n%s", output)
	}
	if _, err := os.Stat(h.path("console", ".git")); err != nil {
		t.Errorf("the repository was not created: %v", err)
	}
}

func TestAFailedRenamePutsTheManifestBackAsWellAsTheDirectory(t *testing.T) {
	// Arrange: writing the manifest and the .gitignore are two writes, and the
	// second can fail after the first succeeded. Rolling back only the
	// directory left the manifest naming the new name and the disk holding the
	// old one — the exact disagreement the rollback exists to prevent.
	h := adoptedFixture(t, "payments")
	// A directory where the .gitignore should be: readable as a path, writable
	// as a file by nothing.
	if err := os.Remove(h.path(".gitignore")); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if err := os.MkdirAll(h.path(".gitignore"), 0o755); err != nil {
		t.Fatalf("create: %v", err)
	}

	// Act
	code, output := h.run("repo", "rename", "payments", "billing")

	// Assert
	if code == ExitOK {
		t.Fatalf("the rename reported success while .gitignore could not be written:\n%s", output)
	}
	written, err := os.ReadFile(h.path("vat.yaml"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if strings.Contains(string(written), "billing") {
		t.Errorf("the manifest kept the new name after the rename failed:\n%s", written)
	}
	if !strings.Contains(string(written), "payments") {
		t.Errorf("the manifest lost the original name:\n%s", written)
	}
	if _, err := os.Stat(h.path("payments")); err != nil {
		t.Error("the directory was not put back under its original name")
	}
	if _, err := os.Stat(h.path("billing")); err == nil {
		t.Error("the renamed directory survived a failed rename")
	}
}

func TestHarnessAdoptBringsAHandWrittenDefinitionUnderTheContract(t *testing.T) {
	// Arrange: the state everybody adopting vat is actually in. They already
	// have agent files and skills written by hand, in one runtime's directory,
	// and moving them by hand is the step where adoption stops.
	h := adoptedFixture(t, "payments")
	writeFile(t, h.path(".claude", "agents", "reviewer.md"),
		"---\nname: reviewer\ndescription: Reviews a diff.\nmodel: opus\n---\n\n# Reviewer\n\nRead the diff and report.\n")
	writeFile(t, h.path(".claude", "skills", "deploy", "SKILL.md"),
		"---\nname: deploy\ndescription: Deploys one service.\n---\n\n# Deploy\n\n1. Build.\n2. Ship.\n")

	// Act: reports without --apply, writes with it.
	preview := h.mustRun("harness", "adopt")
	before, err := os.ReadFile(h.path(".claude", "agents", "reviewer.md"))
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if !strings.Contains(string(before), "Read the diff and report.") {
		t.Error("a preview rewrote the file it was previewing")
	}
	applied := h.mustRun("harness", "adopt", "--apply")

	// Assert
	for _, name := range []string{"reviewer", "deploy"} {
		if !strings.Contains(preview, name) {
			t.Errorf("the preview never mentioned %q:\n%s", name, preview)
		}
		if !strings.Contains(applied, name) {
			t.Errorf("applying never mentioned %q:\n%s", name, applied)
		}
	}
	role := readFile(t, h.path(".agents", "roles", "reviewer.md"))
	if !strings.Contains(role, "Read the diff and report.") {
		t.Errorf("the prose did not survive adoption:\n%s", role)
	}
	// A bare model is honoured only by a role targeting one runtime, so adopting
	// a Claude file must not silently claim Codex as well.
	if !strings.Contains(role, "runtimes:") || !strings.Contains(role, "claude") {
		t.Errorf("the adopted role does not say which runtime it came from:\n%s", role)
	}
	skill := readFile(t, h.path(".agents", "skills", "deploy", "SKILL.md"))
	if !strings.Contains(skill, "1. Build.") {
		t.Errorf("the procedure did not survive adoption:\n%s", skill)
	}
	// The adapter is regenerated, so what was the body becomes a pointer.
	adapter := readFile(t, h.path(".claude", "skills", "deploy", "SKILL.md"))
	if strings.Contains(adapter, "1. Build.") {
		t.Errorf("the adapter still carries a copy of the procedure:\n%s", adapter)
	}
	if code, output := h.run("lint", "--offline"); strings.Contains(output, "adapter-orphaned") {
		t.Errorf("adoption left an orphan behind (exit %d):\n%s", code, output)
	}
}

func TestHarnessAdoptRefusesToOverwriteADefinitionThatAlreadyExists(t *testing.T) {
	// Arrange: the canonical file is the one vat must never clobber. A hand
	// written adapter beside an existing definition is drift, not a candidate.
	h := adoptedFixture(t, "payments")
	h.mustRun("harness", "role", "new", "reviewer", "--description", "The real one.")
	writeFile(t, h.path(".claude", "agents", "reviewer.md"),
		"---\nname: reviewer\ndescription: An impostor.\n---\n\nDifferent prose.\n")
	original := readFile(t, h.path(".agents", "roles", "reviewer.md"))

	// Act
	output := h.mustRun("harness", "adopt", "--apply")

	// Assert
	if readFile(t, h.path(".agents", "roles", "reviewer.md")) != original {
		t.Error("adopt overwrote a canonical definition that already existed")
	}
	if !strings.Contains(output, "already") {
		t.Errorf("adopt did not say why it skipped the file:\n%s", output)
	}
}

func TestHarnessAdoptSaysSoWhenThereIsNothingToAdopt(t *testing.T) {
	// Arrange: every adapter present was generated by vat, which is the state a
	// workspace is in the day after it adopts. Silence would read as failure.
	h := adoptedFixture(t, "payments")

	// Act
	output := h.mustRun("harness", "adopt")

	// Assert
	if strings.TrimSpace(output) == "" {
		t.Error("`vat harness adopt` printed nothing at all")
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(content)
}
