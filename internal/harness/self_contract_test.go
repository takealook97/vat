package harness_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/takealook97/vat/internal/harness"
)

// vat generates the agent contracts it asks every other workspace to generate,
// and until this test nothing checked that its own were in step. The gap was
// not theoretical: `.codex/agents/*.toml` shipped `model = "opus"` — a name
// Codex cannot resolve — because a single Model field was rendered into both
// vendors' adapters. `vat lint` would have caught it in a governed workspace,
// but vat is one repository and runs no lint on itself.
//
// This is the seam that check covers. It reads the repository's own roles,
// renders what the adapters should be, and compares them to what is committed.
func TestVatsOwnAdaptersMatchItsOwnRoleDefinitions(t *testing.T) {
	// Arrange
	const repositoryRoot = "../.."
	roles, err := harness.LoadRoles(repositoryRoot)
	if err != nil {
		t.Fatalf("LoadRoles: %v", err)
	}
	if len(roles) == 0 {
		t.Fatal("no roles found; this repository defines its own and the path must be wrong")
	}

	// Act & Assert
	for _, role := range roles {
		if role.ModelIsAmbiguous() {
			t.Errorf("role %q names one model for several runtimes; split it into a models: map",
				role.Name)
		}
		for _, adapter := range harness.RenderAdapters(role) {
			path := filepath.Join(repositoryRoot, adapter.Path)
			committed, err := os.ReadFile(path)
			if err != nil {
				t.Errorf("role %q has no %s adapter at %s: %v", role.Name, adapter.Runtime, adapter.Path, err)
				continue
			}
			if string(committed) != adapter.Content {
				t.Errorf("%s is out of step with %s/%s.md; re-render it\n--- committed ---\n%s\n--- expected ---\n%s",
					adapter.Path, harness.RolesDir, role.Name, committed, adapter.Content)
			}
		}
	}
}

// A model name belongs to one vendor's namespace, so an adapter for one runtime
// must never name the model of another. This asserts it of the files actually
// committed, not of a fixture, because those are what a session loads.
func TestNoCommittedAdapterNamesAnotherRuntimesModel(t *testing.T) {
	// Arrange
	const repositoryRoot = "../.."
	roles, err := harness.LoadRoles(repositoryRoot)
	if err != nil {
		t.Fatalf("LoadRoles: %v", err)
	}

	// Act & Assert
	for _, role := range roles {
		for _, runtime := range role.TargetedRuntimes() {
			mine := role.ModelFor(runtime)
			for _, other := range role.TargetedRuntimes() {
				theirs := role.ModelFor(other)
				if other == runtime || theirs == "" || theirs == mine {
					continue
				}
				adapterPath := adapterPathFor(t, role, runtime)
				content, err := os.ReadFile(filepath.Join(repositoryRoot, adapterPath))
				if err != nil {
					t.Fatalf("read %s: %v", adapterPath, err)
				}
				if strings.Contains(string(content), theirs) {
					t.Errorf("%s names %q, which belongs to the %s runtime", adapterPath, theirs, other)
				}
			}
		}
	}
}

func adapterPathFor(t *testing.T, role harness.Role, runtime string) string {
	t.Helper()
	for _, adapter := range harness.RenderAdapters(role) {
		if adapter.Runtime == runtime {
			return adapter.Path
		}
	}
	t.Fatalf("role %q renders no %s adapter", role.Name, runtime)
	return ""
}
