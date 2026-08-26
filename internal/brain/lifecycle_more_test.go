package brain

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The value of this layer is entirely in what it refuses: a record nobody
// reviewed is not a fact, an observation nobody re-checked is not current, and a
// decision that was replaced is not deleted. These drive those refusals.

var (
	observedOn = time.Date(2026, 5, 2, 0, 0, 0, 0, time.UTC)
	longAfter  = time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
)

func newStore(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if _, err := Init(root, observedOn); err != nil {
		t.Fatalf("init: %v", err)
	}
	return root
}

func mustCreate(t *testing.T, root string, input NewRecordInput) string {
	t.Helper()
	if input.Now.IsZero() {
		input.Now = observedOn
	}
	path, err := Create(root, input)
	if err != nil {
		t.Fatalf("create %s: %v", input.ID, err)
	}
	return path
}

func mustLoad(t *testing.T, root string) *Store {
	t.Helper()
	store, err := Load(root)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	return store
}

func recordByID(t *testing.T, store *Store, id string) Record {
	t.Helper()
	for _, record := range store.Records {
		if record.ID == id {
			return record
		}
	}
	t.Fatalf("no record %s in the store", id)
	return Record{}
}

func defaultPolicy() CheckPolicy {
	return CheckPolicy{StaleAfterDays: 90, ReviewSLADays: 30}
}

func TestParseKindAcceptsEveryKindItAdvertisesAndNothingElse(t *testing.T) {
	// Arrange: the parser turns a command-line word into a record type, so a
	// kind it cannot parse is a kind nobody can create.
	for _, kind := range Kinds() {
		t.Run(string(kind), func(t *testing.T) {
			// Act
			parsed, err := ParseKind(string(kind))

			// Assert
			if err != nil {
				t.Fatalf("%q is advertised as a kind but does not parse: %v", kind, err)
			}
			if parsed != kind {
				t.Errorf("parsing %q produced %q", kind, parsed)
			}
		})
	}

	// Act
	_, err := ParseKind("not-a-kind")

	// Assert
	if err == nil {
		t.Error("an unknown kind parsed without complaint")
	}
}

func TestParseSourceRefSplitsARevisionPinnedReference(t *testing.T) {
	// Arrange: the reference is the whole provenance claim. Reading it wrongly
	// attributes a fact to the wrong repository or the wrong revision.
	cases := []struct {
		ref      string
		repo     string
		revision string
		file     string
		ok       bool
	}{
		{"payments@3f9a1c2e:docs/ORDERING.md", "payments", "3f9a1c2e", "docs/ORDERING.md", true},
		{"payments@3f9a1c2e", "payments", "3f9a1c2e", "", true},
		{"payments", "", "", "", false},
		{"", "", "", "", false},
	}

	for _, testCase := range cases {
		t.Run(testCase.ref, func(t *testing.T) {
			// Act
			repo, revision, file, ok := ParseSourceRef(testCase.ref)

			// Assert
			if ok != testCase.ok {
				t.Fatalf("parsing %q reported ok=%v, want %v", testCase.ref, ok, testCase.ok)
			}
			if !ok {
				return
			}
			if repo != testCase.repo || revision != testCase.revision || file != testCase.file {
				t.Errorf("parsing %q gave (%q, %q, %q), want (%q, %q, %q)",
					testCase.ref, repo, revision, file,
					testCase.repo, testCase.revision, testCase.file)
			}
		})
	}
}

func TestAProvisionalRecordIsNotAnswerableAndAPromotedOneIs(t *testing.T) {
	// Arrange: this is the promotion gate. Without it the layer is a wiki with
	// extra steps.
	root := newStore(t)
	mustCreate(t, root, NewRecordInput{
		Kind: KindDecision, ID: "D-0001", Title: "Pricing is per seat",
		Status: StatusProvisional,
	})

	// Act
	before := recordByID(t, mustLoad(t, root), "D-0001")
	if err := Promote(root, before, "reviewer", observedOn); err != nil {
		t.Fatalf("promote: %v", err)
	}
	after := recordByID(t, mustLoad(t, root), "D-0001")

	// Assert
	if before.Status.Answerable() {
		t.Error("a record nobody reviewed already counted as an answer")
	}
	if !after.Status.Answerable() {
		t.Errorf("a promoted record is still not citable: status %q", after.Status)
	}
}

