package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestEmptyBrainReadJSONIsAnArray(t *testing.T) {
	h := brainFixture(t, "payments")
	for _, args := range [][]string{
		{"brain", "review"},
		{"brain", "review", "--drifted"},
		{"brain", "review", "--overdue"},
		{"brain", "query", "no-match-for-this-query"},
		{"brain", "query", " "},
	} {
		code, output, errOutput := h.runSplit(args...)
		if code != ExitOK {
			t.Fatalf("%v: %d %s", args, code, errOutput)
		}
		if strings.TrimSpace(output) != "[]" {
			t.Errorf("%v: %s", args, output)
		}
	}
}

func TestReviewJSONNamesTheKindForQueueAndDriftItems(t *testing.T) {
	h := brainFixture(t, "payments")
	h.mustRun("brain", "new", "goal", "--title", "Intent", "--claim", "current-state", "--owner", "payments", "--source-path", "README.md")
	assertGoal := func(args ...string) {
		t.Helper()
		code, output, errOutput := h.runSplit(args...)
		if code != ExitOK {
			t.Fatalf("%v: %d %s", args, code, errOutput)
		}
		var items []struct {
			ID   string `json:"id"`
			Kind string `json:"kind"`
		}
		if err := json.Unmarshal([]byte(output), &items); err != nil {
			t.Fatal(err)
		}
		if len(items) != 1 || items[0].ID != "O-0001" || items[0].Kind != "goal" {
			t.Fatalf("goal kind missing: %s", output)
		}
	}
	assertGoal("brain", "review")
	h.mustRun("brain", "promote", "O-0001")
	writeFile(t, h.path("payments", "README.md"), "Changed evidence")
	git(t, h.path("payments"), "add", "README.md")
	git(t, h.path("payments"), "commit", "--quiet", "-m", "Move source")
	assertGoal("brain", "review", "--drifted")
}
