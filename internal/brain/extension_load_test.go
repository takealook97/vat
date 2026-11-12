package brain_test

import (
	"runtime/debug"
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
