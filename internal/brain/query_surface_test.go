package brain_test

import (
	"os"
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
	hits, _ := brain.Query(store, []string{"retries", "idempotency", "payments"}, brain.QueryOptions{})

	// Assert
	if len(hits) < 2 {
		t.Fatalf("both records should match: %+v", hits)
	}
	if hits[0].ID != "M-0002" {
		t.Errorf("the record answering every term ranked below one repeating a single term: %+v", hits)
	}
}

// The limit cuts what is shown, never what is counted. A caller that could not
// learn how many matched would print a truncated search as a complete one.
func TestQueryCountsEveryMatchBeyondTheLimit(t *testing.T) {
	// Arrange
	root, _ := newStore(t)
	for _, id := range []string{"M-0001", "M-0002", "M-0003"} {
		writeRecord(t, root, "memory/2026-05/"+id+"-retries.md",
			"id: "+id+"\nstatus: active\nclaim_kind: historical\n",
			"# "+id+" — Retries\n\nRetries were observed.")
	}
	store := reload(t, root)

	// Act
	cut, matched := brain.Query(store, []string{"retries"}, brain.QueryOptions{Limit: 1})
	whole, all := brain.Query(store, []string{"retries"}, brain.QueryOptions{})

	// Assert
	if len(cut) != 1 || matched != 3 {
		t.Errorf("limit 1 over three matches returned %d hits and a count of %d, want 1 and 3", len(cut), matched)
	}
	if len(whole) != 3 || all != 3 {
		t.Errorf("an uncut search returned %d hits and a count of %d, want 3 and 3", len(whole), all)
	}
}

// Equal text is not equal standing: a reviewed record is an answer and a
// provisional one is a draft. The draft's path sorts first here on purpose, so
// the only thing that can put the reviewed record on top is the preference
// under test — take it out and this fails rather than passing on path order.
func TestQueryRanksAReviewedRecordAboveAnUnreviewedOneWithTheSameText(t *testing.T) {
	// Arrange
	root := recordsOnly(t)
	body := "Retries within the window create a second order."
	writeRecord(t, root, "decisions/D-0001-draft.md",
		"id: D-0001\nstatus: provisional\n", "# D-0001 — Retries\n\n"+body)
	writeRecord(t, root, "decisions/D-0002-reviewed.md",
		"id: D-0002\nstatus: active\n", "# D-0002 — Retries\n\n"+body)

	// Act
	hits, _ := brain.Query(reload(t, root), []string{"retries"}, brain.QueryOptions{})

	// Assert
	if len(hits) < 2 {
		t.Fatalf("both records should match: %+v", hits)
	}
	if hits[0].ID != "D-0002" {
		t.Errorf("an unreviewed draft outranked the reviewed record saying the same thing: %+v", hits)
	}
}

// Each ranking mechanism below is pinned by a test that fails when that one
// mechanism is switched off. The suite used to pass with saturation, length
// normalisation, and the coverage weight each zeroed, so any of them could have
// been deleted, or retuned into its opposite, with every test green.
//
// These stores hold only the records under test. The root projections are part
// of the query surface and so of the average length every record is measured
// against; left in, a longer scaffold could decide these orderings instead of
// the mechanism each test is about.

// recordsOnly removes the root projections from a fresh store.
func recordsOnly(t *testing.T) string {
	t.Helper()
	root, _ := newStore(t)
	projections, err := filepath.Glob(filepath.Join(root, "*.md"))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	for _, path := range projections {
		if err := os.Remove(path); err != nil {
			t.Fatalf("remove %s: %v", path, err)
		}
	}
	return root
}

// Length normalisation: two records mention the term once, and the one that
// says little else is the better answer. The long record's path sorts first, so
// without the discount the tie goes to it.
func TestQueryDiscountsARecordForBeingLong(t *testing.T) {
	// Arrange
	root := recordsOnly(t)
	writeRecord(t, root, "decisions/D-0001-long.md", "id: D-0001\nstatus: active\n",
		"# D-0001 — Ordering\n\nRetries are mentioned once. "+strings.Repeat("Unrelated context about ledgers. ", 60))
	writeRecord(t, root, "decisions/D-0002-short.md", "id: D-0002\nstatus: active\n",
		"# D-0002 — Ordering\n\nRetries are mentioned once.")

	// Act
	hits, _ := brain.Query(reload(t, root), []string{"retries"}, brain.QueryOptions{})

	// Assert
	if len(hits) < 2 || hits[0].ID != "D-0002" {
		t.Errorf("a long record outranked a short one saying the same thing: %+v", hits)
	}
}

// Saturation: saying a word twenty times is not twenty times the relevance. A
// record repeating one term must not outrank one of the same length that
// answers both. Switching saturation off means letting repetition count without
// bound — a large k1, not zero: at zero repetition counts for nothing, which is
// the opposite failure and not the one this layer has to fear.
func TestQueryStopsRewardingRepetition(t *testing.T) {
	// Arrange
	root := recordsOnly(t)
	writeRecord(t, root, "decisions/D-0001-repeats.md", "id: D-0001\nstatus: active\n",
		"# D-0001 — Notes\n\n"+strings.Repeat("retries ", 20))
	writeRecord(t, root, "decisions/D-0002-answers.md", "id: D-0002\nstatus: active\n",
		"# D-0002 — Notes\n\nretries break idempotency "+strings.Repeat("filler ", 18))

	// Act
	hits, _ := brain.Query(reload(t, root), []string{"retries", "idempotency"}, brain.QueryOptions{})

	// Assert
	if len(hits) < 2 || hits[0].ID != "D-0002" {
		t.Errorf("repeating one term outranked answering both: %+v", hits)
	}
}

// Coverage: the length discount must not be able to bury the record that
// answers every term under a short one that answers one. Density alone lets
// it; the coverage weight is what makes matching every term decisive.
func TestQueryKeepsTheRecordAnsweringEveryTermAboveAShortDenseOne(t *testing.T) {
	// Arrange
	root := recordsOnly(t)
	writeRecord(t, root, "decisions/D-0001-dense.md", "id: D-0001\nstatus: active\n",
		"# D-0001 — Retries\n\nretries retries retries")
	writeRecord(t, root, "decisions/D-0002-thorough.md", "id: D-0002\nstatus: active\n",
		"# D-0002 — Ordering\n\nRetries break idempotency. "+strings.Repeat("Context about the ledger and its owners. ", 40))

	// Act
	hits, _ := brain.Query(reload(t, root), []string{"retries", "idempotency"}, brain.QueryOptions{})

	// Assert
	if len(hits) < 2 || hits[0].ID != "D-0002" {
		t.Errorf("a short record answering one term outranked a long one answering both: %+v", hits)
	}
}
