package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/takealook97/vat/internal/brain"
	"github.com/takealook97/vat/internal/ui"
)

// standaloneFixture drives commands against a brain no workspace stands over.
type standaloneFixture struct {
	t     *testing.T
	base  string
	brain string
}

func newStandalone(t *testing.T) *standaloneFixture {
	t.Helper()
	base := t.TempDir()
	return &standaloneFixture{t: t, base: base, brain: filepath.Join(base, "brain")}
}

// run invokes vat from cwd with --brain-dir set to dir; an empty dir leaves
// the brain to discovery.
func (s *standaloneFixture) run(cwd, dir string, args ...string) (int, string) {
	s.t.Helper()
	var out, errOut bytes.Buffer
	env := &Env{
		Printer:  ui.NewWith(&out, &errOut, false),
		Now:      testNow,
		Cwd:      cwd,
		BrainDir: dir,
		Yes:      true,
	}
	code := dispatch(context.Background(), env, Root(), args, nil)
	return code, out.String() + errOut.String()
}

func (s *standaloneFixture) mustRun(args ...string) string {
	s.t.Helper()
	code, output := s.run(s.base, s.brain, args...)
	if code != ExitOK {
		s.t.Fatalf("`vat --brain-dir %s %s` exited %d:\n%s", s.brain, strings.Join(args, " "), code, output)
	}
	return output
}

func (s *standaloneFixture) initWithIdentity() {
	s.t.Helper()
	s.mustRun("brain", "init")
	git(s.t, s.brain, "init", "--quiet", "--initial-branch", "main", ".")
	git(s.t, s.brain, "config", "user.name", "Fixture Reviewer")
	git(s.t, s.brain, "config", "user.email", "reviewer@example.com")
}

func (s *standaloneFixture) record(id string) brain.Record {
	s.t.Helper()
	store, err := brain.Load(s.brain)
	if err != nil {
		s.t.Fatalf("load: %v", err)
	}
	record, ok := store.ByID()[id]
	if !ok {
		s.t.Fatalf("no record %s", id)
	}
	return record
}

func TestStandaloneBrainLifecycle(t *testing.T) {
	isolateGitIdentity(t)
	s := newStandalone(t)
	s.initWithIdentity()

	declared, err := brain.ReadMarkerPolicy(s.brain)
	if err != nil {
		t.Fatalf("marker policy: %v", err)
	}
	if declared.Resolve() != brain.DefaultMarkerPolicy() || len(declared) != len(brain.PolicyKeys()) {
		t.Fatalf("standalone init wrote %v, want every default declared", declared)
	}

	s.mustRun("brain", "new", "decision", "--title", "Orders own their idempotency keys")
	s.mustRun("brain", "new", "decision", "--title", "Orders own their keys per tenant")
	s.mustRun("brain", "new", "gap", "--title", "Retries can double-submit")
	s.mustRun("brain", "new", "decision", "--title", "A decision we take back")
	s.mustRun("brain", "build")
	s.mustRun("brain", "check")
	if output := s.mustRun("brain", "query", "idempotency"); !strings.Contains(output, "D-0001") {
		t.Errorf("query did not find the record:\n%s", output)
	}
	s.mustRun("brain", "promote", "D-0001")
	if record := s.record("D-0001"); record.Status != brain.StatusActive || record.ReviewedBy == "" {
		t.Fatalf("promotion under the manual gate did not attribute: %+v", record.Metadata)
	}
	s.mustRun("brain", "supersede", "D-0001", "D-0002")
	// require_promotion_gate defaults on without a workspace.
	if record := s.record("D-0002"); record.Status != brain.StatusProvisional {
		t.Errorf("the replacement is %s; the default policy holds it for promotion", record.Status)
	}
	s.mustRun("brain", "revoke", "D-0003", "--reason", "taken back")
	s.mustRun("brain", "resolve", "G-0001", "--reason", "fixed upstream")
	s.mustRun("brain", "sweep")
	s.mustRun("brain", "archive", "--apply")
	s.mustRun("brain", "review")

	// Discovery: no flag, run from inside the brain.
	nested := filepath.Join(s.brain, "decisions")
	if code, output := s.run(nested, "", "brain", "check"); code != ExitOK {
		t.Fatalf("a brain found by its marker was not checked (exit %d):\n%s", code, output)
	}
}

