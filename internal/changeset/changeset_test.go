package changeset_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/takealook97/vat/internal/changeset"
)

var reference = time.Date(2026, 8, 25, 0, 0, 0, 0, time.UTC)

func TestSaveAndLoadRoundTripAChangeset(t *testing.T) {
	// Arrange
	root := t.TempDir()
	original := changeset.New("CS-0001", "Move cancellation to v2", reference)
	original = changeset.WithParticipant(original, changeset.Participant{
		Name: "payments", RollbackPoint: "aaaaaaaaaaaa",
		Checks: []changeset.CheckRun{{Command: "make check", Status: "pass"}},
	})

	// Act
	if err := changeset.Save(root, original); err != nil {
		t.Fatalf("Save returned an error: %v", err)
	}
	loaded, err := changeset.Load(root, "CS-0001")

	// Assert
	if err != nil {
		t.Fatalf("Load returned an error: %v", err)
	}
	if loaded.Objective != original.Objective {
		t.Errorf("objective = %q, want %q", loaded.Objective, original.Objective)
	}
	if len(loaded.Repositories) != 1 || loaded.Repositories[0].RollbackPoint != "aaaaaaaaaaaa" {
		t.Errorf("participants lost in round trip: %+v", loaded.Repositories)
	}
}

func TestWithParticipantReplacesByNameWithoutMutatingTheOriginal(t *testing.T) {
	// Arrange
	original := changeset.New("CS-0001", "x", reference)
	original = changeset.WithParticipant(original, changeset.Participant{Name: "a", Revision: "one"})

	// Act
	updated := changeset.WithParticipant(original, changeset.Participant{Name: "a", Revision: "two"})

	// Assert
	if len(updated.Repositories) != 1 || updated.Repositories[0].Revision != "two" {
		t.Errorf("participant not replaced: %+v", updated.Repositories)
	}
	if original.Repositories[0].Revision != "one" {
		t.Error("the original changeset was mutated")
	}
}

func TestNextIDContinuesFromTheHighestExisting(t *testing.T) {
	// Arrange
	root := t.TempDir()
	for _, id := range []string{"CS-0001", "CS-0007"} {
		if err := changeset.Save(root, changeset.New(id, "x", reference)); err != nil {
			t.Fatalf("Save returned an error: %v", err)
		}
	}

	// Act
	next, err := changeset.NextID(root)

	// Assert
	if err != nil {
		t.Fatalf("NextID returned an error: %v", err)
	}
	if next != "CS-0008" {
		t.Errorf("NextID = %q, want CS-0008", next)
	}
}

func TestFullyVerifiedRequiresAPassingCheckAtAKnownRevision(t *testing.T) {
	// Arrange
	noRevision := changeset.WithParticipant(changeset.New("CS-0001", "x", reference),
		changeset.Participant{Name: "a", Checks: []changeset.CheckRun{{Status: "pass"}}})
	failing := changeset.WithParticipant(changeset.New("CS-0002", "x", reference),
		changeset.Participant{Name: "a", Revision: "abc", Checks: []changeset.CheckRun{{Status: "fail"}}})
	complete := changeset.WithParticipant(changeset.New("CS-0003", "x", reference),
		changeset.Participant{Name: "a", Revision: "abc", Checks: []changeset.CheckRun{{Status: "pass"}}})

	// Act & Assert
	if noRevision.FullyVerified() {
		t.Error("a check with no revision counted as verification")
	}
	if failing.FullyVerified() {
		t.Error("a failing check counted as verification")
	}
	if !complete.FullyVerified() {
		t.Error("a passing check at a known revision was not accepted")
	}
}

