package brain_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/takealook97/vat/internal/brain"
)

var reference = time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)

// writeRecord puts a record on disk with the given front matter and body.
func writeRecord(t *testing.T, root, relative, frontMatter, body string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create directory: %v", err)
	}
	content := "---\n" + strings.TrimSpace(frontMatter) + "\n---\n\n" + body + "\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", relative, err)
	}
}

func newStore(t *testing.T) (string, *brain.Store) {
	t.Helper()
	root := t.TempDir()
	if _, err := brain.Init(root, reference); err != nil {
		t.Fatalf("Init returned an error: %v", err)
	}
	store, err := brain.Load(root)
	if err != nil {
		t.Fatalf("Load returned an error: %v", err)
	}
	return root, store
}

func reload(t *testing.T, root string) *brain.Store {
	t.Helper()
	store, err := brain.Load(root)
	if err != nil {
		t.Fatalf("Load returned an error: %v", err)
	}
	return store
}

func findingRules(findings []brain.Finding) map[string]string {
	rules := map[string]string{}
	for _, finding := range findings {
		rules[finding.Rule] = finding.Message
	}
	return rules
}

func TestInitCreatesTheLayoutAndIsSafeToRerun(t *testing.T) {
	// Arrange
	root := t.TempDir()

	// Act
	first, err := brain.Init(root, reference)
	if err != nil {
		t.Fatalf("first Init returned an error: %v", err)
	}
	second, err := brain.Init(root, reference)

	// Assert
	if err != nil {
		t.Fatalf("second Init returned an error: %v", err)
	}
	if len(first) == 0 {
		t.Error("first Init created nothing")
	}
	if len(second) != 0 {
		t.Errorf("second Init recreated %v; it must never overwrite", second)
	}
	if !brain.IsBrain(root) {
		t.Error("IsBrain is false after Init")
	}
}

func TestARecordWithoutAStatusEntersAsProvisionalRatherThanTruth(t *testing.T) {
	// Arrange
	root, _ := newStore(t)
	writeRecord(t, root, "decisions/D-0001-x.md", "id: D-0001", "# D-0001 — Something")

	// Act
	store := reload(t, root)

	// Assert
	record := store.ByID()["D-0001"]
	if record.Status != brain.StatusProvisional {
		t.Errorf("status = %q, want provisional; unreviewed records must not be citable",
			record.Status)
	}
	if record.Status.Answerable() {
		t.Error("a provisional record reports itself as answerable")
	}
}

func TestCheckRequiresProvenanceOnAClaimAboutThePresent(t *testing.T) {
	// Arrange
	root, _ := newStore(t)
	writeRecord(t, root, "gaps/G-0001-x.md", `
id: G-0001
status: active
claim_kind: current-state
`, "# G-0001 — Something is broken")

	// Act
	findings := brain.Check(reload(t, root), brain.CheckPolicy{StaleAfterDays: 90}, reference)

	// Assert
	rules := findingRules(findings)
	for _, want := range []string{"brain/claim-owner", "brain/claim-source", "brain/claim-observed"} {
		if _, found := rules[want]; !found {
			t.Errorf("missing rule %s; got %v", want, rules)
		}
	}
}

func TestCheckWarnsWhenEvidencePointsAtABranchInsteadOfARevision(t *testing.T) {
	// Arrange: a branch keeps moving, so the claim silently changes what it was
	// evidence for.
	root, _ := newStore(t)
	writeRecord(t, root, "gaps/G-0001-x.md", `
id: G-0001
status: active
claim_kind: current-state
owned_by: payments
source_ref: payments@main:docs/STATUS.md
observed_at: "2026-08-01"
`, "# G-0001 — Something")

	// Act
	findings := brain.Check(reload(t, root), brain.CheckPolicy{StaleAfterDays: 90}, reference)

	// Assert
	if _, found := findingRules(findings)["brain/claim-source-branch"]; !found {
		t.Errorf("a branch-pinned source_ref was accepted: %v", findingRules(findings))
	}
}

