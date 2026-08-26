package manifest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The manifest is the one file every other package reads the workspace shape
// from, and every update to it goes through a function that returns a new value.
// A helper that mutated its argument would corrupt a caller still holding the
// old manifest to compare against — which is what the commands that regenerate
// contracts do.

func sampleManifest() Manifest {
	m := Default("acme")
	m = WithRepo(m, Repo{
		Name: "payments", Origin: "https://example.invalid/acme/payments.git",
		Role: RoleProduct, Group: "backend", Required: true,
	})
	m = WithRepo(m, Repo{
		Name: "console", Origin: "https://example.invalid/acme/console.git",
		Role: RoleProduct, Group: "frontend",
	})
	m = WithRepo(m, Repo{
		Name: "knowledge", Origin: "https://example.invalid/acme/knowledge.git",
		Role: RoleBrain,
	})
	return m
}

func TestWithRepoLeavesTheManifestItWasGivenUntouched(t *testing.T) {
	// Arrange
	original := sampleManifest()
	before := len(original.Repos)

	// Act
	updated := WithRepo(original, Repo{
		Name: "docs", Origin: "https://example.invalid/acme/docs.git", Role: RoleDocs,
	})

	// Assert
	if len(original.Repos) != before {
		t.Errorf("the original manifest grew to %d repositories; WithRepo mutated its argument",
			len(original.Repos))
	}
	if len(updated.Repos) != before+1 {
		t.Errorf("the returned manifest holds %d repositories, want %d", len(updated.Repos), before+1)
	}
}

func TestWithRepoReplacesAnEntryRatherThanDuplicatingIt(t *testing.T) {
	// Arrange: two entries with the same name would make every lookup depend on
	// iteration order.
	original := sampleManifest()

	// Act
	updated := WithRepo(original, Repo{
		Name: "payments", Origin: "https://example.invalid/acme/payments.git",
		Role: RoleProduct, Group: "platform",
	})

	// Assert
	seen := 0
	for _, repo := range updated.Repos {
		if repo.Name == "payments" {
			seen++
			if repo.Group != "platform" {
				t.Errorf("the replacement kept the old group %q", repo.Group)
			}
		}
	}
	if seen != 1 {
		t.Errorf("payments appears %d times after replacement, want once", seen)
	}
	if found, _ := original.Find("payments"); found.Group != "backend" {
		t.Errorf("the original entry was rewritten to group %q", found.Group)
	}
}

func TestWithoutRepoReportsWhetherItRemovedAnything(t *testing.T) {
	// Arrange: a silent no-op would let `repo remove typo` report success.
	original := sampleManifest()

	// Act
	reduced, removed := WithoutRepo(original, "console")
	unchanged, missing := WithoutRepo(original, "never-enrolled")

	// Assert
	if !removed {
		t.Error("removing an enrolled repository reported that nothing was removed")
	}
	if _, exists := reduced.Find("console"); exists {
		t.Error("console survived its own removal")
	}
	if missing {
		t.Error("removing a repository that was never enrolled reported success")
	}
	if len(unchanged.Repos) != len(original.Repos) {
		t.Error("a failed removal changed the repository list anyway")
	}
	if _, exists := original.Find("console"); !exists {
		t.Error("WithoutRepo mutated the manifest it was given")
	}
}