func TestStandaloneUsesTheMarkerPolicy(t *testing.T) {
	isolateGitIdentity(t)
	s := newStandalone(t)
	s.initWithIdentity()
	marker := readFile(t, filepath.Join(s.brain, brain.MarkerFile))
	writeFile(t, filepath.Join(s.brain, brain.MarkerFile),
		strings.Replace(marker, "require_promotion_gate: true", "require_promotion_gate: false", 1))
	s.mustRun("brain", "new", "decision", "--title", "First")
	s.mustRun("brain", "new", "decision", "--title", "Second")
	s.mustRun("brain", "supersede", "D-0001", "D-0002")
	if record := s.record("D-0002"); record.Status != brain.StatusActive {
		t.Errorf("the marker turned the gate off and the replacement is %s", record.Status)
	}

	writeFile(t, filepath.Join(s.brain, brain.MarkerFile), "schema: 1\npolicy:\n  promotion_gate: sometimes\n")
	if code, output := s.run(s.base, s.brain, "brain", "check"); code == ExitOK || !strings.Contains(output, "promotion_gate") {
		t.Errorf("an invalid marker policy was not refused (exit %d):\n%s", code, output)
	}
}

func TestStandaloneRejectsGovernedEvidenceOperations(t *testing.T) {
	isolateGitIdentity(t)
	s := newStandalone(t)
	s.initWithIdentity()

	cases := []struct {
		name string
		args []string
		want string
	}{
		{"owner", []string{"brain", "new", "gap", "--title", "x", "--owner", "payments"}, "needs a workspace"},
		{"source path", []string{"brain", "new", "gap", "--title", "x", "--source-path", "README.md"}, "--owner"},
		{"current-state", []string{"brain", "new", "gap", "--title", "x", "--claim", "current-state"}, "--owner"},
		{"drifted review", []string{"brain", "review", "--drifted"}, "needs a workspace"},
		{"adopt", []string{"brain", "adopt", "brain"}, "needs a workspace"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, output := s.run(s.base, s.brain, tc.args...)
			if code != ExitUsage || !strings.Contains(output, tc.want) {
				t.Errorf("exit %d, want %d naming %q:\n%s", code, ExitUsage, tc.want, output)
			}
		})
	}

	t.Run("agent gate on a non-goal record", func(t *testing.T) {
		writeFile(t, filepath.Join(s.brain, brain.MarkerFile), "schema: 1\npolicy:\n  promotion_gate: agent\n")
		s.mustRun("brain", "new", "decision", "--title", "Agents cannot vouch for this")
		s.mustRun("brain", "new", "goal", "--title", "An outcome a goal can name")
		code, output := s.run(s.base, s.brain, "brain", "promote", "D-0001", "O-0001")
		if code != ExitFindings || !strings.Contains(output, "standalone brain") {
			t.Fatalf("exit %d, want the agent refusal:\n%s", code, output)
		}
		if s.record("O-0001").Status != brain.StatusActive {
			t.Errorf("the goal exception was lost: %s", s.record("O-0001").Status)
		}
		if s.record("D-0001").Status == brain.StatusActive {
			t.Error("a decision was promoted through the agent gate without evidence")
		}
	})
}

func TestExplicitBrainDirDoesNotEscapeWriteRoot(t *testing.T) {
	s := newStandalone(t)
	for _, escape := range []string{"../outside", "nested/../../outside"} {
		code, output := s.run(s.base, s.brain, "brain", "init", escape)
		if code != ExitUsage || !strings.Contains(output, "outside --brain-dir") {
			t.Errorf("init %s: exit %d:\n%s", escape, code, output)
		}
		if _, err := os.Stat(filepath.Join(s.base, "outside")); err == nil {
			t.Fatalf("init %s wrote outside --brain-dir", escape)
		}
	}
	// A directory inside the boundary is fine.
	s.mustRun("brain", "init", "team")
	if !brain.IsBrain(filepath.Join(s.brain, "team")) {
		t.Error("a nested directory inside --brain-dir was not scaffolded")
	}

	// A symbolic link inside the boundary must not carry the scaffold out.
	outside := filepath.Join(s.base, "elsewhere")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(s.brain, "link")); err != nil {
		t.Skipf("symbolic links unavailable: %v", err)
	}
	if code, output := s.run(s.base, s.brain, "brain", "init", "link"); code != ExitUsage {
		t.Errorf("init through a link leaving --brain-dir: exit %d:\n%s", code, output)
	}
	if brain.IsBrain(outside) {
		t.Error("init followed a link out of --brain-dir")
	}
}

