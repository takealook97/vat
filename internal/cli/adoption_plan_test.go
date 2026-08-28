package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Adoption deliberately rewrites nothing, which is right and leaves a team with
// two hundred findings and no idea which conversions are mechanical. A plan
// counts and groups; it proposes no mapping.

func TestAdoptPlanGroupsTheWorkAndWritesNothing(t *testing.T) {
	// Arrange: a knowledge repository written before this schema existed.
	h := adoptedFixture(t, "payments", "notes")
	write := func(rel, body string) {
		t.Helper()
		path := h.path("notes", filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	write("decisions/D-0001-x.md", "---\nid: D-0001\nstatus: 부분 해소\n---\n\n# D-0001 — One\n")
	write("decisions/D-0002-x.md", "---\nid: D-0002\nstatus: 운영 표본 대기\n---\n\n# D-0002 — Two\n")
	write("gaps/G-0001-x.md", "---\nid: G-0001\nstatus: active\nsuperseded_by: G-0002\n---\n\n# G-0001 — Gap\n")
	write("memory/2024-11/2024-11-03.md", "---\nid: M-0001\nstatus: active\n---\n\n# A day\n")
	write("CURRENT.md", "# Maintained by hand\n")

	// Act
	var plan struct {
		Records int `json:"records"`
		Groups  []struct {
			Kind       string `json:"kind"`
			Count      int    `json:"count"`
			Mechanical bool   `json:"mechanical"`
		} `json:"groups"`
	}
	h.runJSON(&plan, "brain", "adopt", "notes", "--plan")

	// Assert
	groups := map[string]int{}
	for _, group := range plan.Groups {
		groups[group.Kind] = group.Count
	}
	for _, want := range []string{"status-unknown", "projection-unmanaged", "journal-shaped"} {
		if groups[want] == 0 {
			t.Errorf("the plan does not group %s: %+v", want, plan.Groups)
		}
	}
	// Nothing at all: not the marker, not the manifest, not a projection. The
	// applying path writes all three before it reads anything.
	for _, written := range []string{".brain", "graph.json"} {
		if _, err := os.Stat(h.path("notes", written)); err == nil {
			t.Errorf("the plan wrote %s", written)
		}
	}
	manifestText, err := os.ReadFile(h.path("vat.yaml"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if strings.Contains(string(manifestText), "role: brain") {
		t.Errorf("the plan adopted the repository:\n%s", manifestText)
	}
	before, err := os.ReadFile(h.path("notes", "CURRENT.md"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(before) != "# Maintained by hand\n" {
		t.Errorf("the plan touched the existing index:\n%s", before)
	}
}

func TestAdoptPlanSeparatesMechanicalWorkFromDecisions(t *testing.T) {
	// Arrange: the distinction is the point. A one-sided supersession link can
	// be completed without knowing what either record means; a status this
	// schema does not have cannot.
	h := adoptedFixture(t, "payments", "notes")
	path := h.path("notes", "decisions")
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	for name, body := range map[string]string{
		"D-0001-x.md": "---\nid: D-0001\nstatus: active\nsupersedes: [D-0002]\n---\n\n# D-0001 — One\n",
		"D-0002-x.md": "---\nid: D-0002\nstatus: active\n---\n\n# D-0002 — Two\n",
	} {
		if err := os.WriteFile(filepath.Join(path, name), []byte(body), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}

	// Act
	var plan struct {
		Groups []struct {
			Kind       string `json:"kind"`
			Mechanical bool   `json:"mechanical"`
		} `json:"groups"`
	}
	h.runJSON(&plan, "brain", "adopt", "notes", "--plan")

	// Assert
	var found bool
	for _, group := range plan.Groups {
		if group.Kind == "relation-asymmetric" {
			found = true
			if !group.Mechanical {
				t.Error("a one-sided link is grouped as a decision; completing it needs no judgement")
			}
		}
	}
	if !found {
		t.Errorf("the one-sided supersession was not grouped: %+v", plan.Groups)
	}
}