func TestCheckRequiresSupersessionToLinkBothWays(t *testing.T) {
	// Arrange: only the old record points forward.
	root, _ := newStore(t)
	writeRecord(t, root, "decisions/D-0001-x.md", `
id: D-0001
status: superseded
superseded_by: D-0002
`, "# D-0001 — Old")
	writeRecord(t, root, "decisions/D-0002-y.md", "id: D-0002\nstatus: active", "# D-0002 — New")

	// Act
	findings := brain.Check(reload(t, root), brain.CheckPolicy{}, reference)

	// Assert
	if _, found := findingRules(findings)["brain/superseded-asymmetric"]; !found {
		t.Errorf("a one-way supersession link was accepted: %v", findingRules(findings))
	}
}

func TestCheckDetectsASupersessionCycle(t *testing.T) {
	// Arrange
	root, _ := newStore(t)
	writeRecord(t, root, "decisions/D-0001-x.md", `
id: D-0001
status: superseded
superseded_by: D-0002
supersedes: [D-0002]
`, "# D-0001 — A")
	writeRecord(t, root, "decisions/D-0002-y.md", `
id: D-0002
status: superseded
superseded_by: D-0001
supersedes: [D-0001]
`, "# D-0002 — B")

	// Act
	findings := brain.Check(reload(t, root), brain.CheckPolicy{}, reference)

	// Assert
	if _, found := findingRules(findings)["brain/supersede-cycle"]; !found {
		t.Errorf("a supersession cycle went undetected: %v", findingRules(findings))
	}
}

func TestCheckRequiresAReasonForAWithdrawal(t *testing.T) {
	// Arrange
	root, _ := newStore(t)
	writeRecord(t, root, "decisions/D-0001-x.md", "id: D-0001\nstatus: revoked", "# D-0001 — Gone")
	writeRecord(t, root, "decisions/D-0002-y.md", "id: D-0002\nstatus: quarantined", "# D-0002 — Doubted")

	// Act
	findings := brain.Check(reload(t, root), brain.CheckPolicy{}, reference)

	// Assert
	rules := findingRules(findings)
	for _, want := range []string{"brain/revoke-reason", "brain/quarantine-reason"} {
		if _, found := rules[want]; !found {
			t.Errorf("missing rule %s; a withdrawal with no cause cannot be reviewed later", want)
		}
	}
}

func TestCheckReportsABrokenInternalLink(t *testing.T) {
	// Arrange
	root, _ := newStore(t)
	writeRecord(t, root, "decisions/D-0001-x.md", "id: D-0001\nstatus: active",
		"# D-0001 — Something\n\nSee [the gap](../gaps/G-9999-missing.md).")

	// Act
	findings := brain.Check(reload(t, root), brain.CheckPolicy{}, reference)

	// Assert
	if _, found := findingRules(findings)["brain/link-broken"]; !found {
		t.Errorf("a broken link was accepted: %v", findingRules(findings))
	}
}

func TestSweepDemotesAnAgedClaimOnlyWhenApplied(t *testing.T) {
	// Arrange
	root, _ := newStore(t)
	writeRecord(t, root, "gaps/G-0001-x.md", `
id: G-0001
status: active
claim_kind: current-state
owned_by: payments
source_ref: payments@0123456789abcdef
observed_at: "2026-01-01"
`, "# G-0001 — Something")
	policy := brain.CheckPolicy{StaleAfterDays: 90}

	// Act
	planned, err := brain.Sweep(reload(t, root), policy, reference, false)
	if err != nil {
		t.Fatalf("dry Sweep returned an error: %v", err)
	}
	applied, err := brain.Sweep(reload(t, root), policy, reference, true)

	// Assert
	if err != nil {
		t.Fatalf("applied Sweep returned an error: %v", err)
	}
	if len(planned) != 1 || planned[0].Applied {
		t.Fatalf("dry run should plan one unapplied transition, got %+v", planned)
	}
	if len(applied) != 1 || !applied[0].Applied {
		t.Fatalf("apply should record one applied transition, got %+v", applied)
	}
	after := reload(t, root).ByID()["G-0001"]
	if after.Status != brain.StatusStale {
		t.Errorf("status after sweep = %q, want stale", after.Status)
	}
	if after.Reason == "" {
		t.Error("the demotion recorded no reason")
	}
}

