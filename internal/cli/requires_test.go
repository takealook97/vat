package cli

import (
	"os"
	"strings"
	"testing"
)

// `version: 1` in vat.yaml tells a reader which file format this is. It says
// nothing about which commands exist or what they refuse to write, so a
// workspace that depends on a behaviour has no way to say so — and finds out by
// having an older vat do the older thing to its files.

// requiring writes a `requires.vat` constraint into the fixture's manifest.
func requiring(t *testing.T, h *workspaceFixture, constraint string) {
	t.Helper()
	path := h.path("vat.yaml")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	updated := "requires:\n  vat: \"" + constraint + "\"\n" + string(content)
	if err := os.WriteFile(path, []byte(updated), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func TestAWorkspaceRequiringANewerVatIsRefusedRatherThanOperatedOn(t *testing.T) {
	// Arrange
	h := adoptedFixture(t, "payments")
	requiring(t, h, ">=9.0.0")

	// Act
	code, output := h.runAs("0.2.1", "status")

	// Assert
	if code != ExitFindings {
		t.Errorf("exit = %d, want %d for a workspace this build may not operate", code, ExitFindings)
	}
	for _, want := range []string{">=9.0.0", "0.2.1", "Upgrade vat"} {
		if !strings.Contains(output, want) {
			t.Errorf("output is missing %q:\n%s", want, output)
		}
	}
}

func TestAWorkspaceRequirementThisBuildSatisfiesIsInvisible(t *testing.T) {
	// Arrange
	h := adoptedFixture(t, "payments")
	requiring(t, h, ">=0.2.0 <1.0.0")

	// Act
	code, output := h.runAs("0.3.4", "status")

	// Assert
	if code != ExitOK {
		t.Errorf("exit = %d, want %d; the running version satisfies the constraint:\n%s",
			code, ExitOK, output)
	}
}

func TestAnUnstampedBuildIsNotRefusedByAVersionRequirement(t *testing.T) {
	// Arrange: a contributor's `go build` binary calls itself "dev". Refusing
	// there would enforce a constraint that cannot be evaluated either way, at
	// the cost of every checkout of the workspace.
	h := adoptedFixture(t, "payments")
	requiring(t, h, ">=9.0.0")

	// Act
	code, output := h.runAs("dev", "status")

	// Assert
	if code != ExitOK {
		t.Errorf("exit = %d, want %d for a build whose version says nothing:\n%s",
			code, ExitOK, output)
	}
}

func TestAConstraintNobodyCanParseIsReportedAgainstTheFile(t *testing.T) {
	// Arrange: refused for every reader, not only on the machine whose version
	// would have failed it. A constraint that cannot be parsed allows
	// everything, which is worse than having written none.
	h := adoptedFixture(t, "payments")
	requiring(t, h, "^0.3.0")

	// Act
	code, output := h.runAs("0.3.0", "status")

	// Assert
	if code == ExitOK {
		t.Errorf("a manifest with an unreadable requirement was accepted:\n%s", output)
	}
	if !strings.Contains(output, "requires.vat") {
		t.Errorf("output does not name the field at fault:\n%s", output)
	}
}

// The counterpart, and the one that had the wrong advice. A workspace pinned to
// a minor range refuses the next minor, and telling that person to upgrade
// names the one action that cannot help: they are already past the range, and
// every upgrade moves them further from it. This is not a corner case — pinning
// `>=0.4.0 <0.5.0` is how the field is meant to be used, so every such
// workspace meets this message on the release that follows.
func TestAWorkspaceOlderThanThisVatIsToldToWidenItRatherThanUpgrade(t *testing.T) {
	// Arrange
	h := adoptedFixture(t, "payments")
	requiring(t, h, ">=0.4.0 <0.5.0")

	// Act
	code, output := h.runAs("0.5.0", "status")

	// Assert
	if code != ExitFindings {
		t.Errorf("exit = %d, want %d for a workspace this build may not operate", code, ExitFindings)
	}
	if strings.Contains(output, "Upgrade vat") {
		t.Errorf("the hint tells a person already past the range to upgrade, which cannot help:\n%s", output)
	}
	for _, want := range []string{"<0.5.0", "0.5.0", "requires.vat"} {
		if !strings.Contains(output, want) {
			t.Errorf("output is missing %q:\n%s", want, output)
		}
	}
}

// A constraint failing at both ends is unsatisfiable by any version, so the
// advice must not claim that widening one end fixes it. Upgrading is still the
// nearer truth — the lower bound is the reachable one — so the old hint stands.
func TestAConstraintNoVersionCanSatisfyStillAdvisesTheReachableEnd(t *testing.T) {
	// Arrange
	h := adoptedFixture(t, "payments")
	requiring(t, h, ">=9.0.0 <0.1.0")

	// Act
	code, output := h.runAs("0.5.0", "status")

	// Assert
	if code != ExitFindings {
		t.Errorf("exit = %d, want %d", code, ExitFindings)
	}
	if !strings.Contains(output, "Upgrade vat") {
		t.Errorf("a constraint whose lower bound is unmet should still name the upgrade:\n%s", output)
	}
}
