package cli

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestBrainQueryReportsExpiredClaimsWithoutRewritingThem(t *testing.T) {
	h := brainFixture(t)
	for i, age := range []int{120, 90} {
		path := h.path(fmt.Sprintf("brain/decisions/D-%04d.md", i+1))
		content := fmt.Sprintf("---\nid: D-%04d\nstatus: active\nclaim_kind: current-state\nobserved_at: %s\n---\n\n# Retrying orders\n", i+1, testNow.AddDate(0, 0, -age).Format("2006-01-02"))
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	before, err := os.ReadFile(h.path("brain/decisions/D-0001.md"))
	if err != nil {
		t.Fatal(err)
	}
	code, output := h.run("brain", "query", "retrying")
	if code != 0 || !strings.Contains(output, "expired") || !strings.Contains(output, "WARN") {
		t.Fatalf("query code %d:\n%s", code, output)
	}
	var hits []struct {
		ID      string `json:"id"`
		Status  string `json:"status"`
		Citable *bool  `json:"citable"`
	}
	h.runJSON(&hits, "brain", "query", "retrying")
	if len(hits) != 2 {
		t.Fatalf("hits = %+v", hits)
	}
	for _, hit := range hits {
		want := hit.ID == "D-0002"
		if hit.Status != "active" || hit.Citable == nil || *hit.Citable != want {
			t.Errorf("hit = %+v, want citable %v", hit, want)
		}
	}
	after, err := os.ReadFile(h.path("brain/decisions/D-0001.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Error("query rewrote the expired record")
	}
	manifestPath := h.path("vat.yaml")
	manifestBytes, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	customPolicy := strings.Replace(string(manifestBytes), "stale_after_days: 90", "stale_after_days: 60", 1)
	if customPolicy == string(manifestBytes) {
		t.Fatal("fixture has no default observation window")
	}
	if err := os.WriteFile(manifestPath, []byte(customPolicy), 0o644); err != nil {
		t.Fatal(err)
	}
	h.runJSON(&hits, "brain", "query", "retrying")
	for _, hit := range hits {
		if hit.Citable == nil || *hit.Citable {
			t.Errorf("custom window ignored: %+v", hit)
		}
	}
	h.mustRun("brain", "build")
	current, err := os.ReadFile(h.path("brain/CURRENT.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(current), testNow.AddDate(0, 0, -60).Format("2006-01-02")) {
		t.Errorf("missing expiry date:\n%s", current)
	}
}

func TestBrainBuildAndLintRepairUseTheSameWorkspaceWindow(t *testing.T) {
	h := brainFixture(t)
	path := h.path("vat.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	custom := strings.Replace(string(data), "stale_after_days: 90", "stale_after_days: 30", 1)
	if custom == string(data) {
		t.Fatal("fixture has no default window")
	}
	if err := os.WriteFile(path, []byte(custom), 0o644); err != nil {
		t.Fatal(err)
	}
	content := fmt.Sprintf("---\nid: D-0001\nstatus: active\nclaim_kind: current-state\nobserved_at: %s\n---\n\n# Retrying orders\n", testNow.AddDate(0, 0, -60).Format("2006-01-02"))
	if err := os.WriteFile(h.path("brain/decisions/D-0001.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	h.mustRun("brain", "build")
	before := map[string]string{}
	for _, name := range []string{"CURRENT.md", "graph.json"} {
		data, err := os.ReadFile(h.path("brain/" + name))
		if err != nil {
			t.Fatal(err)
		}
		before[name] = string(data)
	}
	code, output := h.run("lint", "--fix", "--offline")
	if code != 0 && code != 1 {
		t.Fatalf("lint repair code %d: %s", code, output)
	}
	for name, want := range before {
		data, err := os.ReadFile(h.path("brain/" + name))
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != want {
			t.Errorf("lint repair changed %s under a 30-day window", name)
		}
	}
	output = h.mustRun("brain", "build")
	if strings.Contains(output, "regenerated") {
		t.Errorf("second build changed projections: %s", output)
	}
}
