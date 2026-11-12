package brain_test

import (
	"runtime/debug"
	"strings"
	"testing"

	"github.com/takealook97/vat/internal/brain"
)

// malformedReported reports whether Check raised brain/record-malformed.
func malformedReported(store *brain.Store) bool {
	for _, finding := range brain.Check(store, brain.CheckPolicy{}, reference) {
		if finding.Rule == "brain/record-malformed" {
			return true
		}
	}
	return false
}

func TestLoadKeepsRecordWithRecursiveExtensionAlias(t *testing.T) {
	// A small stack turns unbounded recursion into a quick crash instead of a
	// gigabyte of goroutine stack.
	defer debug.SetMaxStack(debug.SetMaxStack(128 << 10))
	root, _ := newStore(t)
	writeRecord(t, root, "decisions/D-0001-review.md",
		"id: D-0001\nstatus: active\nextra: &loop [*loop]\nnote: kept\n", "# Review")

	store, err := brain.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	record, ok := store.ByID()["D-0001"]
	if !ok || record.Extensions["note"] != "kept" {
		t.Fatalf("record was dropped: malformed=%+v", store.Malformed)
	}
	if _, carried := record.Extensions["extra"]; carried {
		t.Error("a recursive extension was carried into the graph")
	}
	if !malformedReported(store) {
		t.Error("recursive extension was not reported as malformed")
	}
}

func TestMalformedHeaderFindingsNeverQuoteTheValue(t *testing.T) {
	// Assembled at run time so no credential-shaped literal sits in the tree.
	token := "gh" + "p_" + strings.Repeat("A1", 18)
	cases := map[string]string{
		"failed integer conversion": "api_key: !!int " + token + "\n",
		"failed boolean conversion": "api_key: !!bool " + token + "\n",
		"custom tag":                "api_key: !" + token + " value\n",
		"tagged key":                "!!int " + token + ": value\n",
		"defined field wrong type":  "source_external: " + token + "\n",
	}
	for name, header := range cases {
		t.Run(name, func(t *testing.T) {
			root, _ := newStore(t)
			writeRecord(t, root, "decisions/D-0001-review.md", "id: D-0001\nstatus: active\n"+header, "# Review")
			store, err := brain.Load(root)
			if err != nil {
				t.Fatal(err)
			}
			if !malformedReported(store) {
				t.Fatal("the header was not reported as malformed")
			}
			for _, finding := range brain.Check(store, brain.CheckPolicy{}, reference) {
				// yaml.v3 shortens a long value to its first seven bytes, which
				// is still part of the credential.
				if strings.Contains(finding.Message, token[:7]) {
					t.Fatalf("finding quoted the synthetic credential (rule=%s)", finding.Rule)
				}
			}
			for _, broken := range store.Malformed {
				if strings.Contains(broken.Problem, token[:7]) {
					t.Fatal("malformed problem quoted the synthetic credential")
				}
			}
		})
	}
}

func TestMalformedExtensionNamesLineAndSafeKey(t *testing.T) {
	root, _ := newStore(t)
	writeRecord(t, root, "decisions/D-0001-review.md",
		"id: D-0001\nstatus: active\nweight: !!int 12x\n", "# Review")
	record := reload(t, root).ByID()["D-0001"]
	if len(record.ExtensionProblems) != 1 {
		t.Fatalf("problems = %v, want one", record.ExtensionProblems)
	}
	problem := record.ExtensionProblems[0]
	if !strings.Contains(problem, "weight") || !strings.Contains(problem, "line 4") || strings.Contains(problem, "12x") {
		t.Errorf("problem = %q, want the key and its line without the value", problem)
	}
}

func TestLoadKeepsRecordWithNonStringHeaderKey(t *testing.T) {
	for _, key := range []string{"true: value", "1: value", "? [a, b]\n: value", "? {a: b}\n: value"} {
		t.Run(strings.ReplaceAll(key, "\n", " "), func(t *testing.T) {
			root, _ := newStore(t)
			writeRecord(t, root, "decisions/D-0001-review.md",
				"id: D-0001\nstatus: active\n"+key+"\nnote: kept\n", "# Review")
			store, err := brain.Load(root)
			if err != nil {
				t.Fatal(err)
			}
			record, ok := store.ByID()["D-0001"]
			if !ok || record.Extensions["note"] != "kept" || record.Status != brain.StatusActive {
				t.Fatalf("record dropped or misread: malformed=%+v", store.Malformed)
			}
			if !malformedReported(store) {
				t.Fatal("the key that is not a string was not reported")
			}
		})
	}
}
