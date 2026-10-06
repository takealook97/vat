package brain_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/takealook97/vat/internal/brain"
)

func TestReadersApplyTheObservationWindowWithoutChangingRecords(t *testing.T) {
	policy := brain.CheckPolicy{StaleAfterDays: 90}
	for _, tc := range []struct {
		name     string
		age      int
		claim    brain.ClaimKind
		observed string
		status   brain.Status
		want     bool
	}{
		{"expired", 120, brain.ClaimCurrentState, "dated", brain.StatusActive, false},
		{"boundary", 90, brain.ClaimCurrentState, "dated", brain.StatusActive, true},
		{"historical", 120, brain.ClaimHistorical, "dated", brain.StatusActive, true},
		{"intent", 120, brain.ClaimIntent, "dated", brain.StatusActive, true},
		{"missing", 0, brain.ClaimCurrentState, "", brain.StatusActive, false},
		{"invalid", 0, brain.ClaimCurrentState, "yesterday", brain.StatusActive, false},
		{"stale", 0, brain.ClaimCurrentState, "dated", brain.StatusStale, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			observed := tc.observed
			if observed == "dated" {
				observed = reference.AddDate(0, 0, -tc.age).Format("2006-01-02")
			}
			record := brain.Record{Metadata: brain.Metadata{ID: "D-0001", Status: tc.status, ClaimKind: tc.claim, ObservedAt: observed}, Kind: brain.KindDecision, Title: "retries", Body: "retries", Path: "decisions/D-0001.md"}
			store := &brain.Store{Root: t.TempDir(), Records: []brain.Record{record}}
			hits, _ := brain.Query(store, []string{"retries"}, brain.QueryOptions{}, policy, reference)
			if len(hits) != 1 || hits[0].Citable != tc.want {
				t.Fatalf("hits = %+v, want citable %v", hits, tc.want)
			}
			if !reflect.DeepEqual(store.Records[0], record) {
				t.Error("record changed")
			}
		})
	}
}

func TestExpiredHitsLoseTheCitableRankingBonus(t *testing.T) {
	store := &brain.Store{Root: t.TempDir(), Records: []brain.Record{
		{Metadata: brain.Metadata{ID: "D-0001", Status: brain.StatusActive, ClaimKind: brain.ClaimCurrentState, ObservedAt: reference.AddDate(0, 0, -120).Format("2006-01-02")}, Path: "decisions/old.md", Title: "retries"},
		{Metadata: brain.Metadata{ID: "D-0002", Status: brain.StatusActive, ClaimKind: brain.ClaimCurrentState, ObservedAt: reference.AddDate(0, 0, -90).Format("2006-01-02")}, Path: "decisions/new.md", Title: "retries"},
	}}
	hits, _ := brain.Query(store, []string{"retries"}, brain.QueryOptions{}, brain.CheckPolicy{StaleAfterDays: 90}, reference)
	if len(hits) != 2 || hits[0].ID != "D-0002" || hits[0].Score != hits[1].Score+5 {
		t.Fatalf("ranking = %+v", hits)
	}
}

func TestProjectionsCarryExpiryAndUseExplicitPolicyWithoutAClock(t *testing.T) {
	root, _ := newStore(t)
	observed := reference.AddDate(0, 0, -30).Format("2006-01-02")
	writeRecord(t, root, "decisions/D-0001.md", "id: D-0001\nstatus: active\nclaim_kind: current-state\nobserved_at: "+observed, "# Retrying orders")
	policy := brain.CheckPolicy{StaleAfterDays: 30}
	store := reload(t, root)
	first, err := brain.Build(store, policy)
	if err != nil {
		t.Fatal(err)
	}
	second, err := brain.Build(store, policy)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Changed) != 2 || len(second.Changed) != 0 {
		t.Fatalf("build changes: %v then %v", first.Changed, second.Changed)
	}
	drift, err := brain.Drift(store, policy)
	if err != nil || len(drift) != 0 {
		t.Fatalf("same-policy drift: %v, %v", drift, err)
	}
	drift, err = brain.Drift(store, brain.CheckPolicy{StaleAfterDays: 90})
	if err != nil || len(drift) != 2 {
		t.Fatalf("changed-policy drift: %v, %v", drift, err)
	}
	hits, _ := brain.Query(store, []string{"retrying"}, brain.QueryOptions{}, policy, reference.AddDate(0, 0, 1))
	if len(hits) != 1 || hits[0].Citable {
		t.Fatalf("expired hit = %+v", hits)
	}
	graphData, err := os.ReadFile(filepath.Join(root, brain.GraphFile))
	if err != nil {
		t.Fatal(err)
	}
	var graph brain.Graph
	if err := json.Unmarshal(graphData, &graph); err != nil {
		t.Fatal(err)
	}
	if graph.Nodes[0].CitableUntil != reference.Format("2006-01-02") || graph.StaleAfterDays != 30 {
		t.Fatalf("graph = %+v", graph)
	}
	if strings.Contains(string(graphData), "generated_at") || strings.Contains(string(graphData), "\"citable\"") {
		t.Fatalf("clock-dependent graph: %s", graphData)
	}
	current := brain.RenderCurrent(store, policy)
	if !strings.Contains(current, "## Active decisions") || !strings.Contains(current, reference.Format("2006-01-02")) || strings.Contains(current, "Rebuilt ") {
		t.Fatalf("projection: %s", current)
	}
}

func TestCurrentIndexShowsExpiryForEveryKindAndWithholdsMissingDates(t *testing.T) {
	for _, kind := range []brain.Kind{brain.KindGoal, brain.KindGap, brain.KindDecision, brain.KindMemory} {
		t.Run(string(kind), func(t *testing.T) {
			record := brain.Record{Metadata: brain.Metadata{ID: "X-0001", Status: brain.StatusActive, ClaimKind: brain.ClaimCurrentState, ObservedAt: reference.AddDate(0, 0, -120).Format("2006-01-02")}, Kind: kind, Title: "Retrying orders", Path: "records/X-0001.md"}
			store := &brain.Store{Root: t.TempDir(), Records: []brain.Record{record}}
			policy := brain.CheckPolicy{StaleAfterDays: 90}
			index := brain.RenderCurrent(store, policy)
			if !strings.Contains(index, reference.AddDate(0, 0, -30).Format("2006-01-02")) || strings.Contains(index, "## Needs attention") {
				t.Fatalf("expiry missing: %s", index)
			}
			for _, observed := range []string{"", "yesterday"} {
				store.Records[0].ObservedAt = observed
				index = brain.RenderCurrent(store, policy)
				before, attention, found := strings.Cut(index, "## Needs attention")
				if !found || strings.Contains(before, "X-0001") || !strings.Contains(attention, "observation date missing or unreadable") {
					t.Fatalf("missing-date claim: %s", index)
				}
				data, err := brain.RenderGraph(store, policy)
				if err != nil {
					t.Fatal(err)
				}
				if strings.Contains(string(data), "citable_until") {
					t.Fatalf("missing-date expiry: %s", data)
				}
			}
		})
	}
}