func TestSaveAndLoadRoundTripEveryFieldThatWasSet(t *testing.T) {
	// Arrange: anything the schema accepts but does not survive a write is a
	// field that silently resets the next time any command touches the file.
	path := filepath.Join(t.TempDir(), FileName)
	original := sampleManifest()

	// Act
	if err := Save(path, original); err != nil {
		t.Fatalf("save: %v", err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	// Assert
	if loaded.Workspace.Name != original.Workspace.Name {
		t.Errorf("workspace name became %q", loaded.Workspace.Name)
	}
	if len(loaded.Repos) != len(original.Repos) {
		t.Fatalf("loaded %d repositories, wrote %d", len(loaded.Repos), len(original.Repos))
	}
	for _, want := range original.Repos {
		got, exists := loaded.Find(want.Name)
		if !exists {
			t.Errorf("%s did not survive the round trip", want.Name)
			continue
		}
		if got.Origin != want.Origin || got.Role != want.Role || got.Group != want.Group {
			t.Errorf("%s came back as %+v, wrote %+v", want.Name, got, want)
		}
	}
}

func TestLoadReportsAManifestThatIsNotThere(t *testing.T) {
	// Arrange: "no workspace here" is a different answer from "this workspace is
	// broken", and the commands print different guidance for each.
	path := filepath.Join(t.TempDir(), FileName)

	// Act
	_, err := Load(path)

	// Assert
	if err == nil {
		t.Fatal("loading a manifest that does not exist succeeded")
	}
}

func TestLoadRejectsAFileWithAKeyTheSchemaDoesNotDefine(t *testing.T) {
	// Arrange: a typo in a policy key would otherwise be read as "unset" and the
	// policy would quietly be the default.
	path := filepath.Join(t.TempDir(), FileName)
	body := "schema_version: 1\nworkspace:\n  name: acme\n  typoed_key: true\nrepos: []\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Act
	_, err := Load(path)

	// Assert
	if err == nil {
		t.Error("a manifest with an undefined key loaded without complaint")
	}
}

func TestFindAndBrainRepoLocateEntriesByTheirDeclaredRole(t *testing.T) {
	// Arrange
	m := sampleManifest()

	// Act
	brain, hasBrain := m.BrainRepo()
	_, hasMissing := m.Find("never-enrolled")

	// Assert
	if !hasBrain {
		t.Fatal("a manifest holding a brain repository reports none")
	}
	if brain.Name != "knowledge" {
		t.Errorf("BrainRepo returned %q, want knowledge", brain.Name)
	}
	if hasMissing {
		t.Error("Find reported a repository that was never enrolled")
	}
}

func TestGroupsReportsEveryGroupExactlyOnce(t *testing.T) {
	// Arrange: the list drives shell completion, so a duplicate is visible.
	m := sampleManifest()

	// Act
	groups := m.Groups()

	// Assert
	seen := map[string]int{}
	for _, group := range groups {
		seen[group]++
	}
	for _, expected := range []string{"backend", "frontend"} {
		if seen[expected] != 1 {
			t.Errorf("group %q appears %d times in %v, want once", expected, seen[expected], groups)
		}
	}
	if seen[""] > 0 {
		t.Errorf("the empty group is listed as a real one: %v", groups)
	}
}

func TestSelectFailsRatherThanMatchingNothingSilently(t *testing.T) {
	// Arrange: an empty run in CI is a green build that did nothing, which is
	// the most expensive way for a filter typo to be wrong.
	m := sampleManifest()

	cases := []struct {
		name     string
		selector Selector
	}{
		{"unknown group", Selector{Groups: []string{"no-such-group"}}},
		{"unknown name", Selector{Names: []string{"no-such-repo"}}},
		{"unknown role", Selector{Roles: []string{"no-such-role"}}},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			// Act
			_, err := m.Select(testCase.selector)

			// Assert
			if err == nil {
				t.Errorf("selecting %+v matched nothing and reported success", testCase.selector)
			}
		})
	}
}

func TestSelectNarrowsToTheRequestedGroup(t *testing.T) {
	// Arrange
	m := sampleManifest()

	// Act
	selected, err := m.Select(Selector{Groups: []string{"backend"}})

	// Assert
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if len(selected) != 1 || selected[0].Name != "payments" {
		t.Errorf("selecting group backend returned %+v, want only payments", selected)
	}
}

func TestArchivedRepositoriesAreLeftOutUnlessAskedFor(t *testing.T) {
	// Arrange: an archived repository is still governed, but it is not part of
	// the daily working set, and including it would make every status noisy.
	m := WithRepo(sampleManifest(), Repo{
		Name: "legacy-api", Origin: "https://example.invalid/acme/legacy-api.git",
		Role: RoleProduct, Archived: true,
	})

	// Act
	everyday := m.Active()
	withArchive, err := m.Select(Selector{IncludeArchive: true})
	if err != nil {
		t.Fatalf("select: %v", err)
	}

	// Assert
	for _, repo := range everyday {
		if repo.Name == "legacy-api" {
			t.Error("an archived repository turned up in the everyday working set")
		}
	}
	found := false
	for _, repo := range withArchive {
		if repo.Name == "legacy-api" {
			found = true
		}
	}
	if !found {
		t.Error("--archived did not bring the archived repository back")
	}
}

func TestRoleNamesOffersEveryRoleTheSchemaAccepts(t *testing.T) {
	// Arrange: the string is what an error message offers someone who mistyped a
	// role, so a role missing from it is a role nobody is told about.

	// Act
	listed := RoleNames()

	// Assert
	if listed == "" {
		t.Fatal("RoleNames returned nothing")
	}
	for _, role := range Roles() {
		if !strings.Contains(listed, string(role)) {
			t.Errorf("role %q is accepted but never offered in %q", role, listed)
		}
	}
}