func TestSweepLeavesAHistoricalClaimAloneNoMatterHowOld(t *testing.T) {
	// Arrange: a statement about the past does not decay.
	root, _ := newStore(t)
	writeRecord(t, root, "decisions/D-0001-x.md", `
id: D-0001
status: active
claim_kind: historical
observed_at: "2019-01-01"
`, "# D-0001 — What we did")

	// Act
	transitions, err := brain.Sweep(reload(t, root), brain.CheckPolicy{StaleAfterDays: 30}, reference, true)

	// Assert
	if err != nil {
		t.Fatalf("Sweep returned an error: %v", err)
	}
	if len(transitions) != 0 {
		t.Errorf("a historical claim was demoted: %+v", transitions)
	}
}

func TestPromoteRefusesAClaimWithNoProvenance(t *testing.T) {
	// Arrange
	root, _ := newStore(t)
	writeRecord(t, root, "gaps/G-0001-x.md", `
id: G-0001
status: provisional
claim_kind: current-state
`, "# G-0001 — Something")
	store := reload(t, root)

	// Act
	err := brain.Promote(root, store.ByID()["G-0001"], brain.PromoteRequest{Reviewer: "alex", Now: reference})

	// Assert
	if err == nil {
		t.Fatal("Promote accepted a claim with no source; the gate would be decorative")
	}
	if !strings.Contains(err.Error(), "source_ref") {
		t.Errorf("error should name the missing field, got %v", err)
	}
}

func TestPromoteStampsTheObservationDateAndReviewer(t *testing.T) {
	// Arrange
	root, _ := newStore(t)
	writeRecord(t, root, "decisions/D-0001-x.md", "id: D-0001\nstatus: provisional", "# D-0001 — Something")
	store := reload(t, root)

	// Act
	if err := brain.Promote(root, store.ByID()["D-0001"], brain.PromoteRequest{Reviewer: "alex", Now: reference}); err != nil {
		t.Fatalf("Promote returned an error: %v", err)
	}

	// Assert
	promoted := reload(t, root).ByID()["D-0001"]
	if promoted.Status != brain.StatusActive {
		t.Errorf("status = %q, want active", promoted.Status)
	}
	if promoted.ObservedAt != "2026-08-25" {
		t.Errorf("observed_at = %q, want 2026-08-25", promoted.ObservedAt)
	}
	if promoted.ReviewedBy != "alex" {
		t.Errorf("reviewed_by = %q, want alex", promoted.ReviewedBy)
	}
}

func TestSupersedeUpdatesBothEndsAndPreservesTheOriginalBody(t *testing.T) {
	// Arrange
	root, _ := newStore(t)
	writeRecord(t, root, "decisions/D-0001-x.md", "id: D-0001\nstatus: active",
		"# D-0001 — Old\n\nThe reasoning at the time.")
	writeRecord(t, root, "decisions/D-0002-y.md", "id: D-0002\nstatus: provisional", "# D-0002 — New")
	store := reload(t, root)
	index := store.ByID()

	// Act
	if err := brain.Supersede(root, index["D-0001"], index["D-0002"], brain.SupersedeOptions{}); err != nil {
		t.Fatalf("Supersede returned an error: %v", err)
	}

	// Assert
	after := reload(t, root).ByID()
	if after["D-0001"].Status != brain.StatusSuperseded {
		t.Errorf("old status = %q, want superseded", after["D-0001"].Status)
	}
	if after["D-0001"].SupersededBy != "D-0002" {
		t.Errorf("superseded_by = %q, want D-0002", after["D-0001"].SupersededBy)
	}
	if len(after["D-0002"].Supersedes) != 1 || after["D-0002"].Supersedes[0] != "D-0001" {
		t.Errorf("supersedes = %v, want [D-0001]", after["D-0002"].Supersedes)
	}
	if !strings.Contains(after["D-0001"].Body, "The reasoning at the time") {
		t.Error("the original reasoning was destroyed; supersession must never rewrite it")
	}
	if findings := brain.Check(reload(t, root), brain.CheckPolicy{}, reference); len(findings) != 0 {
		t.Errorf("Supersede left the chain invalid: %v", findingRules(findings))
	}
}

