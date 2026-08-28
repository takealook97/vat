package cli

import (
	"strings"
	"testing"
)

// Small contracts that only show up when a workspace is operated by scripts
// rather than by hand. Each of these was a real failed invocation during
// adoption, and each cost more to diagnose than it did to fix.

func TestRepoListWritesTabSeparatedRowsForScripts(t *testing.T) {
	// Arrange: replacing a hand-maintained roster file is only an improvement
	// if reading the replacement does not require a JSON parser.
	h := adoptedFixture(t, "payments", "console")

	// Act
	code, output := h.run("repo", "list", "--format", "tsv")

	// Assert
	if code != ExitOK {
		t.Fatalf("exit = %d:\n%s", code, output)
	}
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) != 2 {
		t.Fatalf("wrote %d rows for two repositories:\n%s", len(lines), output)
	}
	for _, line := range lines {
		fields := strings.Split(line, "\t")
		if len(fields) != 6 {
			t.Errorf("row %q has %d tab-separated fields, want 6", line, len(fields))
		}
	}
	// A header would have to be skipped by every consumer, and the aligned
	// table already exists for people.
	if strings.Contains(output, "NAME") {
		t.Errorf("the machine format carries a header:\n%s", output)
	}
}

func TestRepoListRefusesAFormatItDoesNotHave(t *testing.T) {
	// Arrange: silently falling back to the aligned table would hand a script
	// something it cannot parse and call it success.
	h := adoptedFixture(t, "payments")

	// Act
	code, output := h.run("repo", "list", "--format", "csv")

	// Assert
	if code != ExitUsage {
		t.Errorf("exit = %d, want %d for a format that does not exist:\n%s", code, ExitUsage, output)
	}
}

func TestDoctorAcceptsOfflineAsTheDefaultItAlreadyIs(t *testing.T) {
	// Arrange: `sync` and `lint` both take --offline. doctor did not, so a
	// script passing it everywhere failed on the one command that was already
	// offline.
	h := adoptedFixture(t, "payments")

	// Act
	code, output := h.run("doctor", "--offline")

	// Assert
	if code == ExitUsage {
		t.Errorf("doctor rejected --offline:\n%s", output)
	}
}

func TestDoctorRefusesOfflineAndNetworkTogether(t *testing.T) {
	// Arrange: accepting both would have to silently prefer one, and the
	// caller would not know which.
	h := adoptedFixture(t, "payments")

	// Act
	code, output := h.run("doctor", "--offline", "--network")

	// Assert
	if code != ExitUsage {
		t.Errorf("exit = %d, want %d for two flags asking opposite things:\n%s",
			code, ExitUsage, output)
	}
}

func TestDoctorNamesARemoteTheCloneKeptWithoutFaultingIt(t *testing.T) {
	// Arrange: a repository renamed on the forge usually keeps the old URL as a
	// second remote, so the old route still works. The manifest records one
	// identity and that stays true — but a migration that cannot see its own
	// leftovers is told the workspace is clean.
	h := adoptedFixture(t, "payments")
	git(t, h.path("payments"), "remote", "add", "legacy", "https://example.invalid/acme/old-payments.git")

	// Act
	code, output := h.run("doctor")

	// Assert
	if code != ExitOK {
		t.Errorf("exit = %d; an extra remote is context, not a fault:\n%s", code, output)
	}
	if !strings.Contains(output, "legacy") {
		t.Errorf("doctor does not name the remote the clone kept:\n%s", output)
	}
}
