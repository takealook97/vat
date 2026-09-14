package cli

import (
	"os"
	"strings"
	"testing"
)

// One push to an active repository drifts every claim that repository owns, so
// the unit the work arrives in is a repository and the unit the command
// accepted was a record. A workspace holding 53 unpromoted records and another
// needing twenty re-verifications after one merge are the same defect: the gate
// was fine, the walk to it was one record wide.
//
// Batching changes how many records one invocation walks. It changes nothing
// about what the gate permits, which is what the refusal tests below pin.

func TestPromoteAcceptsSeveralRecordsAtOnce(t *testing.T) {
	// Arrange
	h := brainFixture(t, "payments")
	h.mustRun("brain", "new", "decision", "--title", "Orders own their idempotency keys")
	h.mustRun("brain", "new", "decision", "--title", "Refunds settle next day")

	// Act
	output := h.mustRun("brain", "promote", "D-0001", "D-0002", "--reviewer", "alex")

	// Assert
	for _, id := range []string{"D-0001", "D-0002"} {
		record, err := os.ReadFile(findRecord(t, h, id))
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		if !strings.Contains(string(record), "status: active") {
			t.Errorf("%s was not promoted:\n%s\n%s", id, output, record)
		}
	}
}

func TestPromoteCanSelectEverythingOneRepositoryOwns(t *testing.T) {
	// Arrange: a repository is the unit a re-verification actually arrives in.
	h := brainFixture(t, "payments", "console")
	h.mustRun("brain", "new", "gap", "--title", "Ordering is not retry-safe",
		"--claim", "current-state", "--owner", "payments", "--source-path", "README.md")
	h.mustRun("brain", "new", "gap", "--title", "Refund reads are uncached",
		"--claim", "current-state", "--owner", "payments", "--source-path", "README.md")
	h.mustRun("brain", "new", "gap", "--title", "Console paginates twice",
		"--claim", "current-state", "--owner", "console", "--source-path", "README.md")

	// Act
	output := h.mustRun("brain", "promote", "--owner", "payments", "--reviewer", "alex")

	// Assert
	for _, id := range []string{"G-0001", "G-0002"} {
		record, err := os.ReadFile(findRecord(t, h, id))
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		if !strings.Contains(string(record), "status: active") {
			t.Errorf("%s is owned by payments and was not promoted:\n%s", id, output)
		}
	}
	other, err := os.ReadFile(findRecord(t, h, "G-0003"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if strings.Contains(string(other), "status: active") {
		t.Errorf("a claim owned by another repository was swept up:\n%s", other)
	}
}

func TestPromoteReportsEveryRefusalRatherThanStoppingAtTheFirst(t *testing.T) {
	// Arrange: these commands run in a loop while somebody clears a queue. One
	// refusal per run makes that loop unbearable, and the second record here is
	// promotable — stopping early would hide that too.
	h := brainFixture(t, "payments")
	h.mustRun("brain", "new", "gap", "--title", "Ordering is not retry-safe",
		"--claim", "current-state", "--owner", "payments", "--source-path", "README.md")
	h.mustRun("brain", "new", "decision", "--title", "Orders own their idempotency keys")
	h.mustRun("brain", "promote", "G-0001", "--reviewer", "alex")
	h.mustRun("brain", "revoke", "G-0001", "--reason", "superseded by a rewrite")

	// Act
	code, output := h.run("brain", "promote", "G-0001", "D-0001", "--reviewer", "alex")

	// Assert
	if code == ExitOK {
		t.Fatalf("a batch containing a refused record exited clean:\n%s", output)
	}
	if !strings.Contains(output, "G-0001") || !strings.Contains(output, "D-0001") {
		t.Errorf("the batch did not report on both records:\n%s", output)
	}
	promoted, err := os.ReadFile(findRecord(t, h, "D-0001"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(promoted), "status: active") {
		t.Errorf("a promotable record was skipped because another one refused:\n%s", output)
	}
}

func TestPromoteStillRefusesAnUnattributedBatchWhenTheGateIsManual(t *testing.T) {
	// Arrange: batching must not become a way around the gate. Promotion is a
	// claim that a human checked something, and a batch is many such claims.
	h := brainFixture(t, "payments")
	h.mustRun("brain", "new", "decision", "--title", "Orders own their idempotency keys")
	h.mustRun("brain", "new", "decision", "--title", "Refunds settle next day")

	// Act
	code, output := h.run("brain", "promote", "D-0001", "D-0002")

	// Assert
	if code == ExitOK {
		t.Fatalf("records became canonical with nobody attached to the judgement:\n%s", output)
	}
}

func TestPromoteRefusesBothAnOwnerAndExplicitRecords(t *testing.T) {
	// Arrange: the two are different selections, and silently preferring one
	// would promote a set the caller did not ask for.
	h := brainFixture(t, "payments")
	h.mustRun("brain", "new", "decision", "--title", "Orders own their idempotency keys")

	// Act
	code, output := h.run("brain", "promote", "D-0001", "--owner", "payments", "--reviewer", "alex")

	// Assert
	if code == ExitOK {
		t.Fatalf("two conflicting selections were accepted:\n%s", output)
	}
}
