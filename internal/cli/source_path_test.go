package cli

import (
	"context"
	"github.com/takealook97/vat/internal/brain"
	"github.com/takealook97/vat/internal/gitx"
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

func TestAnEvidencePathWithoutAnOwnerIsRefused(t *testing.T) {
	// A source path needs a repository to resolve its pinned revision.
	h := brainFixture(t, "payments")

	// Act
	code, output := h.run("brain", "new", "decision", "--title", "Orders own their idempotency keys",
		"--source-path", "README.md")

	// Assert
	if code != ExitUsage {
		t.Fatalf("an evidence path without an owner did not return a usage error:\n%s", output)
	}
	if !strings.Contains(output, "--owner") {
		t.Errorf("the refusal does not say what the flag needs:\n%s", output)
	}
}

func TestAnyClaimKindCanPinEvidenceWithoutBecomingCurrentState(t *testing.T) {
	for _, claim := range []string{"", "historical", "intent", "current-state"} {
		for _, sourcePath := range []string{"", "README.md"} {
			t.Run(claim+"/"+sourcePath, func(t *testing.T) {
				h := brainFixture(t, "payments")
				args := []string{"brain", "new", "decision", "--title", "A pinned decision", "--owner", "payments"}
				if claim != "" {
					args = append(args, "--claim", claim)
				}
				if sourcePath != "" {
					args = append(args, "--source-path", sourcePath)
				}
				output := h.mustRun(args...)
				store, err := brain.Load(h.path("brain"))
				if err != nil {
					t.Fatal(err)
				}
				record := store.ByID()["D-0001"]
				revision, err := gitx.HeadRevision(context.Background(), h.path("payments"))
				if err != nil {
					t.Fatal(err)
				}
				want := "payments@" + revision
				if sourcePath != "" {
					want += ":" + sourcePath
				}
				if record.SourceRef != want || !strings.Contains(output, want) || string(record.ClaimKind) != claim {
					t.Fatalf("evidence changed or dropped: %+v; output %s", record.Metadata, output)
				}
				if claim == "current-state" {
					if record.ObservedAt != testNow.Format("2006-01-02") || record.RevalidateOn != "source-revision-change" {
						t.Fatalf("missing observation: %+v", record.Metadata)
					}
				} else if record.ObservedAt != "" || record.RevalidateOn != "" {
					t.Fatalf("non-current-state record gained an expiry clock: %+v", record.Metadata)
				}
			})
		}
	}
}

func TestNonCurrentStateEvidenceMustExistAtThePinnedRevision(t *testing.T) {
	for _, claim := range []string{"", "historical", "intent"} {
		t.Run(claim, func(t *testing.T) {
			h := brainFixture(t, "payments")
			writeFile(t, h.path("payments", "uncommitted.md"), "Not in HEAD")
			args := []string{"brain", "new", "decision", "--title", "Missing evidence", "--owner", "payments", "--source-path", "uncommitted.md"}
			if claim != "" {
				args = append(args, "--claim", claim)
			}
			code, output := h.run(args...)
			if code != ExitUsage || !strings.Contains(output, "does not hold uncommitted.md at") {
				t.Fatalf("pin accepted uncommitted evidence: %d %s", code, output)
			}
			store, err := brain.Load(h.path("brain"))
			if err != nil {
				t.Fatal(err)
			}
			if len(store.Records) != 0 {
				t.Fatalf("refusal wrote records: %+v", store.Records)
			}
		})
	}
}

func TestAgentGatePromotesPinnedHistoricalDecisionsAndMemory(t *testing.T) {
	h := brainFixture(t, "payments")
	manifest := readFile(t, h.path("vat.yaml"))
	writeFile(t, h.path("vat.yaml"), strings.Replace(manifest, "brain_promote: manual", "brain_promote: agent", 1))
	h.mustRun("brain", "new", "decision", "--title", "A historical decision", "--claim", "historical", "--owner", "payments", "--source-path", "README.md")
	h.mustRun("brain", "new", "memory", "--title", "A reusable observation", "--owner", "payments", "--source-path", "README.md")
	h.mustRun("brain", "promote", "D-0001", "M-0001")
	store, err := brain.Load(h.path("brain"))
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"D-0001", "M-0001"} {
		record := store.ByID()[id]
		if record.Status != brain.StatusActive || record.IsCurrentStateClaim() ||
			!brain.Citable(record, brain.CheckPolicy{StaleAfterDays: 90}, testNow.AddDate(1, 0, 0)) {
			t.Fatalf("promoted enduring knowledge expires: %+v", record.Metadata)
		}
	}
}

func TestHistoricalPromotionKeepsItsEvidencePinAfterTheSourceMoves(t *testing.T) {
	for _, gate := range []string{"manual", "agent", "auto"} {
		for _, reverified := range []bool{false, true} {
			t.Run(gate+"/"+map[bool]string{false: "plain", true: "reverified"}[reverified], func(t *testing.T) {
				h := brainFixture(t, "payments")
				manifest := readFile(t, h.path("vat.yaml"))
				writeFile(t, h.path("vat.yaml"), strings.Replace(manifest, "brain_promote: manual", "brain_promote: "+gate, 1))
				h.mustRun("brain", "new", "decision", "--title", "Historical evidence", "--claim", "historical", "--owner", "payments", "--source-path", "README.md")
				store, err := brain.Load(h.path("brain"))
				if err != nil {
					t.Fatal(err)
				}
				pin := store.ByID()["D-0001"].SourceRef
				// The cited file survives only in history; promotion must not replace its provenance with HEAD.
				git(t, h.path("payments"), "rm", "README.md")
				git(t, h.path("payments"), "commit", "--quiet", "-m", "Remove old evidence")
				args := []string{"brain", "promote", "D-0001"}
				if reverified {
					args = append(args, "--reverified")
				}
				h.mustRun(args...)
				store, err = brain.Load(h.path("brain"))
				if err != nil {
					t.Fatal(err)
				}
				record := store.ByID()["D-0001"]
				if record.Status != brain.StatusActive || record.SourceRef != pin {
					t.Fatalf("historical provenance changed: %+v; original pin %s", record.Metadata, pin)
				}
			})
		}
	}
}