func TestSweepDemotesAnObservationPastItsWindowWithoutDeletingIt(t *testing.T) {
	// Arrange: demotion is not deletion. The reasoning survives; only its
	// standing as a current fact does not.
	root := newStore(t)
	path := mustCreate(t, root, NewRecordInput{
		Kind: KindGap, ID: "G-0001", Title: "Ordering is not retry-safe",
		Status: StatusActive, ClaimKind: ClaimCurrentState,
		OwnedBy: "payments", SourceRef: "payments@3f9a1c2e",
	})

	// Act
	reported, err := Sweep(mustLoad(t, root), defaultPolicy(), longAfter, false)
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	applied, err := Sweep(mustLoad(t, root), defaultPolicy(), longAfter, true)
	if err != nil {
		t.Fatalf("sweep --apply: %v", err)
	}

	// Assert
	if len(reported) == 0 {
		t.Fatal("an observation seven months past a 90-day window was not reported")
	}
	if reported[0].Applied {
		t.Error("a sweep without apply reported the transition as written")
	}
	if len(applied) == 0 || !applied[0].Applied {
		t.Fatalf("a sweep with apply did not write the transition: %+v", applied)
	}
	// Create returns the path relative to the brain root.
	if _, err := os.Stat(filepath.Join(root, path)); err != nil {
		t.Fatalf("the record file was removed: %v", err)
	}
	after := recordByID(t, mustLoad(t, root), "G-0001")
	if after.Status != StatusStale {
		t.Errorf("the aged claim is %q, want %q", after.Status, StatusStale)
	}
	if after.Status.Answerable() {
		t.Error("a stale claim is still citable")
	}
	if after.Title == "" {
		t.Error("demotion destroyed the record's own content")
	}
}

func TestSweepLeavesAClaimInsideItsWindowAlone(t *testing.T) {
	// Arrange: a sweep that demoted everything would make the queue meaningless.
	root := newStore(t)
	mustCreate(t, root, NewRecordInput{
		Kind: KindGap, ID: "G-0001", Title: "Recently verified",
		Status: StatusActive, ClaimKind: ClaimCurrentState,
		OwnedBy: "payments", SourceRef: "payments@3f9a1c2e",
	})

	// Act
	transitions, err := Sweep(mustLoad(t, root), defaultPolicy(), observedOn.AddDate(0, 0, 10), true)

	// Assert
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if len(transitions) != 0 {
		t.Errorf("a claim ten days old was demoted under a 90-day window: %+v", transitions)
	}
}

func TestSupersedeLinksBothRecordsAndCheckAcceptsTheChain(t *testing.T) {
	// Arrange: a one-way link leaves the replaced record reading as current when
	// somebody opens it directly.
	root := newStore(t)
	mustCreate(t, root, NewRecordInput{
		Kind: KindDecision, ID: "D-0001", Title: "Pricing is per seat", Status: StatusActive,
	})
	mustCreate(t, root, NewRecordInput{
		Kind: KindDecision, ID: "D-0002", Title: "Pricing is per workspace", Status: StatusActive,
	})
	store := mustLoad(t, root)

	// Act
	if err := Supersede(root, recordByID(t, store, "D-0001"), recordByID(t, store, "D-0002")); err != nil {
		t.Fatalf("supersede: %v", err)
	}
	updated := mustLoad(t, root)
	older := recordByID(t, updated, "D-0001")
	newer := recordByID(t, updated, "D-0002")

	// Assert
	if older.Status != StatusSuperseded {
		t.Errorf("the replaced record is %q, want %q", older.Status, StatusSuperseded)
	}
	if older.SupersededBy != "D-0002" {
		t.Errorf("the replaced record points at %q", older.SupersededBy)
	}
	found := false
	for _, id := range newer.Supersedes {
		if id == "D-0001" {
			found = true
		}
	}
	if !found {
		t.Errorf("the replacement does not record what it replaced: %v", newer.Supersedes)
	}
	if problems := Errors(Check(updated, defaultPolicy(), observedOn)); problems > 0 {
		t.Errorf("check found %d errors in a chain supersede had just written", problems)
	}
}

