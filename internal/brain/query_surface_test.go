package brain_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/takealook97/vat/internal/brain"
)

// What an agent may cite as current truth is the brain's entire promise, and
// the selectors that decide it were the least exercised code in the package.
// A selector that quietly widens is worse than one that fails: the answer still
// arrives, and it is a superseded or unreviewed claim stated as fact.

func TestAnswerableReturnsOnlyActiveRecords(t *testing.T) {
	// Arrange: one record per status that is not active, so a selector that
	// widened to any of them would be caught by this one test.
	root, _ := newStore(t)
	writeRecord(t, root, "decisions/D-0001-active.md",
		"id: D-0001\nkind: decision\nstatus: active\nclaim_kind: intent\n", "# Active")
	writeRecord(t, root, "decisions/D-0002-provisional.md",
		"id: D-0002\nkind: decision\nstatus: provisional\nclaim_kind: intent\n", "# Provisional")
	writeRecord(t, root, "decisions/D-0003-superseded.md",
		"id: D-0003\nkind: decision\nstatus: superseded\nclaim_kind: intent\n", "# Superseded")
	store := reload(t, root)

	// Act
	answerable := store.Answerable()

	// Assert
	if len(answerable) != 1 {
		t.Fatalf("Answerable returned %d records, want only the active one: %+v", len(answerable), answerable)
	}
	if answerable[0].ID != "D-0001" {
		t.Errorf("Answerable returned %q, want D-0001", answerable[0].ID)
	}
}

func TestWithStatusSelectsExactlyThatStatus(t *testing.T) {
	// Arrange
	root, _ := newStore(t)
	writeRecord(t, root, "decisions/D-0001-active.md",
		"id: D-0001\nkind: decision\nstatus: active\nclaim_kind: intent\n", "# Active")
	writeRecord(t, root, "decisions/D-0002-provisional.md",
		"id: D-0002\nkind: decision\nstatus: provisional\nclaim_kind: intent\n", "# Provisional")
	writeRecord(t, root, "decisions/D-0003-also-provisional.md",
		"id: D-0003\nkind: decision\nstatus: provisional\nclaim_kind: intent\n", "# Also provisional")
	store := reload(t, root)

	// Act
	provisional := store.WithStatus(brain.StatusProvisional)

	// Assert
	if len(provisional) != 2 {
		t.Fatalf("WithStatus(provisional) returned %d records, want 2: %+v", len(provisional), provisional)
	}
	// SortRecords orders the result, so two identical runs agree.
	if provisional[0].ID > provisional[1].ID {
		t.Errorf("WithStatus returned an unsorted result: %s before %s",
			provisional[0].ID, provisional[1].ID)
	}
	if empty := store.WithStatus(brain.StatusRevoked); len(empty) != 0 {
		t.Errorf("WithStatus(revoked) returned %d records, want none", len(empty))
	}
}

func TestRelReportsThePathInsideTheBrainRatherThanOnThisMachine(t *testing.T) {
	// Arrange: the path is printed and written into projections that are
	// committed, so an absolute one would put a personal directory in git.
	root, _ := newStore(t)
	writeRecord(t, root, "decisions/D-0001-a-decision.md",
		"id: D-0001\nkind: decision\nstatus: active\nclaim_kind: intent\n", "# A decision")
	store := reload(t, root)

	// Act
	records := store.Answerable()

	// Assert
	if len(records) != 1 {
		t.Fatalf("expected one record, got %d", len(records))
	}
	relative := records[0].Rel()
	if filepath.IsAbs(relative) {
		t.Errorf("Rel returned an absolute path: %s", relative)
	}
	if relative != "decisions/D-0001-a-decision.md" {
		t.Errorf("Rel = %q, want the slash-separated path inside the brain", relative)
	}
}

func TestClaimKindValidityIsClosedAndItsErrorTextListsEveryKind(t *testing.T) {
	// Arrange & Act & Assert: an unknown claim kind must not be accepted, and
	// the message that rejects it has to name the alternatives or the user is
	// left guessing.
	for _, kind := range []brain.ClaimKind{brain.ClaimCurrentState, brain.ClaimHistorical, brain.ClaimIntent} {
		if !kind.Valid() {
			t.Errorf("%q is a documented claim kind but Valid() rejected it", kind)
		}
		if !strings.Contains(brain.ClaimKinds(), string(kind)) {
			t.Errorf("ClaimKinds() does not mention %q, so an error message would omit it", kind)
		}
	}
	for _, kind := range []brain.ClaimKind{"", "guess", "CURRENT_STATE"} {
		if kind.Valid() {
			t.Errorf("Valid() accepted %q", kind)
		}
	}
}

// Scoring by raw occurrence count is arithmetic, not relevance: a long record
// that repeats one query word beats a short record that answers all three. The
// long record is usually the sprawling one nobody has split up yet, so the
// ranking actively prefers the least useful document in the repository.
func TestQueryRanksAllTermsMatchedAboveOneTermRepeated(t *testing.T) {
	// Arrange
	root, _ := newStore(t)
	writeRecord(t, root, "memory/2026-05/M-0001-long.md",
		"id: M-0001\nstatus: active\nclaim_kind: historical\n",
		"# M-0001 — The long one\n\n"+strings.Repeat("Retries and more retries. ", 40))
	writeRecord(t, root, "memory/2026-05/M-0002-short.md",
		"id: M-0002\nstatus: active\nclaim_kind: historical\n",
		"# M-0002 — The short one\n\nRetries break idempotency for payments.")
	store := reload(t, root)

	// Act
	hits := brain.Query(store, []string{"retries", "idempotency", "payments"}, brain.QueryOptions{})

	// Assert
	if len(hits) < 2 {
		t.Fatalf("both records should match: %+v", hits)
	}
	if hits[0].ID != "M-0002" {
		t.Errorf("the record answering every term ranked below one repeating a single term: %+v", hits)
	}
}
