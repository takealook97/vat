package brain_test

import (
	"encoding/json"
	"runtime/debug"
	"strings"
	"testing"

	"github.com/takealook97/vat/internal/brain"
	"github.com/takealook97/vat/internal/frontmatter"
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

func TestLoadResolvesMergeKeysAsFieldsDoes(t *testing.T) {
	cases := map[string]string{
		"single alias":         "shared: &shared {project: demo-project}\n<<: *shared\n",
		"explicit key wins":    "shared: &shared {project: other, extra: x}\n<<: *shared\nproject: demo-project\n",
		"earlier source wins":  "a: &a {project: demo-project}\nb: &b {project: other, extra: y}\n<<: [*a, *b]\n",
		"nested merge":         "a: &a {project: demo-project}\nb: &b {<<: *a, extra: z}\n<<: *b\n",
		"merged defined field": "shared: &shared {status: superseded, project: demo-project}\n<<: *shared\n",
		"alias of a mapping":   "shared: &shared {project: demo-project}\ncopy: *shared\n<<: *shared\n",
	}
	for name, header := range cases {
		t.Run(name, func(t *testing.T) {
			raw := "id: D-0001\n" + header
			fields, err := frontmatter.Document{Present: true, Raw: raw}.Fields()
			if err != nil || fields["project"] != "demo-project" {
				t.Fatalf("fixture not merged by Fields: %v %v", fields, err)
			}
			root, _ := newStore(t)
			writeRecord(t, root, "decisions/D-0001-review.md", raw, "# Review")
			store, err := brain.Load(root)
			if err != nil {
				t.Fatal(err)
			}
			record, ok := store.ByID()["D-0001"]
			if !ok {
				t.Fatalf("record dropped: %+v", store.Malformed)
			}
			if len(record.ExtensionProblems) > 0 {
				t.Errorf("merge reported as malformed: %v", record.ExtensionProblems)
			}
			for key, want := range fields {
				if brain.IsDefinedField(key) {
					continue
				}
				if got, err := json.Marshal(record.Extensions[key]); err != nil || string(got) != mustJSON(t, want) {
					t.Errorf("extension %s = %s, Fields shows %s", key, got, mustJSON(t, want))
				}
			}
			if _, leaked := record.Extensions["<<"]; leaked {
				t.Error("the merge key itself was carried as an extension")
			}
			if status, _ := fields["status"].(string); status != "" && string(record.Status) != status {
				t.Errorf("status = %s, Fields shows %s", record.Status, status)
			}
		})
	}
}

func TestLoadReportsMergeKeyThatNamesNoMapping(t *testing.T) {
	defer debug.SetMaxStack(debug.SetMaxStack(128 << 10))
	for name, header := range map[string]string{
		"scalar":       "<<: 5\nnote: kept\n",
		"self include": "loop: &loop {<<: *loop}\nnote: kept\n",
		"self alias":   "loop: &loop {inner: *loop}\nnote: kept\n",
	} {
		t.Run(name, func(t *testing.T) {
			root, _ := newStore(t)
			writeRecord(t, root, "decisions/D-0001-review.md", "id: D-0001\nstatus: active\n"+header, "# Review")
			store := reload(t, root)
			record, ok := store.ByID()["D-0001"]
			if !ok || record.Extensions["note"] != "kept" {
				t.Fatalf("record dropped: %+v", store.Malformed)
			}
			if !malformedReported(store) {
				t.Error("the merge was not reported")
			}
		})
	}
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
