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
