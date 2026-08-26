package harness_test

import (
	"testing"

	"github.com/takealook97/vat/internal/harness"
	"github.com/takealook97/vat/internal/manifest"
)

// RepoNames feeds the generated regions and the .gitignore exclusion. Sorting
// is the point: an unsorted list makes every render produce a different file
// and turns drift detection into noise.
func TestRepoNamesAreSortedAndUseTheDirectoryNotTheName(t *testing.T) {
	// Arrange: one repository whose directory differs from its name, so a
	// version that used Name would be caught.
	m := manifest.Default("acme")
	m = manifest.WithRepo(m, manifest.Repo{
		Name: "notes", Origin: "https://example.invalid/acme/notes.git", Role: manifest.RoleDocs})
	m = manifest.WithRepo(m, manifest.Repo{
		Name: "payments", Origin: "https://example.invalid/acme/payments.git",
		Role: manifest.RoleProduct, Path: "apps/billing"})
	m = manifest.WithRepo(m, manifest.Repo{
		Name: "identity", Origin: "https://example.invalid/acme/identity.git", Role: manifest.RoleProduct})

	// Act
	names := harness.RepoNames(m)

	// Assert
	want := []string{"apps/billing", "identity", "notes"}
	if len(names) != len(want) {
		t.Fatalf("RepoNames = %v, want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Errorf("RepoNames[%d] = %q, want %q (full: %v)", i, names[i], want[i], names)
		}
	}
}
