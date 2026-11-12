package brain_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/takealook97/vat/internal/brain"
)

func TestAgentPromotionChecksEveryRecordKind(t *testing.T) {
	for _, kind := range brain.Kinds() {
		for _, claim := range []brain.ClaimKind{"", brain.ClaimCurrentState} {
			for _, sample := range []struct {
				name, source, head                      string
				reverified, wantCurrentOK, wantPinnedOK bool
			}{
				{name: "missing pin"},
				{name: "repo pin", source: "payments@abcdef1234", head: "abcdef1234"},
				{name: "branch pin", source: "payments@main:README.md", head: "abcdef1234", reverified: true},
				{name: "unreadable HEAD", source: "payments@abcdef1234:README.md", reverified: true, wantPinnedOK: true},
				{name: "unchanged", source: "payments@abcdef1234:README.md", head: "abcdef1234", wantCurrentOK: true, wantPinnedOK: true},
				{name: "moved", source: "payments@abcdef1234:README.md", head: "fedcba4321", wantPinnedOK: true},
				{name: "reverified", source: "payments@abcdef1234:README.md", head: "fedcba4321", reverified: true, wantCurrentOK: true, wantPinnedOK: true},
			} {
				t.Run(string(kind)+"/"+string(claim)+"/"+sample.name, func(t *testing.T) {
					root, _ := newStore(t)
					id := kind.Prefix() + "-0001"
					_, err := brain.Create(root, brain.NewRecordInput{Kind: kind, ID: id, Title: "Evidence", SourceRef: sample.source, ClaimKind: claim, OwnedBy: "payments", Now: reference})
					if err != nil {
						t.Fatal(err)
					}
					record := reload(t, root).ByID()[id]
					request := brain.PromoteRequest{Reviewer: "Fixture Author <author@example.com>", Now: reference, AgentGate: true, SourceRevision: sample.head, Reverified: sample.reverified}
					err = brain.Promote(root, record, request)
					wantOK := sample.wantPinnedOK || kind == brain.KindGoal
					if record.IsCurrentStateClaim() {
						wantOK = sample.wantCurrentOK
						if kind == brain.KindGoal && (sample.name == "repo pin" || sample.name == "branch pin" || sample.name == "unreadable HEAD") {
							wantOK = true
						}
					}
					if (err == nil) != wantOK {
						t.Fatalf("Promote: %v; want success %v", err, wantOK)
					}
					updated := reload(t, root).ByID()[id]
					if !wantOK && updated.Status != brain.StatusProvisional {
						t.Fatal("refusal mutated record")
					}
					if wantOK && updated.ReviewedBy != request.Reviewer {
						t.Fatal("missing identity")
					}
					if wantOK {
						wantSource := record.SourceRef
						if record.IsCurrentStateClaim() && sample.reverified && sample.head != "" {
							wantSource = "payments@" + sample.head + ":README.md"
						}
						if updated.SourceRef != wantSource {
							t.Fatalf("evidence pin %s, want %s", updated.SourceRef, wantSource)
						}
					}
				})
			}
		}
	}
}

func TestReviewerUnattributedIsGateSensitive(t *testing.T) {
	for _, gate := range []string{"manual", "agent", "auto", ""} {
		for _, status := range []brain.Status{brain.StatusActive, brain.StatusProvisional, brain.StatusStale} {
			for _, reviewer := range []string{"", "Fixture Author <author@example.com>"} {
				store := &brain.Store{Records: []brain.Record{{Metadata: brain.Metadata{ID: "D-0001", Status: status, ReviewedBy: reviewer}, Kind: brain.KindDecision}}}
				findings := brain.Check(store, brain.CheckPolicy{PromotionGate: gate, Only: []string{"brain/reviewer-unattributed"}}, reference)
				want := (gate == "manual" || gate == "agent") && status == brain.StatusActive && reviewer == ""
				if (len(findings) == 1) != want {
					t.Fatalf("gate %q status %s reviewer %q: %v", gate, status, reviewer, findings)
				}
				if want && (findings[0].Severity != brain.SeverityWarn || findings[0].Fixable) {
					t.Fatalf("incorrect severity or repair: %v", findings)
				}
			}
		}
	}
}