func TestCheckReportsARecordReferringToOneThatDoesNotExist(t *testing.T) {
	// Arrange: a dangling reference reads as supporting evidence right up until
	// somebody tries to open it.
	root := newStore(t)
	mustCreate(t, root, NewRecordInput{
		Kind: KindGoal, ID: "GO-0001", Title: "Ship v2", Status: StatusActive,
		Refs: []string{"D-9999"},
	})

	// Act
	findings := Check(mustLoad(t, root), defaultPolicy(), observedOn)

	// Assert
	mentioned := false
	for _, finding := range findings {
		if strings.Contains(finding.Message, "D-9999") || finding.ID == "D-9999" {
			mentioned = true
		}
	}
	if !mentioned {
		t.Errorf("a reference to a record that does not exist was not reported: %+v", findings)
	}
}

func TestCheckRejectsASourceReferenceThatIsABranch(t *testing.T) {
	// Arrange: a branch keeps moving and takes the evidence with it, so the
	// claim would silently become evidence for whatever landed last.
	root := newStore(t)
	mustCreate(t, root, NewRecordInput{
		Kind: KindGap, ID: "G-0001", Title: "Ordering is not retry-safe",
		Status: StatusActive, ClaimKind: ClaimCurrentState,
		OwnedBy: "payments", SourceRef: "payments@main",
	})

	// Act
	findings := Check(mustLoad(t, root), defaultPolicy(), observedOn)

	// Assert
	if len(findings) == 0 {
		t.Error("a claim pinned to a branch name produced no finding at all")
	}
	mentioned := false
	for _, finding := range findings {
		if strings.Contains(finding.Message, "branch") {
			mentioned = true
		}
	}
	if !mentioned {
		t.Errorf("nothing reported the branch-shaped source reference: %+v", findings)
	}
}

func TestReviewQueueSaysWhatIsWaitingAndWhy(t *testing.T) {
	// Arrange: a flat queue grows until it is ignored wholesale. Each entry has
	// to carry its own reason or the list cannot be worked through.
	root := newStore(t)
	mustCreate(t, root, NewRecordInput{
		Kind: KindGap, ID: "G-0001", Title: "Observation aged out",
		Status: StatusStale, ClaimKind: ClaimCurrentState,
		OwnedBy: "payments", SourceRef: "payments@3f9a1c2e",
	})
	mustCreate(t, root, NewRecordInput{
		Kind: KindDecision, ID: "D-0001", Title: "Suspected wrong",
		Status: StatusQuarantined,
	})

	// Act
	queue := ReviewQueue(mustLoad(t, root), defaultPolicy(), longAfter)

	// Assert
	if len(queue) < 2 {
		t.Fatalf("the queue holds %d items, want both records awaiting review", len(queue))
	}
	for _, item := range queue {
		if item.ID == "" {
			t.Errorf("a queue entry does not say what it is: %+v", item)
		}
	}
}

func TestBuildProducesEveryGeneratedFileAndCheckStillPasses(t *testing.T) {
	// Arrange: summaries are projections. Regenerating them must never be able
	// to invalidate the records they were derived from.
	root := newStore(t)
	mustCreate(t, root, NewRecordInput{
		Kind: KindDecision, ID: "D-0001", Title: "Pricing is per seat", Status: StatusActive,
	})
	mustCreate(t, root, NewRecordInput{
		Kind: KindGoal, ID: "GO-0001", Title: "Ship v2", Status: StatusActive,
	})

	// Act
	if _, err := Build(mustLoad(t, root), observedOn); err != nil {
		t.Fatalf("build: %v", err)
	}

	// Assert
	for _, name := range Generated() {
		if _, err := os.Stat(filepath.Join(root, name)); err != nil {
			t.Errorf("build did not produce the generated file %s: %v", name, err)
		}
	}
	if problems := Errors(Check(mustLoad(t, root), defaultPolicy(), observedOn)); problems > 0 {
		t.Errorf("check reports %d errors on a repository build had just produced", problems)
	}
}

