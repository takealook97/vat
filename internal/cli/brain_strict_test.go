package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

func cleanBrain(t *testing.T) *workspaceFixture {
	t.Helper()
	return brainFixture(t)
}

// warningOnlyBrain holds one revoked record still in the working set, which is
// brain/terminal-unarchived: a warning and nothing else.
func warningOnlyBrain(t *testing.T) *workspaceFixture {
	t.Helper()
	h := brainFixture(t)
	h.mustRun("brain", "new", "decision", "--title", "Taken back")
	h.mustRun("brain", "revoke", "D-0001", "--reason", "withdrawn")
	h.mustRun("brain", "build")
	return h
}

func TestCheckFailOnWarningIsOptIn(t *testing.T) {
	cases := []struct {
		name    string
		fixture func(*testing.T) *workspaceFixture
		args    []string
		want    int
	}{
		{"clean, default", cleanBrain, nil, ExitOK},
		{"clean, strict", cleanBrain, []string{"--fail-on-warning"}, ExitOK},
		{"warning only, default", warningOnlyBrain, nil, ExitOK},
		{"warning only, strict", warningOnlyBrain, []string{"--fail-on-warning"}, ExitFindings},
		{"error, default", errorBrain, nil, ExitFindings},
		{"error, strict", errorBrain, []string{"--fail-on-warning"}, ExitFindings},
		{"misuse, strict", cleanBrain, []string{"--fail-on-warning", "--only", "no-such-rule"}, ExitUsage},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := tc.fixture(t)
			code, output := h.run(append([]string{"brain", "check"}, tc.args...)...)
			if code != tc.want {
				t.Errorf("exit %d, want %d:\n%s", code, tc.want, output)
			}
		})
	}
	// The human report still lists the warning in both modes.
	h := warningOnlyBrain(t)
	for _, args := range [][]string{{"brain", "check"}, {"brain", "check", "--fail-on-warning"}} {
		if _, output := h.run(args...); !strings.Contains(output, "brain/terminal-unarchived") || !strings.Contains(output, "1 warning") {
			t.Errorf("%v did not print the warning:\n%s", args, output)
		}
	}
}

// errorBrain holds a record citing one that does not exist: brain/ref-missing.
func errorBrain(t *testing.T) *workspaceFixture {
	t.Helper()
	h := brainFixture(t)
	h.mustRun("brain", "new", "decision", "--title", "Cites nothing real", "--refs", "D-0404")
	h.mustRun("brain", "build")
	return h
}

func TestCheckFailOnWarningKeepsJSONAndOnlySemantics(t *testing.T) {
	h := warningOnlyBrain(t)
	var plain, strict []map[string]any
	if code := h.runJSON(&plain, "brain", "check"); code != ExitOK {
		t.Fatalf("default exit %d", code)
	}
	if code := h.runJSON(&strict, "brain", "check", "--fail-on-warning"); code != ExitFindings {
		t.Fatalf("strict exit %d", code)
	}
	a, _ := json.Marshal(plain)
	b, _ := json.Marshal(strict)
	if string(a) != string(b) || len(plain) == 0 {
		t.Errorf("the flag changed the JSON:\n%s\n%s", a, b)
	}
	// Only the selected rules count: narrowing past the warning passes.
	if code, output := h.run("brain", "check", "--fail-on-warning", "--only", "claim"); code != ExitOK {
		t.Errorf("a warning outside --only failed the run (exit %d):\n%s", code, output)
	}
	if code, _ := h.run("brain", "check", "--fail-on-warning", "--only", "terminal"); code != ExitFindings {
		t.Errorf("the selected warning did not fail the run (exit %d)", code)
	}
}

func TestCheckListIgnoresStrictExit(t *testing.T) {
	h := warningOnlyBrain(t)
	code, output := h.run("brain", "check", "--list", "--fail-on-warning")
	if code != ExitOK || !strings.Contains(output, "brain/terminal-unarchived") {
		t.Errorf("exit %d:\n%s", code, output)
	}
}