func TestValidateRefusesToCloseWithoutAnIntegrationOutcome(t *testing.T) {
	// Arrange: per-repository checks passing is not the same as the pieces
	// working together.
	closed := changeset.New("CS-0001", "x", reference)
	closed.Status = changeset.StatusClosed
	closed = changeset.WithParticipant(closed, changeset.Participant{
		Name: "a", Revision: "abc", RollbackPoint: "def",
		Checks: []changeset.CheckRun{{Status: "pass"}},
	})

	// Act
	problems := changeset.Validate(closed, true)

	// Assert
	joined := strings.Join(problems, "\n")
	if !strings.Contains(joined, "integration_acceptance") {
		t.Errorf("closing without an integration outcome was accepted: %v", problems)
	}
}

func TestValidateReportsAParticipantWithNoReturnPoint(t *testing.T) {
	// Arrange
	current := changeset.WithParticipant(changeset.New("CS-0001", "x", reference),
		changeset.Participant{Name: "a"})

	// Act
	problems := changeset.Validate(current, true)

	// Assert
	if !strings.Contains(strings.Join(problems, "\n"), "cannot be undone") {
		t.Errorf("a participant with no return point was accepted: %v", problems)
	}
}

func TestValidateRejectsASingleRepositoryChangesetAsJustACommit(t *testing.T) {
	// Arrange
	empty := changeset.New("CS-0001", "x", reference)

	// Act
	problems := changeset.Validate(empty, false)

	// Assert
	if !strings.Contains(strings.Join(problems, "\n"), "no repositories enrolled") {
		t.Errorf("an empty changeset was accepted: %v", problems)
	}
}

func TestRollbackPlanUndoesInReverseEnrolmentOrder(t *testing.T) {
	// Arrange: the contract owner is enrolled first, so it must be undone last.
	current := changeset.New("CS-0001", "x", reference)
	current = changeset.WithParticipant(current,
		changeset.Participant{Name: "payments", RollbackPoint: "aaaaaaaaaaaa"})
	current = changeset.WithParticipant(current,
		changeset.Participant{Name: "console", RollbackPoint: "bbbbbbbbbbbb"})

	// Act
	steps, err := current.RollbackPlan()

	// Assert
	if err != nil {
		t.Fatalf("RollbackPlan returned an error: %v", err)
	}
	if len(steps) != 2 {
		t.Fatalf("step count = %d, want 2", len(steps))
	}
	if !strings.Contains(steps[0], "console") {
		t.Errorf("first step = %q, want the consumer first", steps[0])
	}
	if !strings.Contains(steps[1], "payments") {
		t.Errorf("second step = %q, want the contract owner last", steps[1])
	}
}

func TestRollbackPlanRefusesWhenAReturnPointWasNeverRecorded(t *testing.T) {
	// Arrange
	current := changeset.WithParticipant(changeset.New("CS-0001", "x", reference),
		changeset.Participant{Name: "payments"})

	// Act
	_, err := current.RollbackPlan()

	// Assert
	if err == nil || !strings.Contains(err.Error(), "payments") {
		t.Fatalf("want an error naming the repository, got %v", err)
	}
}

func TestAgeDaysCountsFromTheOpeningDate(t *testing.T) {
	// Arrange
	current := changeset.New("CS-0001", "x", time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC))

	// Act
	age := current.AgeDays(reference)

	// Assert
	if age != 24 {
		t.Errorf("AgeDays = %d, want 24", age)
	}
}

// The identifier decides where the file is written. An unchecked traversing id
// escaped the workspace entirely — through Load from a command-line argument,
// and through Save from the `id:` field of a changeset somebody committed.
// Writing outside the root is the boundary the whole tool rests on, and the
// defect class that retracted three releases.
func TestATraversingIdentifierIsRefusedRatherThanFollowed(t *testing.T) {
	// Arrange
	root := t.TempDir()
	traversing := []string{
		"../../../escaped",
		"../escaped",
		"CS-0001/../../escaped",
		"/absolute",
		"CS-0001; rm -rf /",
		"",
	}

	// Act & Assert
	for _, id := range traversing {
		if _, err := changeset.Load(root, id); err == nil {
			t.Errorf("Load accepted %q", id)
		}
		if err := changeset.Save(root, changeset.Changeset{ID: id}); err == nil {
			t.Errorf("Save accepted %q", id)
		}
	}
	// Nothing may have been created anywhere above the root.
	parent := filepath.Dir(root)
	entries, err := os.ReadDir(parent)
	if err != nil {
		t.Fatalf("read parent: %v", err)
	}
	for _, entry := range entries {
		if strings.Contains(entry.Name(), "escaped") {
			t.Errorf("a file escaped the workspace: %s", filepath.Join(parent, entry.Name()))
		}
	}
}