func TestReviewQueueRanksAWidelyCitedClaimAboveAnIgnoredOne(t *testing.T) {
	// Arrange: same status and age, different number of dependants.
	root, _ := newStore(t)
	writeRecord(t, root, "gaps/G-0001-ignored.md", `
id: G-0001
status: stale
observed_at: "2026-08-01"
`, "# G-0001 — Nobody cites this")
	writeRecord(t, root, "gaps/G-0002-cited.md", `
id: G-0002
status: stale
observed_at: "2026-08-01"
`, "# G-0002 — Everything rests on this")
	for _, id := range []string{"D-0001", "D-0002", "D-0003"} {
		writeRecord(t, root, "decisions/"+id+"-x.md",
			"id: "+id+"\nstatus: active\nrefs: [G-0002]", "# "+id+" — Depends on G-0002")
	}

	// Act
	queue := brain.ReviewQueue(reload(t, root), brain.CheckPolicy{ReviewSLADays: 30}, reference)

	// Assert
	if len(queue) == 0 {
		t.Fatal("the review queue is empty")
	}
	if queue[0].ID != "G-0002" {
		t.Errorf("highest priority = %s, want G-0002; a claim everything cites cannot wait",
			queue[0].ID)
	}
}

func TestBuildIsDeterministicAndDriftIsDetected(t *testing.T) {
	// Arrange
	root, _ := newStore(t)
	writeRecord(t, root, "decisions/D-0001-x.md", "id: D-0001\nstatus: active", "# D-0001 — Something")

	// Act
	first, err := brain.Build(reload(t, root), reference)
	if err != nil {
		t.Fatalf("first Build returned an error: %v", err)
	}
	second, err := brain.Build(reload(t, root), reference)
	if err != nil {
		t.Fatalf("second Build returned an error: %v", err)
	}
	drift, err := brain.Drift(reload(t, root), reference)
	if err != nil {
		t.Fatalf("Drift returned an error: %v", err)
	}

	// Assert
	if len(first.Changed) == 0 {
		t.Error("the first build wrote nothing")
	}
	if len(second.Changed) != 0 {
		t.Errorf("build is not deterministic; it rewrote %v", second.Changed)
	}
	if len(drift) != 0 {
		t.Errorf("Drift reported %v immediately after a build", drift)
	}
}

