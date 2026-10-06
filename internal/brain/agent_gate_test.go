package brain_test

import (
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
