package cli

import (
	"os"
	"strings"
	"testing"
)

// `source_ref` has always been specified as `<repo>@<revision>[:<path>]`. The
// parser returned the path, promotion round-tripped it, and the template vat
// writes into every new brain advertised it — but no command could produce one,
// so every claim in every real workspace pinned a repository and nothing finer.
// That is why drift could only ever mean "the repository moved": with no path,
// there is nothing narrower to ask about.

func TestAClaimCanPinTheFileItsEvidenceWasReadFrom(t *testing.T) {
	// Arrange
	h := brainFixture(t, "payments")

	// Act
	output := h.mustRun("brain", "new", "gap", "--title", "Ordering is not retry-safe",
		"--claim", "current-state", "--owner", "payments", "--source-path", "README.md")

	// Assert
	record, err := os.ReadFile(findRecord(t, h, "G-0001"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	body := string(record)
	if !strings.Contains(body, ":README.md") {
		t.Errorf("the record pins no evidence file:\n%s\n%s", output, body)
	}
	if !strings.Contains(body, "source_ref: payments@") {
		t.Errorf("the record lost its repository and revision:\n%s", body)
	}
}

func TestAnEvidencePathThatIsNotInTheRepositoryIsRefused(t *testing.T) {
	// Arrange: a path that resolves to nothing is worse than no path at all. It
	// reads as precision, and every later re-check silently asks about a file
	// that was never there.
	h := brainFixture(t, "payments")

	// Act
	code, output := h.run("brain", "new", "gap", "--title", "Ordering is not retry-safe",
		"--claim", "current-state", "--owner", "payments", "--source-path", "docs/never-written.md")

	// Assert
	if code == ExitOK {
		t.Fatalf("a claim was pinned to a file the repository does not have:\n%s", output)
	}
	if !strings.Contains(output, "docs/never-written.md") {
		t.Errorf("the refusal does not name the path that was wrong:\n%s", output)
	}
}

func TestAnEvidencePathWithoutACurrentStateClaimIsRefused(t *testing.T) {
	// Arrange: only a claim about the present carries provenance. Accepting the
	// flag elsewhere and dropping it would tell the caller they recorded
	// something they did not.
	h := brainFixture(t, "payments")

	// Act
	code, output := h.run("brain", "new", "decision", "--title", "Orders own their idempotency keys",
		"--source-path", "README.md")

	// Assert
	if code == ExitOK {
		t.Fatalf("an evidence path was accepted for a record that records no evidence:\n%s", output)
	}
	if !strings.Contains(output, "--claim current-state") {
		t.Errorf("the refusal does not say what the flag needs:\n%s", output)
	}
}