func TestDriftIsReportedWhenAGeneratedFileIsEditedByHand(t *testing.T) {
	// Arrange
	root, _ := newStore(t)
	writeRecord(t, root, "decisions/D-0001-x.md", "id: D-0001\nstatus: active", "# D-0001 — Something")
	if _, err := brain.Build(reload(t, root), reference); err != nil {
		t.Fatalf("Build returned an error: %v", err)
	}
	path := filepath.Join(root, brain.CurrentFile)
	if err := os.WriteFile(path, []byte("# hand edited\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Act
	drift, err := brain.Drift(reload(t, root), reference)

	// Assert
	if err != nil {
		t.Fatalf("Drift returned an error: %v", err)
	}
	if len(drift) == 0 {
		t.Error("a hand-edited generated file was not reported as drift")
	}
}

func TestQueryPrefersARecordMatchingEveryTerm(t *testing.T) {
	// Arrange
	root, _ := newStore(t)
	writeRecord(t, root, "decisions/D-0001-both.md", "id: D-0001\nstatus: active",
		"# D-0001 — Retries and idempotency")
	writeRecord(t, root, "decisions/D-0002-one.md", "id: D-0002\nstatus: active",
		"# D-0002 — Retries retries retries retries")

	// Act
	hits := brain.Query(reload(t, root), []string{"retries", "idempotency"}, brain.QueryOptions{})

	// Assert
	if len(hits) == 0 {
		t.Fatal("Query found nothing")
	}
	if hits[0].ID != "D-0001" {
		t.Errorf("top hit = %s, want D-0001; matching every term beats repeating one",
			hits[0].ID)
	}
}

func TestQueryExcludesSupersededRecordsUnlessAsked(t *testing.T) {
	// Arrange
	root, _ := newStore(t)
	writeRecord(t, root, "decisions/D-0001-old.md", `
id: D-0001
status: superseded
superseded_by: D-0002
`, "# D-0001 — Pricing is per seat")
	writeRecord(t, root, "decisions/D-0002-new.md", `
id: D-0002
status: active
supersedes: [D-0001]
`, "# D-0002 — Something else entirely")

	// Act
	narrow := brain.Query(reload(t, root), []string{"pricing"}, brain.QueryOptions{})
	wide := brain.Query(reload(t, root), []string{"pricing"}, brain.QueryOptions{IncludeTerminal: true})

	// Assert
	if len(narrow) != 0 {
		t.Errorf("superseded reasoning surfaced in a default query: %+v", narrow)
	}
	if len(wide) == 0 {
		t.Error("--all did not reach the superseded record")
	}
}

func TestNextIDContinuesFromTheHighestExistingIdentifier(t *testing.T) {
	// Arrange
	root, _ := newStore(t)
	writeRecord(t, root, "decisions/D-0009-x.md", "id: D-0009\nstatus: active", "# D-0009 — Nine")
	writeRecord(t, root, "decisions/D-0010-y.md", "id: D-0010\nstatus: active", "# D-0010 — Ten")

	// Act
	next := reload(t, root).NextID(brain.KindDecision)

	// Assert
	if next != "D-0011" {
		t.Errorf("NextID = %q, want D-0011", next)
	}
}

func TestParseSourceRefSplitsRepositoryRevisionAndPath(t *testing.T) {
	// Arrange
	ref := "payments@0123456789abcdef:docs/STATUS.md"

	// Act
	repo, revision, path, ok := brain.ParseSourceRef(ref)

	// Assert
	if !ok {
		t.Fatal("ParseSourceRef rejected a well-formed reference")
	}
	if repo != "payments" || revision != "0123456789abcdef" || path != "docs/STATUS.md" {
		t.Errorf("got (%q, %q, %q)", repo, revision, path)
	}
}

func TestCreateWritesAProvisionalRecordWithRevalidationMetadata(t *testing.T) {
	// Arrange
	root, _ := newStore(t)

	// Act
	relative, err := brain.Create(root, brain.NewRecordInput{
		Kind: brain.KindGap, ID: "G-0001", Title: "Retries double-submit",
		ClaimKind: brain.ClaimCurrentState, OwnedBy: "payments",
		SourceRef: "payments@0123456789abcdef", Now: reference,
	})

	// Assert
	if err != nil {
		t.Fatalf("Create returned an error: %v", err)
	}
	if !strings.HasPrefix(relative, "gaps/G-0001") {
		t.Errorf("path = %q, want gaps/G-0001...", relative)
	}
	record := reload(t, root).ByID()["G-0001"]
	if record.Status != brain.StatusProvisional {
		t.Errorf("status = %q, want provisional", record.Status)
	}
	if record.RevalidateOn == "" || record.ObservedAt == "" {
		t.Errorf("a current-state claim was created without revalidation metadata: %+v", record.Metadata)
	}
}

// CURRENT.md is documented as a fixed-size entry point and was nothing of the
// kind: it grew one row per record, forever. An entry point that has to be read
// in full to be used is the summary file this whole layer was built to replace
// — and the failure arrives late, when the repository is finally big enough to
// be worth having.
func TestTheIndexStaysBoundedAsRecordsAccumulate(t *testing.T) {
	// Arrange
	root, _ := newStore(t)
	for i := 1; i <= 60; i++ {
		id := fmt.Sprintf("D-%04d", i)
		writeRecord(t, root, fmt.Sprintf("decisions/%s-x.md", id),
			fmt.Sprintf("id: %s\nstatus: active\nclaim_kind: intent\n", id),
			fmt.Sprintf("# %s — Decision %d", id, i))
	}
	store := reload(t, root)

	// Act
	index := brain.RenderCurrent(store, reference)

	// Assert
	rows := strings.Count(index, "| `D-")
	if rows > 30 {
		t.Errorf("the index listed %d of 60 records; it is not an entry point, it is the repository", rows)
	}
	if !strings.Contains(index, "more") {
		t.Errorf("the index truncated without saying so:\n%s", index)
	}
}

// A knowledge layer whose whole claim is that it outlives the tool that wrote
// it will eventually be handed to an older tool. Reading it quietly and
// reporting it clean is the worst available outcome: the records look sound
// because half of what governs them was invisible.
func TestABrainWrittenAgainstANewerSchemaIsRefusedRatherThanReadQuietly(t *testing.T) {
	// Arrange
	root := t.TempDir()
	if _, err := brain.Init(root, reference); err != nil {
		t.Fatalf("Init: %v", err)
	}
	marker := filepath.Join(root, brain.MarkerFile)
	if err := os.WriteFile(marker,
		[]byte(fmt.Sprintf("# brain\nschema: %d\n", brain.SchemaVersion+1)), 0o644); err != nil {
		t.Fatalf("write marker: %v", err)
	}
	store, err := brain.Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	// Act
	findings := brain.Check(store, brain.CheckPolicy{}, reference)

	// Assert
	found := false
	for _, finding := range findings {
		if finding.Rule == "brain/schema-newer" {
			found = true
			if finding.Severity != brain.SeverityError {
				t.Errorf("severity = %s, want error", finding.Severity)
			}
		}
	}
	if !found {
		t.Errorf("an unreadable schema went unreported: %+v", findings)
	}
}

// The version vat writes must be the version vat accepts, or every freshly
// scaffolded brain greets its owner with an error.
func TestAFreshlyScaffoldedBrainDeclaresASchemaThisBuildAccepts(t *testing.T) {
	// Arrange
	root := t.TempDir()
	if _, err := brain.Init(root, reference); err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Act
	declared, ok := brain.DeclaredSchema(root)

	// Assert
	if !ok {
		t.Fatal("a scaffolded brain records no schema version")
	}
	if declared != brain.SchemaVersion {
		t.Errorf("declared schema %d, this build is %d", declared, brain.SchemaVersion)
	}
	store, err := brain.Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	for _, finding := range brain.Check(store, brain.CheckPolicy{}, reference) {
		if finding.Rule == "brain/schema-newer" {
			t.Errorf("a fresh brain reports its own schema as unreadable: %+v", finding)
		}
	}
}

// A version this build cannot parse is the strongest available signal that
// something other than vat wrote the marker — which is the case the field
// exists for. Falling through to "predates versioning" produced silence.
func TestAnUnparseableSchemaIsReportedRatherThanReadAsAncient(t *testing.T) {
	// Arrange
	root := t.TempDir()
	if _, err := brain.Init(root, reference); err != nil {
		t.Fatalf("Init: %v", err)
	}
	for _, value := range []string{"2.0", "v2", "two", "-1"} {
		if err := os.WriteFile(filepath.Join(root, brain.MarkerFile),
			[]byte("# brain\nschema: "+value+"\n"), 0o644); err != nil {
			t.Fatalf("write marker: %v", err)
		}
		store, err := brain.Load(root)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}

		// Act
		findings := brain.Check(store, brain.CheckPolicy{}, reference)

		// Assert
		found := false
		for _, finding := range findings {
			if finding.Rule == "brain/schema-newer" {
				found = true
			}
		}
		if !found {
			t.Errorf("schema %q was read as a pre-versioning brain and reported clean", value)
		}
	}
}

// The same failure the harness had: under git's default on Windows a committed
// projection comes back with CRLF, so a byte comparison reported CURRENT.md and
// graph.json as drifted on every run, and every build rewrote both.
func TestALineEndingIsNotProjectionDrift(t *testing.T) {
	// Arrange
	root := t.TempDir()
	if _, err := brain.Init(root, reference); err != nil {
		t.Fatalf("Init: %v", err)
	}
	store, err := brain.Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if _, err := brain.Build(store, reference); err != nil {
		t.Fatalf("Build: %v", err)
	}
	for _, name := range brain.Generated() {
		path := filepath.Join(root, name)
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		crlf := strings.ReplaceAll(string(content), "\n", "\r\n")
		if err := os.WriteFile(path, []byte(crlf), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	// Act
	drifted, err := brain.Drift(store, reference)
	if err != nil {
		t.Fatalf("Drift: %v", err)
	}
	rebuilt, err := brain.Build(store, reference)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	// Assert
	if len(drifted) != 0 {
		t.Errorf("projections reported as drifted for their line endings: %v", drifted)
	}
	if len(rebuilt.Changed) != 0 {
		t.Errorf("projections rewritten for their line endings: %v", rebuilt.Changed)
	}
}
