package cli

import (
	"os"
	"strings"
	"testing"

	"github.com/takealook97/vat/internal/changeset"
)

func TestClosedChangesetCanRecordKnowledgeWithoutRewritingCompletion(t *testing.T) {
	h := adoptedFixture(t, "payments", "console")
	h.mustRun("changeset", "new", "Move cancellation", "--repos", "payments,console", "--decision", "D-0001")
	h.mustRun("changeset", "close", "CS-0001", "--acceptance", "integration passes", "--force")
	before, err := changeset.Load(h.root, "CS-0001")
	if err != nil {
		t.Fatal(err)
	}
	h.mustRun("changeset", "record", "CS-0001", "--knowledge", "D-0001,D-0002,D-0002")
	after, err := changeset.Load(h.root, "CS-0001")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(after.Knowledge, ",") != "D-0001,D-0002" || strings.Join(after.Decisions, ",") != "D-0001" {
		t.Fatalf("links = %+v", after)
	}
	if after.Status != before.Status || after.Acceptance != before.Acceptance || after.ClosedAt != before.ClosedAt || after.Objective != before.Objective {
		t.Fatalf("completion changed: %+v", after)
	}
	output := h.mustRun("changeset", "show", "CS-0001")
	if !strings.Contains(output, "knowledge: D-0001, D-0002") || !strings.Contains(output, "decisions: D-0001") {
		t.Fatalf("missing knowledge: %s", output)
	}
}

func TestChangesetKnowledgeFlagsRejectInvalidChoicesWithoutSaving(t *testing.T) {
	for _, command := range []string{"record", "close"} {
		for _, flags := range [][]string{nil, {"--decision", "D-0001"}, {"--knowledge", "D-0001", "--no-record", "none"}, {"--no-record", " "}, {"--knowledge", ""}, {"--knowledge", ","}} {
			if command == "close" && flags == nil {
				continue
			}
			t.Run(command+strings.Join(flags, ":"), func(t *testing.T) {
				h := adoptedFixture(t, "payments")
				h.mustRun("changeset", "new", "Move cancellation", "--repos", "payments")
				before, err := os.ReadFile(h.path(changeset.Path("CS-0001")))
				if err != nil {
					t.Fatal(err)
				}
				args := []string{"changeset", command, "CS-0001"}
				if command == "close" {
					args = append(args, "--acceptance", "integration passes", "--force")
				}
				args = append(args, flags...)
				code, output := h.run(args...)
				if code != ExitUsage {
					t.Fatalf("exit %d, want usage: %s", code, output)
				}
				after, err := os.ReadFile(h.path(changeset.Path("CS-0001")))
				if err != nil {
					t.Fatal(err)
				}
				if string(after) != string(before) {
					t.Fatal("refusal modified changeset")
				}
			})
		}
	}
}

func TestAbandonedChangesetRefusesKnowledgeRecording(t *testing.T) {
	h := adoptedFixture(t, "payments")
	h.mustRun("changeset", "new", "Move cancellation", "--repos", "payments")
	h.mustRun("changeset", "abandon", "CS-0001")
	code, output := h.run("changeset", "record", "CS-0001", "--knowledge", "D-0001")
	if code != ExitUsage || !strings.Contains(output, "abandoned") {
		t.Fatalf("exit %d: %s", code, output)
	}
}

func TestCloseCanRecordWhyNoKnowledgeWasProduced(t *testing.T) {
	h := adoptedFixture(t, "payments")
	h.mustRun("changeset", "new", "Move cancellation", "--repos", "payments")
	h.mustRun("changeset", "close", "CS-0001", "--acceptance", "integration passes", "--force", "--no-record", "Only a mechanical rename")
	output := h.mustRun("changeset", "show", "CS-0001")
	if !strings.Contains(output, "no record: Only a mechanical rename") {
		t.Fatalf("missing reason: %s", output)
	}
	var current map[string]any
	h.runJSON(&current, "changeset", "show", "CS-0001")
	if current["no_record_reason"] != "Only a mechanical rename" {
		t.Fatalf("missing JSON reason: %v", current)
	}
}

func TestChangesetKnowledgeIDsMustResolveWhenABrainIsDeclared(t *testing.T) {
	for _, command := range []string{"record", "close"} {
		t.Run(command, func(t *testing.T) {
			h := brainFixture(t, "payments")
			h.mustRun("brain", "new", "decision", "--title", "Cancellation is idempotent")
			h.mustRun("changeset", "new", "Move cancellation", "--repos", "payments")
			before, err := os.ReadFile(h.path(changeset.Path("CS-0001")))
			if err != nil {
				t.Fatal(err)
			}
			args := []string{"changeset", command, "CS-0001", "--knowledge", "D-0001,D-9998,D-9999"}
			if command == "close" {
				args = append(args, "--acceptance", "integration passes", "--force")
			}
			code, output := h.run(args...)
			if code != ExitUsage || !strings.Contains(output, "D-9998") || !strings.Contains(output, "D-9999") {
				t.Fatalf("exit %d, missing unresolved ids: %s", code, output)
			}
			after, err := os.ReadFile(h.path(changeset.Path("CS-0001")))
			if err != nil {
				t.Fatal(err)
			}
			if string(after) != string(before) {
				t.Fatal("unresolved ids modified changeset")
			}
			args[4] = "D-0001"
			h.mustRun(args...)
		})
	}
}

