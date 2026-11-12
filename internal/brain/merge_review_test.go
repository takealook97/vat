package brain_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/takealook97/vat/internal/brain"
	"github.com/takealook97/vat/internal/frontmatter"
)

// mergeLoadDeadline is far above the milliseconds a linear resolution takes,
// so a slow CI runner passes, and far below the hours an exponential one would.
const mergeLoadDeadline = 20 * time.Second

func loadWithin(t *testing.T, root string) *brain.Store {
	t.Helper()
	type result struct {
		store *brain.Store
		err   error
	}
	done := make(chan result, 1)
	go func() {
		store, err := brain.Load(root)
		done <- result{store, err}
	}()
	select {
	case got := <-done:
		if got.err != nil {
			t.Fatal(got.err)
		}
		return got.store
	case <-time.After(mergeLoadDeadline):
		t.Fatalf("Load did not finish within %s", mergeLoadDeadline)
		return nil
	}
}

func hasMalformed(store *brain.Store) bool {
	for _, f := range brain.Check(store, brain.CheckPolicy{}, reference) {
		if f.Rule == "brain/record-malformed" {
			return true
		}
	}
	return false
}

func TestFixReviewRepeatedMergeKeysAreMalformed(t *testing.T) {
	root, _ := newStore(t)
	header := "id: D-0001\n<<: {status: active}\n<<: {status: revoked}\n"
	if _, err := (frontmatter.Document{Present: true, Raw: header}).Fields(); err == nil {
		t.Fatal("fixture must be invalid in the generic YAML decoder")
	}
	writeRecord(t, root, "decisions/D-0001-example.md", header, "# Example")
	store, err := brain.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if hasMalformed(store) {
		return
	}
	t.Fatalf("duplicate merge keys accepted as healthy; status=%s", store.ByID()["D-0001"].Status)
}

func TestRepeatedNestedMergeKeysLoseOnlyTheExtension(t *testing.T) {
	root, _ := newStore(t)
	header := "id: D-0001\nstatus: provisional\nnote: kept\nextra: {<<: {a: 1}, <<: {b: 2}}\n"
	writeRecord(t, root, "decisions/D-0001-example.md", header, "# Example")
	store, err := brain.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if !hasMalformed(store) {
		t.Fatal("a nested duplicate merge key was not reported")
	}
	record := store.ByID()["D-0001"]
	if record.Status != brain.StatusProvisional || record.Extensions["note"] != "kept" {
		t.Fatalf("the rest of the record was lost: %+v", record)
	}
	if _, ok := record.Extensions["extra"]; ok {
		t.Fatal("the extension with the duplicate merge key was carried")
	}
}

func TestFixReviewMergeExpansionIsBounded(t *testing.T) {
	root, _ := newStore(t)
	var h strings.Builder
	h.WriteString("id: D-0001\nstatus: active\na0: &a0 {project: example}\n")
	for i := 1; i <= 30; i++ {
		fmt.Fprintf(&h, "a%d: &a%d {<<: [*a%d, *a%d]}\n", i, i, i-1, i-1)
	}
	writeRecord(t, root, "decisions/D-0001-example.md", h.String(), "# Example")
	store := loadWithin(t, root)
	if hasMalformed(store) {
		t.Fatal("a merge chain that resolves to one key was reported malformed")
	}
	got := store.ByID()["D-0001"].Extensions["a30"]
	if want := map[string]any{"project": "example"}; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("a30 = %v, want %v", got, want)
	}
}

// Keys that are not plain strings are not deduplicated, so each level doubles
// what it carries; the budget has to stop that rather than the memo.
func TestMergeExpansionBeyondBudgetIsMalformed(t *testing.T) {
	root, _ := newStore(t)
	var h strings.Builder
	h.WriteString("id: D-0001\nstatus: active\na0: &a0 {? [secret]: value}\n")
	for i := 1; i <= 30; i++ {
		fmt.Fprintf(&h, "a%d: &a%d {<<: [*a%d, *a%d]}\n", i, i, i-1, i-1)
	}
	h.WriteString("<<: *a30\n")
	writeRecord(t, root, "decisions/D-0001-example.md", h.String(), "# Example")
	store := loadWithin(t, root)
	for _, f := range brain.Check(store, brain.CheckPolicy{}, reference) {
		if f.Rule != "brain/record-malformed" {
			continue
		}
		if strings.Contains(f.Message, "secret") {
			t.Fatalf("finding quotes the header: %s", f.Message)
		}
		return
	}
	t.Fatal("a merge past the expansion budget was not reported malformed")
}

func TestAliasExpansionBeyondBudgetIsMalformed(t *testing.T) {
	root, _ := newStore(t)
	var h strings.Builder
	h.WriteString("id: D-0001\nstatus: provisional\nnote: kept\nb0: &b0 [x]\n")
	for i := 1; i <= 30; i++ {
		fmt.Fprintf(&h, "b%d: &b%d [*b%d, *b%d]\n", i, i, i-1, i-1)
	}
	writeRecord(t, root, "decisions/D-0001-example.md", h.String(), "# Example")
	store := loadWithin(t, root)
	if !hasMalformed(store) {
		t.Fatal("an alias chain past the expansion budget was not reported malformed")
	}
	if store.ByID()["D-0001"].Extensions["note"] != "kept" {
		t.Fatal("the rest of the record was lost")
	}
}