func TestBuildIsIdempotent(t *testing.T) {
	// Arrange: a projection that differs between two runs over identical records
	// shows as drift in git and trains everyone to ignore the diff.
	root := newStore(t)
	mustCreate(t, root, NewRecordInput{
		Kind: KindDecision, ID: "D-0001", Title: "Pricing is per seat", Status: StatusActive,
	})
	if _, err := Build(mustLoad(t, root), observedOn); err != nil {
		t.Fatalf("build: %v", err)
	}
	first := readGenerated(t, root)

	// Act
	if _, err := Build(mustLoad(t, root), observedOn); err != nil {
		t.Fatalf("build again: %v", err)
	}
	second := readGenerated(t, root)

	// Assert
	if len(first) == 0 {
		t.Fatal("no generated files to compare")
	}
	for name, body := range first {
		if second[name] != body {
			t.Errorf("%s differs between two builds over identical records", name)
		}
	}
}

func readGenerated(t *testing.T, root string) map[string]string {
	t.Helper()
	files := map[string]string{}
	for _, name := range Generated() {
		content, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			continue
		}
		files[name] = string(content)
	}
	return files
}

func TestQueryLeavesSupersededReasoningOutOfTheDefaultSurface(t *testing.T) {
	// Arrange: an answer assembled from reasoning that was already replaced is
	// worse than no answer, because it reads as current.
	root := newStore(t)
	mustCreate(t, root, NewRecordInput{
		Kind: KindDecision, ID: "D-0001", Title: "Pricing is per seat",
		Status: StatusSuperseded, Body: "pricing per seat",
	})
	mustCreate(t, root, NewRecordInput{
		Kind: KindDecision, ID: "D-0002", Title: "Pricing is per workspace",
		Status: StatusActive, Body: "pricing per workspace",
	})
	store := mustLoad(t, root)

	// Act
	narrow := Query(store, []string{"pricing"}, QueryOptions{Limit: 10})
	wide := Query(store, []string{"pricing"}, QueryOptions{Limit: 10, IncludeTerminal: true})

	// Assert
	for _, hit := range narrow {
		if hit.ID == "D-0001" {
			t.Error("a superseded decision turned up in the default search surface")
		}
	}
	if len(wide) <= len(narrow) {
		t.Errorf("including terminal records did not widen the surface: %d vs %d", len(wide), len(narrow))
	}
}

func TestSlugAndFileNameProduceAStablePathForATitle(t *testing.T) {
	// Arrange: the filename is how a record is found by eye in a directory
	// listing, and it must not change when the title is re-rendered.
	cases := []struct{ title, want string }{
		{"Retries are not idempotent", "retries-are-not-idempotent"},
		{"  Spaces   collapse  ", "spaces-collapse"},
		{"Punctuation: removed!", "punctuation-removed"},
	}

	for _, testCase := range cases {
		t.Run(testCase.title, func(t *testing.T) {
			// Act
			slug := Slug(testCase.title)

			// Assert
			if slug != testCase.want {
				t.Errorf("Slug(%q) = %q, want %q", testCase.title, slug, testCase.want)
			}
			if name := FileName("D-0001", testCase.title); !strings.HasPrefix(name, "D-0001-") {
				t.Errorf("FileName produced %q, which does not lead with the id", name)
			}
		})
	}
}

func TestIsBrainRecognisesAnInitialisedDirectoryOnly(t *testing.T) {
	// Arrange: the commands refuse to write records into a directory that is not
	// a brain repository, so the test for it must not be a guess.
	initialised := newStore(t)
	bare := t.TempDir()

	// Act & Assert
	if !IsBrain(initialised) {
		t.Error("a directory Init just produced is not recognised as a brain repository")
	}
	if IsBrain(bare) {
		t.Error("an empty directory is treated as a brain repository")
	}
}

func TestAnIdentifierThatCouldBecomeAPathIsRefused(t *testing.T) {
	// Arrange: ids are normally generated, but `--id` lets a caller supply one
	// and it is pasted straight into a filename. The check lives here rather
	// than at the command so no caller can write a record to a path of its own
	// choosing.
	refused := []string{"", "   ", "../../../pwned", "nested/id", ".hidden", strings.Repeat("x", 65)}
	accepted := []string{"D-0001", "GO-0014", "some_id", "some.id", "a"}

	// Act & Assert
	for _, id := range refused {
		if err := ValidateID(id); err == nil {
			t.Errorf("ValidateID(%q) accepted an id that becomes a path", id)
		}
	}
	for _, id := range accepted {
		if err := ValidateID(id); err != nil {
			t.Errorf("ValidateID(%q) refused an ordinary identifier: %v", id, err)
		}
	}
}