func TestChangesetRecordsKnowledgeBeforeAndAfterClosing(t *testing.T) {
	h := adoptedFixture(t, "payments")
	h.mustRun("changeset", "new", "Mechanical rename", "--repos", "payments")
	h.mustRun("changeset", "record", "CS-0001", "--no-record", "Existing decisions still apply")
	current, err := changeset.Load(h.root, "CS-0001")
	if err != nil {
		t.Fatal(err)
	}
	if current.Status != changeset.StatusOpen || current.NoRecordReason != "Existing decisions still apply" {
		t.Fatalf("open record: %+v", current)
	}
	h.mustRun("changeset", "close", "CS-0001", "--acceptance", "integration passes", "--force", "--knowledge", "D-0001,D-0001")
	current, err = changeset.Load(h.root, "CS-0001")
	if err != nil {
		t.Fatal(err)
	}
	if current.Status != changeset.StatusClosed || strings.Join(current.Knowledge, ",") != "D-0001" || current.NoRecordReason != "" {
		t.Fatalf("close record: %+v", current)
	}
	code, refusal := h.run("changeset", "record", "CS-0001", "--no-record", "Knowledge already exists")
	if code != ExitUsage || !strings.Contains(refusal, "D-0001") {
		t.Fatalf("exit %d: %s", code, refusal)
	}
	output := h.mustRun("changeset", "show", "CS-0001")
	if strings.Contains(output, "no record:") || !strings.Contains(output, "knowledge: D-0001") {
		t.Fatalf("missing knowledge: %s", output)
	}
}

func TestAuthorisingDecisionAloneDoesNotRecordProducedKnowledge(t *testing.T) {
	h := adoptedFixture(t, "payments")
	h.mustRun("changeset", "new", "Move cancellation", "--repos", "payments", "--decision", "D-0001")
	h.mustRun("changeset", "close", "CS-0001", "--acceptance", "integration passes", "--force")
	output := h.mustRun("lint", "--only", "changeset/closed-unrecorded")
	if !strings.Contains(output, "closed without knowledge") {
		t.Fatalf("missing warning: %s", output)
	}
	h.mustRun("changeset", "record", "CS-0001", "--knowledge", "D-0002")
	output = h.mustRun("lint", "--only", "changeset/closed-unrecorded")
	if strings.Contains(output, "closed without knowledge") {
		t.Fatalf("warning after recording: %s", output)
	}
}

func TestCloseRefusesNoRecordOnceKnowledgeExistsWithoutSaving(t *testing.T) {
	h := adoptedFixture(t, "payments")
	h.mustRun("changeset", "new", "Move cancellation", "--repos", "payments")
	h.mustRun("changeset", "record", "CS-0001", "--knowledge", "D-0001,D-0002")
	before, err := os.ReadFile(h.path(changeset.Path("CS-0001")))
	if err != nil {
		t.Fatal(err)
	}
	code, output := h.run("changeset", "close", "CS-0001", "--acceptance", "integration passes", "--force", "--no-record", "None")
	if code != ExitUsage || !strings.Contains(output, "D-0001, D-0002") {
		t.Fatalf("exit %d: %s", code, output)
	}
	after, err := os.ReadFile(h.path(changeset.Path("CS-0001")))
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("refusal modified changeset")
	}
}

func TestCloseRefusesTerminalChangesetsWithoutSaving(t *testing.T) {
	for _, status := range []changeset.Status{changeset.StatusAbandoned, changeset.StatusRolledBack, changeset.StatusClosed} {
		for _, flags := range [][]string{nil, {"--knowledge", "D-0001"}, {"--no-record", "Mechanical rename"}} {
			t.Run(string(status)+strings.Join(flags, ":"), func(t *testing.T) {
				h := adoptedFixture(t, "payments")
				h.mustRun("changeset", "new", "Move cancellation", "--repos", "payments")
				switch status {
				case changeset.StatusAbandoned:
					h.mustRun("changeset", "abandon", "CS-0001")
				case changeset.StatusClosed:
					h.mustRun("changeset", "close", "CS-0001", "--acceptance", "Original outcome", "--force")
				case changeset.StatusRolledBack:
					current, err := changeset.Load(h.root, "CS-0001")
					if err != nil {
						t.Fatal(err)
					}
					current.Status = status
					if err := changeset.Save(h.root, current); err != nil {
						t.Fatal(err)
					}
				}
				before, err := os.ReadFile(h.path(changeset.Path("CS-0001")))
				if err != nil {
					t.Fatal(err)
				}
				args := append([]string{"changeset", "close", "CS-0001", "--acceptance", "x", "--force"}, flags...)
				code, output := h.run(args...)
				if code != ExitUsage || !strings.Contains(output, string(status)) {
					t.Fatalf("exit %d, want usage naming %s: %s", code, status, output)
				}
				after, err := os.ReadFile(h.path(changeset.Path("CS-0001")))
				if err != nil {
					t.Fatal(err)
				}
				if string(before) != string(after) {
					t.Fatal("refusal modified changeset")
				}
			})
		}
	}
}
