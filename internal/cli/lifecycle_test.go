package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The commands in integration_test.go are the ones a workspace uses daily. The
// ones here are the layers on top of it — evidence, changesets, brain, and the
// scaffolding commands — driven end to end, because each of them writes records
// that outlive the session that created them and are wrong forever if the write
// is wrong once.

// adoptedFixture returns a workspace with the named repositories enrolled and
// the generated contracts committed, which is the state every layered command
// assumes it starts from.
func adoptedFixture(t *testing.T, repos ...string) *workspaceFixture {
	t.Helper()
	h := newFixture(t, repos...)
	h.mustRun("init", "--adopt", "--name", "acme")
	for _, name := range repos {
		commitAll(t, h, name)
	}
	return h
}

// brainFixture adds a brain repository, which the knowledge commands refuse to
// run without.
func brainFixture(t *testing.T, repos ...string) *workspaceFixture {
	t.Helper()
	h := adoptedFixture(t, repos...)
	h.mustRun("repo", "new", "brain", "--role", "brain", "--no-remote")
	h.mustRun("brain", "init")
	return h
}

func TestDoctorReportsEveryFindingAtOnceAndRepairsNothing(t *testing.T) {
	// Arrange: doctor runs in a loop while someone cleans a workspace, so one
	// finding per run would be unusable, and a doctor that repaired would make
	// the next run's output a report on itself.
	h := adoptedFixture(t, "payments")
	before, err := os.ReadFile(h.path("vat.yaml"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	// Act
	var report struct {
		Findings []struct {
			Section string `json:"section"`
			Subject string `json:"subject"`
			Status  string `json:"status"`
		} `json:"findings"`
	}
	h.runJSON(&report, "doctor")

	// Assert
	if len(report.Findings) == 0 {
		t.Fatal("doctor reported nothing at all; it should always describe the environment it judged")
	}
	for _, finding := range report.Findings {
		if finding.Status == "" || finding.Section == "" || finding.Subject == "" {
			t.Errorf("finding %+v is missing a section, subject, or status, so nobody can act on it", finding)
		}
	}
	after, err := os.ReadFile(h.path("vat.yaml"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(before) != string(after) {
		t.Error("doctor modified vat.yaml; diagnosis and repair are separate commands")
	}
}

func TestDoctorPrintsTheSameJudgementAsText(t *testing.T) {
	// Arrange: the human-readable form is the one most people ever see, and it
	// was the only rendering path with no test at all.
	h := adoptedFixture(t, "payments")

	// Act
	_, output := h.run("doctor")

	// Assert
	if !strings.Contains(output, "git") {
		t.Errorf("doctor never mentioned git, which is the one tool it cannot work without:\n%s", output)
	}
}

func TestABrainRecordIsNotCitableUntilAHumanPromotesIt(t *testing.T) {
	// Arrange: the promotion gate is the whole point of the layer. A record that
	// counted as a fact the moment it was written would be a wiki.
	h := brainFixture(t, "payments")
	h.mustRun("brain", "new", "decision", "--title", "Cancellation is v2 only", "--owner", "payments")

	// Act
	_, before := h.run("brain", "query", "cancellation")

	// Assert
	if strings.Contains(before, "active") {
		t.Errorf("a freshly written record already reads as active:\n%s", before)
	}

	// Act
	h.mustRun("brain", "promote", "D-0001", "--reviewer", "test")
	_, after := h.run("brain", "query", "cancellation")

	// Assert
	if !strings.Contains(after, "active") {
		t.Errorf("a promoted record is still not citable:\n%s", after)
	}
}

func TestBrainBuildAndCheckAgreeOnAWellFormedRepository(t *testing.T) {
	// Arrange: summaries are projections. Building them must not be able to
	// invalidate the records they were built from.
	h := brainFixture(t, "payments")
	h.mustRun("brain", "new", "goal", "--title", "Ship v2 cancellation", "--owner", "payments")

	// Act
	h.mustRun("brain", "build")
	code, output := h.run("brain", "check")

	// Assert
	if code != ExitOK {
		t.Errorf("`brain check` failed on a repository `brain build` had just produced:\n%s", output)
	}
}

func TestBrainReviewNamesEveryRecordItQueues(t *testing.T) {
	// Arrange: an unordered or unlabelled queue grows until it is ignored
	// wholesale, which is the failure this layer exists to prevent.
	h := brainFixture(t, "payments")
	h.mustRun("brain", "new", "decision", "--title", "Retries are not idempotent", "--owner", "payments")

	// Act
	var queue []struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	code := h.runJSON(&queue, "brain", "review")

	// Assert
	if code != ExitOK {
		t.Errorf("`brain review` exited %d on a healthy repository", code)
	}
	for _, entry := range queue {
		if entry.ID == "" {
			t.Errorf("a queue entry has no id, so nobody can act on it: %+v", entry)
		}
	}
}

func TestBrainSweepWithoutApplyChangesNothing(t *testing.T) {
	// Arrange: sweep demotes records. Reporting and acting are separate, so the
	// default must be a report.
	h := brainFixture(t, "payments")
	h.mustRun("brain", "new", "decision", "--title", "Pricing is per seat", "--owner", "payments")
	path := findRecord(t, h, "D-0001")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	// Act
	h.mustRun("brain", "sweep")

	// Assert
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(before) != string(after) {
		t.Error("`brain sweep` rewrote a record without --apply")
	}
}

// findRecord locates a record file by id anywhere under the brain repository.
func findRecord(t *testing.T, h *workspaceFixture, id string) string {
	t.Helper()
	var found string
	err := filepath.Walk(h.path("brain"), func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || found != "" {
			return err
		}
		if strings.HasPrefix(filepath.Base(path), id) {
			found = path
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if found == "" {
		t.Fatalf("no record file for %s", id)
	}
	return found
}

func TestAChangesetClosesOnlyWithAnAcceptanceStatement(t *testing.T) {
	// Arrange: every repository's own suite passing is not evidence the pieces
	// work together, and that gap is where multi-repo changes break.
	h := adoptedFixture(t, "payments", "console")
	h.mustRun("changeset", "new", "Move cancellation to v2", "--repos", "payments,console")

	// Act
	code, output := h.run("changeset", "close", "CS-0001")

	// Assert
	if code == ExitOK {
		t.Errorf("a changeset closed with no acceptance statement:\n%s", output)
	}
}

func TestAChangesetUndoPlanReturnsInReverseEnrolmentOrder(t *testing.T) {
	// Arrange: consumers must be returned before the contract they depend on,
	// or a window exists where a consumer expects an interface already gone.
	h := adoptedFixture(t, "payments", "console")
	h.mustRun("changeset", "new", "Move cancellation to v2", "--repos", "payments,console")

	// Act
	plan := h.mustRun("changeset", "undo-plan", "CS-0001")

	// Assert
	payments := strings.Index(plan, "payments")
	console := strings.Index(plan, "console")
	if payments < 0 || console < 0 {
		t.Fatalf("the plan names neither repository:\n%s", plan)
	}
	if console > payments {
		t.Errorf("the plan returns payments before console, which is enrolment order, not reverse:\n%s", plan)
	}
}

func TestAnAbandonedChangesetStaysReadable(t *testing.T) {
	// Arrange: abandoning is a decision worth keeping. Deleting the record
	// destroys the only account of what was attempted.
	h := adoptedFixture(t, "payments")
	h.mustRun("changeset", "new", "Try the v2 shape", "--repos", "payments")

	// Act
	h.mustRun("changeset", "abandon", "CS-0001", "--reason", "the interface moved")
	shown := h.mustRun("changeset", "show", "CS-0001")

	// Assert
	if !strings.Contains(shown, "abandoned") {
		t.Errorf("an abandoned changeset does not say so:\n%s", shown)
	}
	if !strings.Contains(shown, "the interface moved") {
		t.Errorf("the reason was not kept:\n%s", shown)
	}
}

func TestChangesetListReportsOpenWorkSeparately(t *testing.T) {
	// Arrange: open cross-repository work with no closing evidence is the thing
	// worth surfacing; a flat list of everything ever done is not.
	h := adoptedFixture(t, "payments")
	h.mustRun("changeset", "new", "Still going", "--repos", "payments")
	h.mustRun("changeset", "new", "Given up on", "--repos", "payments")
	h.mustRun("changeset", "abandon", "CS-0002", "--reason", "superseded")

	// Act
	var open []struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	h.runJSON(&open, "changeset", "list", "--open")

	// Assert
	if len(open) != 1 || open[0].ID != "CS-0001" {
		t.Errorf("--open listed %+v, want only the open changeset CS-0001", open)
	}
}

func TestEvidenceRendersABriefingAnAgentCanBeHanded(t *testing.T) {
	// Arrange: the packet exists to be given to a worker. If it does not render
	// the objective, the repositories, and the acceptance, it is not a contract.
	h := adoptedFixture(t, "payments", "console")
	h.mustRun("evidence", "new", "EV-0001", "Move cancellation to v2",
		"--repos", "payments,console",
		"--acceptance", "cancel-then-refund passes end to end")

	// Act
	briefing := h.mustRun("evidence", "show", "EV-0001", "--markdown")

	// Assert
	for _, required := range []string{"Move cancellation to v2", "payments", "console", "cancel-then-refund"} {
		if !strings.Contains(briefing, required) {
			t.Errorf("the briefing never mentions %q:\n%s", required, briefing)
		}
	}
}

func TestEvidenceListAndCheckAgreeOnWhatExists(t *testing.T) {
	// Arrange
	h := adoptedFixture(t, "payments")
	h.mustRun("evidence", "new", "EV-0001", "Tidy the ordering docs",
		"--repos", "payments", "--acceptance", "the doc names the current revision")

	// Act
	var packets []struct {
		ID string `json:"id"`
	}
	h.runJSON(&packets, "evidence", "list")
	code, output := h.run("evidence", "check", "EV-0001")

	// Assert
	if len(packets) != 1 || packets[0].ID != "EV-0001" {
		t.Errorf("`evidence list` reported %+v, want the one packet just written", packets)
	}
	if code == ExitUsage {
		t.Errorf("`evidence check` could not find a packet `evidence list` had just reported:\n%s", output)
	}
}

func TestExecFailsLoudlyWhenARepositoryCommandFails(t *testing.T) {
	// Arrange: a runner that reported success while a repository failed is the
	// exact behaviour the shell loop this replaces was guilty of.
	h := adoptedFixture(t, "payments")

	// Act
	code, output := h.run("exec", "--", "sh", "-c", "exit 3")

	// Assert
	if code == ExitOK {
		t.Errorf("`vat exec` exited 0 after the command failed everywhere:\n%s", output)
	}
	if !strings.Contains(output, "payments") {
		t.Errorf("the failure does not name the repository it happened in:\n%s", output)
	}
}

func TestExecChecksRunsTheCanonicalCheck(t *testing.T) {
	// Arrange: --checks is how a workspace runs "whatever this repository calls
	// passing" without every caller knowing what that is.
	h := adoptedFixture(t, "payments")
	addCheck(t, h, "payments", "echo canonical-check-ran")

	// Act
	code, output := h.run("exec", "--checks")

	// Assert
	if code != ExitOK {
		t.Errorf("`vat exec --checks` exited %d:\n%s", code, output)
	}
	if !strings.Contains(output, "canonical-check-ran") {
		t.Errorf("the declared check never ran:\n%s", output)
	}
}

func TestSyncClonesARepositoryThatIsNotOnDiskYet(t *testing.T) {
	// Arrange: a manifest entry with no clone is the normal state of a fresh
	// machine, and cloning it is the one write sync is allowed to make.
	h := adoptedFixture(t, "payments")
	if err := os.RemoveAll(h.path("payments")); err != nil {
		t.Fatalf("remove: %v", err)
	}

	// Act
	code, output := h.run("sync")

	// Assert
	if code != ExitOK {
		t.Errorf("`vat sync` exited %d rather than cloning what was missing:\n%s", code, output)
	}
	if _, err := os.Stat(h.path("payments", ".git")); err != nil {
		t.Errorf("sync did not clone the missing repository:\n%s", output)
	}
}

func TestHarnessRolesReportsWhatEachRoleMayWriteTo(t *testing.T) {
	// Arrange: being trusted to decide something is not the same as being able
	// to act on it, so the write target is the field that matters most here.
	h := adoptedFixture(t, "payments")
	h.mustRun("harness", "role", "new", "planner", "--description", "plans work")
	h.mustRun("harness", "role", "new", "builder", "--writes", "payments", "--description", "does work")

	// Act
	var roles []roleSummary
	h.runJSON(&roles, "harness", "roles")

	// Assert
	byName := map[string]roleSummary{}
	for _, role := range roles {
		byName[role.Name] = role
	}
	if len(byName["planner"].Writes) != 0 {
		t.Errorf("planner declares no write target but reports %v", byName["planner"].Writes)
	}
	if len(byName["builder"].Writes) == 0 {
		t.Error("builder declares a write target and reports none")
	}
	for name, role := range byName {
		if len(role.Runtimes) == 0 {
			t.Errorf("role %s generates no adapter for any runtime", name)
		}
	}
}

func TestRepoNewArrivesWithAContractAlreadyInIt(t *testing.T) {
	// Arrange: the difference between a workspace an agent can work in and one
	// it has to guess at is whether the contract was there on arrival.
	h := adoptedFixture(t, "payments")

	// Act
	output := h.mustRun("repo", "new", "console", "--no-remote", "--group", "frontend")

	// Assert
	for _, expected := range []string{
		filepath.Join("console", ".git"),
		filepath.Join("console", "AGENTS.md"),
	} {
		if _, err := os.Stat(h.path(expected)); err != nil {
			t.Errorf("`repo new` left no %s:\n%s", expected, output)
		}
	}
	written, err := os.ReadFile(h.path("vat.yaml"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(written), "console") {
		t.Error("the new repository is on disk but not in the manifest")
	}
	ignore, err := os.ReadFile(h.path(".gitignore"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(ignore), "console") {
		t.Error("the new repository is in the manifest but not excluded from the root history")
	}
}

func TestInitBuildsAManifestFromATabSeparatedList(t *testing.T) {
	// Arrange: adopting an existing estate usually starts from a list someone
	// already has, not from repositories already cloned.
	h := newFixture(t)
	listing := h.path("repos.tsv")
	body := "# name\torigin\n" +
		"payments\thttps://example.invalid/acme/payments.git\n" +
		"\n" +
		"docs\thttps://example.invalid/acme/docs.git\n"
	if err := os.WriteFile(listing, []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Act
	output := h.mustRun("init", "--name", "acme", "--from-tsv", listing)

	// Assert
	written, err := os.ReadFile(h.path("vat.yaml"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	for _, name := range []string{"payments", "docs"} {
		if !strings.Contains(string(written), name) {
			t.Errorf("%s was in the listing but not the manifest:\n%s", name, output)
		}
	}
	// The role is guessed from the name and written down where it is visible
	// and correctable, rather than re-derived silently on every run.
	if !strings.Contains(string(written), "role: docs") {
		t.Errorf("the guessed role was not recorded:\n%s", written)
	}
}

func TestInitRejectsAListingItCannotRead(t *testing.T) {
	// Arrange: a line that is not name-then-origin must fail the run, not
	// silently enrol a repository with an empty remote.
	h := newFixture(t)
	listing := h.path("repos.tsv")
	if err := os.WriteFile(listing, []byte("payments\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Act
	code, output := h.run("init", "--name", "acme", "--from-tsv", listing)

	// Assert
	if code == ExitOK {
		t.Errorf("a malformed listing was accepted:\n%s", output)
	}
	if _, err := os.Stat(h.path("vat.yaml")); err == nil {
		t.Error("a manifest was written from a listing that could not be read")
	}
}

func TestMetricsRecordAppendsToTheLedgerAndHistoryReadsItBack(t *testing.T) {
	// Arrange: the measure that matters is the trend, so a snapshot nobody can
	// read back later measures nothing.
	h := adoptedFixture(t, "payments")

	// Act
	h.mustRun("metrics", "--record")
	history := h.mustRun("metrics", "--history")

	// Assert
	if strings.TrimSpace(history) == "" {
		t.Error("`metrics --history` reported nothing after a snapshot was recorded")
	}
}

func TestFitStartsRecommendingTheHarnessOnceAgentsAreInTheLoop(t *testing.T) {
	// Arrange: the harness problems — adapters drifting apart, a contract with
	// no stated trust boundary — exist at one repository. Gating the advice on
	// repository count would tell the people who need it most to adopt nothing.
	h := adoptedFixture(t, "payments")

	// Act
	var verdicts []struct {
		Layer string `json:"layer"`
		Adopt bool   `json:"adopt"`
	}
	h.runJSON(&verdicts, "fit", "--repos", "1", "--people", "1", "--agent-sessions", "20")

	// Assert
	found := false
	for _, verdict := range verdicts {
		if verdict.Layer != "harness" {
			continue
		}
		found = true
		if !verdict.Adopt {
			t.Errorf("fit told a workspace running 20 agent sessions to skip the harness: %+v", verdict)
		}
	}
	if !found {
		t.Errorf("fit never judged the harness layer at all: %+v", verdicts)
	}
}

// docs/ADOPTION.md tells a reader which rules a CI checkout can answer, and the
// answer rests on a fact about vat's own design: the governed repositories are
// excluded from the workspace's history, so the workspace repository checked
// out on its own has the manifest and none of the trees it names.
//
// The advice was wrong before it was checked. It said to run `vat lint` and
// `vat doctor`, and both fail there — every repository reported missing, which
// is true and tells the reader nothing about what they changed. What makes the
// corrected advice safe is that the failures are confined to `repo/`: if a rule
// outside that prefix ever starts needing a working tree, the documented
// selectors quietly begin failing builds for a reason the documentation does
// not mention.
func TestOnlyRepositoryRulesNeedTheWorkingTreesToBePresent(t *testing.T) {
	// Arrange
	h := newFixture(t)
	h.mustRun("init", "--name", "acme")
	h.mustRun("repo", "add", "payments",
		"--origin", "https://example.invalid/acme/payments.git", "--no-clone")
	h.mustRun("repo", "add", "console",
		"--origin", "https://example.invalid/acme/console.git", "--no-clone")

	// Act
	code, output := h.run("lint", "--offline")

	// Assert
	if code == 0 {
		t.Fatalf("lint passed with every repository absent, so this test is checking nothing:\n%s", output)
	}
	for _, line := range strings.Split(output, "\n") {
		if !strings.Contains(line, "FAIL") && !strings.Contains(line, "WARN") {
			continue
		}
		if strings.Contains(line, "lint ") || strings.Contains(line, "errors") {
			continue // the summary line, not a finding
		}
		if !strings.Contains(line, "repo/") {
			t.Errorf("a rule outside repo/ needs the working trees, which docs/ADOPTION.md does not account for:\n%s", line)
		}
	}
}

// docs/HARNESS.md says `vat repo add|new|adopt|rename|archive` re-renders
// automatically, so a new repository arrives with a contract already in it.
// --no-clone returned before the render, so the one command documented to
// leave the harness current left it stale: `repo add` reported success and the
// very next `vat lint` reported drift in the file that command had just made
// wrong. The generated region comes from the manifest, and the manifest changed
// whether or not anything reached disk.
func TestRegisteringARepositoryWithoutCloningStillRendersTheContract(t *testing.T) {
	// Arrange
	h := newFixture(t)
	h.mustRun("init", "--name", "acme")

	// Act
	h.mustRun("repo", "add", "payments",
		"--origin", "https://example.invalid/acme/payments.git", "--no-clone")

	// Assert
	code, output := h.run("lint", "--offline", "--only", "harness")
	if code != 0 {
		t.Errorf("the harness is stale immediately after the command that renders it:\n%s", output)
	}
}
