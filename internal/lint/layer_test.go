package lint_test

import (
	"strings"
	"testing"

	"github.com/takealook97/vat/internal/brain"
	"github.com/takealook97/vat/internal/manifest"
)

// A workspace ran the knowledge layer with 53 records awaiting promotion, a
// third of everything it held, and nothing ever said so — because its workspace
// checks never ran `vat brain check`. Another workspace had wired exactly that
// and stayed clean. The difference was not discipline; it was that nobody had
// told the first workspace's owner to do it, and nothing could.
//
// Adopting a layer and never checking it is a state vat can see, which is the
// only test for whether something belongs here.

func TestABrainNobodyChecksIsReported(t *testing.T) {
	// Arrange
	ws, root := withBrain(t, manifest.Repo{
		Name: "payments", Origin: "https://example.invalid/acme/payments.git",
		Role: manifest.RoleProduct, Checks: []string{"make check"},
	})
	if _, err := brain.Init(root, reference); err != nil {
		t.Fatalf("brain init: %v", err)
	}
	ws.Manifest.Workspace.Checks = []string{"vat lint"}

	// Act
	finding, found := rules(run(t, ws))["workspace/layer-unchecked"]

	// Assert
	if !found {
		t.Fatal("a workspace running the knowledge layer with nothing checking it was reported as clean")
	}
	if !strings.Contains(finding.Fix, "vat brain check") {
		t.Errorf("fix = %q; it does not name the check that is missing", finding.Fix)
	}
}

func TestABrainThatIsCheckedIsNotReported(t *testing.T) {
	// Arrange: the other side. A rule that fires on a workspace doing the right
	// thing teaches people to ignore the rule.
	ws, root := withBrain(t, manifest.Repo{
		Name: "payments", Origin: "https://example.invalid/acme/payments.git",
		Role: manifest.RoleProduct, Checks: []string{"make check"},
	})
	if _, err := brain.Init(root, reference); err != nil {
		t.Fatalf("brain init: %v", err)
	}
	ws.Manifest.Workspace.Checks = []string{"vat lint", "vat brain check"}

	// Act
	_, found := rules(run(t, ws))["workspace/layer-unchecked"]

	// Assert
	if found {
		t.Error("a workspace that checks its knowledge layer was reported for not checking it")
	}
}

func TestAWorkspaceWithNoBrainIsNotAskedToCheckOne(t *testing.T) {
	// Arrange: a layer nobody adopted costs nothing and is asked for nothing.
	ws := fixture(t, manifest.Repo{
		Name: "payments", Origin: "https://example.invalid/acme/payments.git",
		Role: manifest.RoleProduct, Checks: []string{"make check"},
	})
	ws.Manifest.Workspace.Checks = []string{"vat lint"}

	// Act
	_, found := rules(run(t, ws))["workspace/layer-unchecked"]

	// Assert
	if found {
		t.Error("a workspace that never adopted the knowledge layer was told to check it")
	}
}
