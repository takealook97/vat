package cli

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

type listPayload struct {
	Records []struct {
		ID         string            `json:"id"`
		Status     string            `json:"status"`
		Kind       string            `json:"kind"`
		Citable    bool              `json:"citable"`
		Extensions map[string]string `json:"extensions"`
	} `json:"records"`
	Skipped []struct {
		Path   string `json:"path"`
		Reason string `json:"reason"`
	} `json:"skipped"`
}

func listIDs(payload listPayload) string {
	ids := make([]string, 0, len(payload.Records))
	for _, record := range payload.Records {
		ids = append(ids, record.ID)
	}
	return strings.Join(ids, ",")
}

func listFixtureCLI(t *testing.T) *workspaceFixture {
	t.Helper()
	h := brainFixture(t)
	h.mustRun("brain", "new", "decision", "--title", "Second", "--id", "D-0002", "--set", "project=checkout")
	h.mustRun("brain", "new", "decision", "--title", "First", "--id", "D-0001", "--set", "project=billing")
	h.mustRun("brain", "new", "goal", "--title", "An outcome", "--set", "project=checkout")
	h.mustRun("brain", "promote", "D-0002")
	return h
}

func TestBrainListFiltersAndOrders(t *testing.T) {
	h := listFixtureCLI(t)
	cases := []struct {
		args []string
		want string
	}{
		{nil, "D-0001,D-0002,O-0001"},
		{[]string{"--status", "active"}, "D-0002"},
		{[]string{"--kind", "goal"}, "O-0001"},
		{[]string{"--where", "project=checkout"}, "D-0002,O-0001"},
		{[]string{"--where", "project=checkout", "--kind", "decision", "--status", "active"}, "D-0002"},
		{[]string{"--where", "team=x"}, ""},
	}
	for _, tc := range cases {
		var payload listPayload
		code := h.runJSON(&payload, append([]string{"brain", "list"}, tc.args...)...)
		if code != ExitOK || listIDs(payload) != tc.want {
			t.Errorf("%v: exit %d, got %q, want %q", tc.args, code, listIDs(payload), tc.want)
		}
	}
	var payload listPayload
	h.runJSON(&payload, "brain", "list", "--status", "active")
	if !payload.Records[0].Citable || payload.Records[0].Extensions["project"] != "checkout" {
		t.Errorf("entry fields: %+v", payload.Records[0])
	}
	if output := h.mustRun("brain", "list"); !strings.Contains(output, "D-0002") || !strings.Contains(output, "3 records") {
		t.Errorf("human listing:\n%s", output)
	}
}

func TestBrainListEmptyIsArraysAndExitsZero(t *testing.T) {
	h := brainFixture(t)
	code, stdout, _ := h.runSplit("brain", "list")
	if code != ExitOK {
		t.Fatalf("exit %d", code)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(stdout), &raw); err != nil {
		t.Fatalf("not one JSON value: %v\n%s", err, stdout)
	}
	if string(raw["records"]) != "[]" || string(raw["skipped"]) != "[]" {
		t.Errorf("empty listing = %s", stdout)
	}
}

func TestBrainListReportsMalformedAndKeepsOtherRows(t *testing.T) {
	h := listFixtureCLI(t)
	if err := os.WriteFile(h.path("brain", "decisions", "D-0009-broken.md"), []byte("---\nid: [unclosed\n---\n# x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := h.runSplit("brain", "list")
	if code != ExitFindings {
		t.Errorf("exit %d, want %d", code, ExitFindings)
	}
	var payload listPayload
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("stdout is not one JSON value: %v\n%s", err, stdout)
	}
	if listIDs(payload) != "D-0001,D-0002,O-0001" || len(payload.Skipped) != 1 || payload.Skipped[0].Path != "decisions/D-0009-broken.md" {
		t.Errorf("payload: %+v", payload)
	}
	if strings.Contains(stdout+stderr, "unclosed") {
		t.Error("the listing quoted the unreadable file")
	}
	if !strings.Contains(stderr, "decisions/D-0009-broken.md") {
		t.Errorf("stderr does not name the skipped file:\n%s", stderr)
	}
}

func TestBrainListRefusesBadFilters(t *testing.T) {
	h := brainFixture(t)
	for _, args := range [][]string{
		{"--status", "maybe"},
		{"--kind", "note"},
		{"--where", "project"},
		{"--where", "status=active"},
		{"stray"},
	} {
		if code, output := h.run(append([]string{"brain", "list"}, args...)...); code != ExitUsage {
			t.Errorf("%v: exit %d:\n%s", args, code, output)
		}
	}
}