// The refusal must not cost the valid case anything.
func TestAWellFormedIdentifierStillRoundTrips(t *testing.T) {
	// Arrange
	root := t.TempDir()
	set := changeset.New("CS-0001", "Move cancellation to v2", reference)

	// Act
	if err := changeset.Save(root, set); err != nil {
		t.Fatalf("Save: %v", err)
	}
	loaded, err := changeset.Load(root, "CS-0001")

	// Assert
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.ID != "CS-0001" || loaded.Objective != set.Objective {
		t.Errorf("round trip lost content: %+v", loaded)
	}
}

// The identifier decides where the next Save writes, so a file whose id
// disagrees with its own name would be read as one changeset and written back
// over another.
func TestAChangesetWhoseIdentifierDisagreesWithItsFilenameIsRefused(t *testing.T) {
	// Arrange
	root := t.TempDir()
	dir := filepath.Join(root, changeset.Dir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create: %v", err)
	}
	body := "id: CS-0009\nobjective: impostor\nstatus: open\nopened_at: 2026-08-26\nrepositories: []\n"
	if err := os.WriteFile(filepath.Join(dir, "CS-0001.yaml"), []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Act
	_, err := changeset.Load(root, "CS-0001")

	// Assert
	if err == nil {
		t.Fatal("a changeset claiming another identifier was accepted")
	}
	if !strings.Contains(err.Error(), "CS-0009") {
		t.Errorf("the error does not say which identifier it found: %v", err)
	}
}

// AgeDays cannot answer for a date it could not parse, so it returns zero — and
// zero reads as a fact. One malformed line made a changeset permanently new,
// invisible to changeset/open-too-long, which is the rule that finds cross-repo
// work somebody abandoned. The published schema says only "string", so an
// honest tool writing an ISO timestamp lands in the same place.
func TestAnOpenDateThatCannotBeReadIsReported(t *testing.T) {
	// Arrange
	cases := []struct {
		name string
		when string
	}{
		{"absent", ""},
		{"an ISO timestamp, which the schema does not forbid", "2020-01-01T00:00:00Z"},
		{"a date nobody writes", "01/02/2020"},
		{"not a date at all", "yesterday"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			set := changeset.Changeset{
				ID: "CS-0001", Objective: "something", Status: changeset.StatusOpen,
				OpenedAt:     tc.when,
				Repositories: []changeset.Participant{{Name: "api", RollbackPoint: "abc123"}},
			}

			// Act
			problems := changeset.Validate(set, true)

			// Assert
			if len(problems) == 0 {
				t.Fatalf("opened_at %q was accepted", tc.when)
			}
			joined := strings.Join(problems, "; ")
			if !strings.Contains(joined, "opened_at") {
				t.Errorf("the problem does not name the field: %s", joined)
			}
			if !strings.Contains(joined, "YYYY-MM-DD") {
				t.Errorf("the problem does not say what shape to write: %s", joined)
			}
		})
	}
}

// The date vat itself writes must pass, or every changeset it creates is
// invalid the moment it is created.
func TestTheOpenDateVatWritesIsAccepted(t *testing.T) {
	// Arrange
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	set := changeset.New("CS-0001", "something", now)
	set = changeset.WithParticipant(set, changeset.Participant{Name: "api", RollbackPoint: "abc123"})

	// Act
	problems := changeset.Validate(set, true)

	// Assert
	if len(problems) != 0 {
		t.Errorf("a changeset vat just created does not validate: %v", problems)
	}
}