func TestCreateRefusesAnIdentifierThatCouldBecomeAPath(t *testing.T) {
	// Arrange: the package, not the command, is where this has to be enforced.
	root := newStore(t)

	// Act
	_, err := Create(root, NewRecordInput{
		Kind: KindDecision, ID: "../../../pwned", Title: "x", Now: observedOn,
	})

	// Assert
	if err == nil {
		t.Error("Create wrote a record to a caller-chosen path")
	}
}

// The sweep is the first lifecycle command most records ever meet, and it runs
// unattended. If it rewrites a record through the typed schema, a workspace
// that extended the header loses that extension on the day its claim ages out —
// a data loss with no error, no prompt, and nothing in the diff anyone reads.
func TestSweepPreservesHeaderFieldsTheSchemaDoesNotModel(t *testing.T) {
	// Arrange
	root := newStore(t)
	relative := mustCreate(t, root, NewRecordInput{
		Kind: KindGap, ID: "G-0014", Title: "Retries double-submit",
		Status: StatusActive, ClaimKind: ClaimCurrentState,
		OwnedBy: "payments", SourceRef: "payments@3f9a1c2e8b74",
	})
	path := filepath.Join(root, filepath.FromSlash(relative))
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	extended := strings.Replace(string(original), "status: active",
		"status: active\n# graded by the payments team, not by vat\nconfidence: high", 1)
	if err := os.WriteFile(path, []byte(extended), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Act
	if _, err := Sweep(mustLoad(t, root), CheckPolicy{StaleAfterDays: 90}, longAfter, true); err != nil {
		t.Fatalf("sweep: %v", err)
	}

	// Assert
	swept, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(swept), "status: stale") {
		t.Fatalf("the claim was not demoted:\n%s", swept)
	}
	for _, kept := range []string{"confidence: high", "# graded by the payments team, not by vat"} {
		if !strings.Contains(string(swept), kept) {
			t.Errorf("sweep silently dropped %q:\n%s", kept, swept)
		}
	}
}

// One record with a git merge conflict marker in its header used to take down
// check, query, sweep, build, doctor, and lint together — the layer reporting
// nothing at all about the records that were fine. "Report every finding at
// once" has to hold hardest exactly when something is broken.
func TestOneUnparseableRecordDoesNotHideEveryOtherRecord(t *testing.T) {
	// Arrange
	root := newStore(t)
	mustCreate(t, root, NewRecordInput{Kind: KindGoal, ID: "O-0001", Title: "Ship weekly"})
	broken := filepath.Join(root, "decisions", "D-0001-broken.md")
	if err := os.WriteFile(broken,
		[]byte("---\nid: D-0001\n<<<<<<< HEAD\nstatus: active\n=======\nstatus: provisional\n---\n\n# D-0001\n"),
		0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Act
	store, err := Load(root)
	if err != nil {
		t.Fatalf("load refused to return anything: %v", err)
	}

	// Assert
	if len(store.Records) != 1 || store.Records[0].ID != "O-0001" {
		t.Errorf("the sound records were not loaded: %+v", store.Records)
	}
	if len(store.Malformed) != 1 {
		t.Fatalf("the broken record was not reported: %+v", store.Malformed)
	}
	if store.Malformed[0].Path != "decisions/D-0001-broken.md" {
		t.Errorf("malformed path = %q", store.Malformed[0].Path)
	}

	findings := Check(store, CheckPolicy{StaleAfterDays: 90}, longAfter)
	var reported bool
	for _, finding := range findings {
		if finding.Rule == "brain/record-malformed" {
			reported = true
			if finding.Severity != SeverityError {
				t.Errorf("severity = %q, want error", finding.Severity)
			}
		}
	}
	if !reported {
		t.Errorf("check did not report the malformed record: %+v", findings)
	}
}