func TestMalformedWorkspaceDoesNotFallBack(t *testing.T) {
	s := newStandalone(t)
	s.mustRun("brain", "init")
	// A broken manifest above a brain with a marker: falling through to the
	// marker would run a governed brain under the wrong policy.
	writeFile(t, filepath.Join(s.base, "vat.yaml"), "version: 1\nworkspace: [broken\n")
	code, output := s.run(s.brain, "", "brain", "check")
	if code == ExitOK || !strings.Contains(output, "vat.yaml") {
		t.Errorf("a malformed manifest was hidden behind the standalone brain (exit %d):\n%s", code, output)
	}
	code, output = s.run(s.base, s.brain, "brain", "check")
	if code == ExitOK || !strings.Contains(output, "vat.yaml") {
		t.Errorf("--brain-dir hid a malformed manifest (exit %d):\n%s", code, output)
	}
}

func TestStandaloneInitWithoutBrainDirSaysHow(t *testing.T) {
	s := newStandalone(t)
	code, output := s.run(s.base, "", "brain", "init")
	if code != ExitUsage || !strings.Contains(output, "--brain-dir") {
		t.Errorf("exit %d:\n%s", code, output)
	}
	code, output = s.run(s.base, "", "brain", "check")
	if code != ExitUsage {
		t.Errorf("no workspace and no brain: exit %d:\n%s", code, output)
	}
}

// The workspace's brain named with --brain-dir is still the workspace's: its
// policy, its repository list, no policy block in the marker.
func TestBrainDirNamingTheWorkspaceBrainKeepsTheWorkspace(t *testing.T) {
	h := brainFixture(t)
	var out, errOut bytes.Buffer
	for _, args := range [][]string{{"brain", "init"}, {"brain", "new", "decision", "--title", "x"}, {"brain", "check"}} {
		env := &Env{Printer: ui.NewWith(&out, &errOut, false), Now: testNow, Cwd: h.root, BrainDir: "brain", Yes: true}
		if code := dispatch(context.Background(), env, Root(), args, nil); code != ExitOK {
			t.Fatalf("%v: exit %d:\n%s%s", args, code, out.String(), errOut.String())
		}
	}
	declared, err := brain.ReadMarkerPolicy(h.path("brain"))
	if err != nil || len(declared) != 0 {
		t.Errorf("the workspace brain's marker declares %v (%v)", declared, err)
	}
}

func TestBrainDirCaseAliasKeepsWorkspacePolicy(t *testing.T) {
	h := brainFixture(t)
	canonical, err := os.Stat(h.path("brain"))
	if err != nil {
		t.Fatal(err)
	}
	alias, err := os.Stat(h.path("BRAIN"))
	if err != nil || !os.SameFile(canonical, alias) {
		t.Skip("case-sensitive filesystem")
	}
	writeFile(t, h.path("brain/.brain"), "schema: 1\npolicy:\n  promotion_gate: auto\n")
	bc, err := resolveBrain(&Env{Cwd: h.root, Root: h.root, BrainDir: "BRAIN", Now: testNow})
	if err != nil {
		t.Fatal(err)
	}
	if bc.Workspace == nil || bc.Policy.PromotionGate != "manual" {
		t.Fatalf("same directory with different casing became standalone=%v gate=%s", bc.Standalone(), bc.Policy.PromotionGate)
	}
}

func TestSameDirComparesFilesystemIdentity(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real")
	if err := os.Mkdir(real, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skip(err)
	}
	if !sameDir(real, link) {
		t.Error("a symbolic link to the directory is the same directory")
	}
	if sameDir(real, filepath.Join(dir, "other")) {
		t.Error("an existing directory matched a path that does not exist")
	}
	if !sameDir(filepath.Join(link, "new"), filepath.Join(real, "new")) {
		t.Error("paths that do not exist yet should compare through their resolved parent")
	}
}
