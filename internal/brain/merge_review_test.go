package brain_test

import (
	"testing"

	"github.com/takealook97/vat/internal/brain"
	"github.com/takealook97/vat/internal/frontmatter"
)

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
