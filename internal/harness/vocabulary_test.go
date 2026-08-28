package harness_test

import (
	"strings"
	"testing"

	"github.com/takealook97/vat/internal/harness"
	"github.com/takealook97/vat/internal/manifest"
)

// A company that calls a bundle of repositories a "project" ends up with two
// vocabularies in one team: its own, and the tool's. The generated contracts are
// what its people and its agents read, so that is where the organisation's word
// belongs — and nowhere else, because vat.yaml is a published format other tools
// read by name.

func vocabularyManifest(t *testing.T, nouns manifest.Vocabulary) manifest.Manifest {
	t.Helper()
	return manifest.Manifest{
		Version: 1,
		Workspace: manifest.Workspace{
			Name: "payments", DefaultBranch: "main", Vocabulary: nouns,
		},
		Repos: []manifest.Repo{{
			Name: "payments-api", Origin: "https://example.invalid/acme/payments-api.git",
			Role: manifest.RoleProduct,
		}},
	}
}

func TestGeneratedContractsUseTheOrganisationsOwnNoun(t *testing.T) {
	// Arrange
	m := vocabularyManifest(t, manifest.Vocabulary{Workspace: "project"})

	// Act
	workspace := harness.RenderWorkspace(m)
	repo := harness.RenderRepo(m, m.Repos[0])

	// Assert
	if !strings.Contains(repo, "in the `payments` project") {
		t.Errorf("the repository contract still calls it a workspace:\n%s", repo)
	}
	if !strings.Contains(workspace, "## Project") {
		t.Errorf("the heading was not renamed, or was renamed without its capital:\n%s", workspace)
	}
	if strings.Contains(repo, "workspace, governed by") {
		t.Errorf("the old noun survives in the repository contract:\n%s", repo)
	}
}

func TestTheDefaultNounsAreTheOnesVatHasAlwaysUsed(t *testing.T) {
	// Arrange: an organisation that declares nothing must get exactly what it
	// got before this field existed, or every workspace on earth reports harness
	// drift on upgrade.
	m := vocabularyManifest(t, manifest.Vocabulary{})

	// Act
	workspace := harness.RenderWorkspace(m)
	repo := harness.RenderRepo(m, m.Repos[0])

	// Assert
	if !strings.Contains(workspace, "## Workspace") {
		t.Errorf("the default heading changed:\n%s", workspace)
	}
	if !strings.Contains(repo, "in the `payments` workspace") {
		t.Errorf("the default sentence changed:\n%s", repo)
	}
}

func TestRenamingTheNounNeverReachesACommandOrARole(t *testing.T) {
	// Arrange: the machine-facing vocabulary is one vocabulary. vat.yaml is a
	// published format, and a role or a command that means something different
	// per organisation is a format that has forked.
	m := vocabularyManifest(t, manifest.Vocabulary{Workspace: "project", Brain: "wiki"})
	m.Repos = append(m.Repos, manifest.Repo{
		Name: "knowledge", Origin: "https://example.invalid/acme/knowledge.git",
		Role: manifest.RoleBrain,
	})

	// Act
	rendered := harness.RenderWorkspace(m)

	// Assert
	for _, unchanged := range []string{"vat brain query", "role"} {
		if strings.Contains(rendered, "vat project") || strings.Contains(rendered, "vat wiki") {
			t.Fatalf("a command name was renamed:\n%s", rendered)
		}
		_ = unchanged
	}
}