// Callers built before PromotionGate existed set only the two booleans. They
// must keep getting the same refusals, and the record must name the gate
// those booleans describe.
func TestLegacyPromotionRequestStillEnforcesGate(t *testing.T) {
	const reviewer = "Fixture Author <author@example.com>"
	for _, sample := range []struct {
		name             string
		kind             brain.Kind
		request          brain.PromoteRequest
		wantErr, wantVia string
	}{
		{name: "auto", request: brain.PromoteRequest{}, wantVia: "auto"},
		{name: "manual", request: brain.PromoteRequest{Reviewer: reviewer, RequireReviewer: true}, wantVia: "manual"},
		{name: "manual without identity", request: brain.PromoteRequest{RequireReviewer: true}, wantErr: "requires a git identity"},
		{name: "agent", kind: brain.KindGoal, request: brain.PromoteRequest{Reviewer: reviewer, RequireReviewer: true, AgentGate: true}, wantVia: "agent"},
		{name: "agent without identity", request: brain.PromoteRequest{AgentGate: true}, wantErr: "requires a git identity"},
		{name: "explicit manual implies identity", request: brain.PromoteRequest{PromotionGate: "manual"}, wantErr: "requires a git identity"},
		{name: "explicit agent implies evidence", request: brain.PromoteRequest{Reviewer: reviewer, PromotionGate: "agent"}, wantErr: "source_ref pinned with a path"},
		{name: "explicit auto against a required reviewer", request: brain.PromoteRequest{Reviewer: reviewer, RequireReviewer: true, PromotionGate: "auto"}, wantErr: "does not match"},
		{name: "explicit manual against the agent gate", request: brain.PromoteRequest{Reviewer: reviewer, AgentGate: true, PromotionGate: "manual"}, wantErr: "does not match"},
		{name: "unknown gate", request: brain.PromoteRequest{Reviewer: reviewer, PromotionGate: "bot"}, wantErr: "not a promotion gate"},
	} {
		t.Run(sample.name, func(t *testing.T) {
			// Arrange
			root, _ := newStore(t)
			kind := sample.kind
			if kind == "" {
				kind = brain.KindDecision
			}
			id := kind.Prefix() + "-0001"
			path, err := brain.Create(root, brain.NewRecordInput{Kind: kind, ID: id, Title: "Gate", Now: reference})
			if err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(filepath.Join(root, path))
			if err != nil {
				t.Fatal(err)
			}
			request := sample.request
			request.Now = reference

			// Act
			err = brain.Promote(root, reload(t, root).ByID()[id], request)

			// Assert
			updated := reload(t, root).ByID()[id]
			if sample.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), sample.wantErr) {
					t.Fatalf("want refusal %q, got %v", sample.wantErr, err)
				}
				if after, _ := os.ReadFile(filepath.Join(root, path)); string(after) != string(before) {
					t.Fatal("a refused promotion changed the record")
				}
				return
			}
			if err != nil {
				t.Fatalf("Promote: %v", err)
			}
			if updated.PromotedVia != sample.wantVia || updated.Status != brain.StatusActive {
				t.Fatalf("promoted_via %q status %s, want %q active", updated.PromotedVia, updated.Status, sample.wantVia)
			}
		})
	}
}

// promoted_via names the last promotion, so re-promoting under another gate
// replaces it, and every other lifecycle write carries it along.
func TestPromotedViaFollowsTheLastPromotionAndSurvivesLifecycle(t *testing.T) {
	// Arrange
	root, _ := newStore(t)
	for _, id := range []string{"D-0001", "D-0002"} {
		if _, err := brain.Create(root, brain.NewRecordInput{Kind: brain.KindDecision, ID: id, Title: "Decision " + id, Now: reference}); err != nil {
			t.Fatal(err)
		}
	}
	legacy := reload(t, root)
	if findings := brain.Check(legacy, brain.CheckPolicy{}, reference); len(findings) != 0 {
		t.Fatalf("a record without promoted_via is reported: %+v", findings)
	}

	// Act
	if err := brain.Promote(root, reload(t, root).ByID()["D-0001"], brain.PromoteRequest{Now: reference}); err != nil {
		t.Fatal(err)
	}
	if got := reload(t, root).ByID()["D-0001"].PromotedVia; got != "auto" {
		t.Fatalf("first promotion: %q", got)
	}
	if err := brain.Promote(root, reload(t, root).ByID()["D-0001"], brain.PromoteRequest{
		Now: reference, Reviewer: "Fixture Author <author@example.com>", PromotionGate: "manual",
	}); err != nil {
		t.Fatal(err)
	}
	store := reload(t, root)
	if err := brain.Supersede(root, store.ByID()["D-0001"], store.ByID()["D-0002"], brain.SupersedeOptions{}); err != nil {
		t.Fatal(err)
	}

	// Assert
	store = reload(t, root)
	if got := store.ByID()["D-0001"].PromotedVia; got != "manual" {
		t.Errorf("re-promotion or supersession lost the gate: %q", got)
	}
	if got := store.ByID()["D-0002"].PromotedVia; got != "" {
		t.Errorf("supersession invented a promotion path for its replacement: %q", got)
	}
}
