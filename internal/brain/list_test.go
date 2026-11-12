package brain

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

func listFixture(t *testing.T) string {
	t.Helper()
	root := newStore(t)
	mustCreate(t, root, NewRecordInput{Kind: KindDecision, ID: "D-0002", Title: "Second", Status: StatusActive,
		ExtensionFields: map[string]any{"project": "checkout"}})
	mustCreate(t, root, NewRecordInput{Kind: KindDecision, ID: "D-0001", Title: "First",
		ExtensionFields: map[string]any{"project": "billing"}})
	mustCreate(t, root, NewRecordInput{Kind: KindGap, ID: "G-0001", Title: "A gap", Status: StatusActive,
		ClaimKind: ClaimCurrentState, OwnedBy: "payments", SourceRef: "payments@3f9a1c2e8b74",
		ExtensionFields: map[string]any{"project": "checkout"}})
	writeRecordFile(t, root, "archive/decisions/D-0003-old.md",
		"---\nid: D-0003\nstatus: superseded\nsuperseded_by: D-0002\n---\n\n# D-0003 — Old\n")
	return root
}

func ids(result ListResult) []string {
	out := make([]string, 0, len(result.Records))
	for _, entry := range result.Records {
		out = append(out, entry.ID)
	}
	return out
}

func TestListFiltersStatusKindAndExtensions(t *testing.T) {
	store := mustLoad(t, listFixture(t))
	policy := CheckPolicy{StaleAfterDays: 90}
	cases := []struct {
		name string
		opts ListOptions
		want []string
	}{
		{"everything, archive included, by id", ListOptions{}, []string{"D-0001", "D-0002", "D-0003", "G-0001"}},
		{"status", ListOptions{Status: StatusActive}, []string{"D-0002", "G-0001"}},
		{"kind", ListOptions{Kind: KindGap}, []string{"G-0001"}},
		{"extension", ListOptions{Where: map[string]string{"project": "checkout"}}, []string{"D-0002", "G-0001"}},
		{"all together", ListOptions{Status: StatusActive, Kind: KindDecision, Where: map[string]string{"project": "checkout"}}, []string{"D-0002"}},
		{"unknown key", ListOptions{Where: map[string]string{"team": "x"}}, []string{}},
		{"terminal", ListOptions{Status: StatusSuperseded}, []string{"D-0003"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ids(List(store, tc.opts, policy, observedOn))
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestListCitabilityUsesReadTime(t *testing.T) {
	store := mustLoad(t, listFixture(t))
	policy := CheckPolicy{StaleAfterDays: 90}
	entry := func(now time.Time) ListEntry {
		for _, e := range List(store, ListOptions{Kind: KindGap}, policy, now).Records {
			return e
		}
		t.Fatal("no gap listed")
		return ListEntry{}
	}
	until := observedOn.AddDate(0, 0, 90)
	if e := entry(until); !e.Citable || e.CitableUntil != until.Format("2006-01-02") {
		t.Errorf("on the last day: %+v", e)
	}
	if e := entry(until.AddDate(0, 0, 1)); e.Citable {
		t.Errorf("a day past the window is still citable: %+v", e)
	}
	// Not current-state: citable without an expiry; terminal: never.
	for _, e := range List(store, ListOptions{Kind: KindDecision}, policy, observedOn).Records {
		switch e.ID {
		case "D-0002":
			if !e.Citable || e.CitableUntil != "" {
				t.Errorf("active decision: %+v", e)
			}
		case "D-0003":
			if e.Citable || e.SupersededBy != "D-0002" {
				t.Errorf("superseded decision: %+v", e)
			}
		}
	}
}

func TestListReportsMalformedAndKeepsOtherRows(t *testing.T) {
	root := listFixture(t)
	writeRecordFile(t, root, "decisions/D-0009-broken.md", "---\nid: [unclosed\n---\n\n# x\n")
	result := List(mustLoad(t, root), ListOptions{}, CheckPolicy{StaleAfterDays: 90}, observedOn)
	if len(result.Records) != 4 {
		t.Errorf("the readable records were not all listed: %v", ids(result))
	}
	if len(result.Skipped) != 1 || result.Skipped[0].Path != "decisions/D-0009-broken.md" {
		t.Fatalf("skipped = %+v", result.Skipped)
	}
	if strings.Contains(result.Skipped[0].Reason, "unclosed") {
		t.Errorf("the reason quotes the file: %q", result.Skipped[0].Reason)
	}
}

func TestEmptyListJSONUsesArrays(t *testing.T) {
	result := List(mustLoad(t, newStore(t)), ListOptions{}, CheckPolicy{}, observedOn)
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"records":[],"skipped":[]}` {
		t.Errorf("empty list = %s", data)
	}
	full, err := json.Marshal(List(mustLoad(t, listFixture(t)), ListOptions{Kind: KindGap}, CheckPolicy{StaleAfterDays: 90}, observedOn))
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Records []map[string]any `json:"records"`
	}
	if err := json.Unmarshal(full, &decoded); err != nil {
		t.Fatal(err)
	}
	keys := make([]string, 0, len(decoded.Records[0]))
	for key := range decoded.Records[0] {
		keys = append(keys, key)
	}
	want := []string{"citable", "citable_until", "claim_kind", "date", "extensions", "id", "kind", "observed_at",
		"path", "promoted_via", "source_external", "source_quote", "source_url", "status", "superseded_by",
		"supersedes", "title"}
	if !sameSet(keys, want) {
		t.Errorf("fields = %v, want %v", keys, want)
	}
}

func sameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	seen := map[string]bool{}
	for _, item := range a {
		seen[item] = true
	}
	for _, item := range b {
		if !seen[item] {
			return false
		}
	}
	return true
}
