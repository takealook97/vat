package harness_test

import (
	"strings"
	"testing"

	"github.com/takealook97/vat/internal/harness"
	"github.com/takealook97/vat/internal/manifest"
)

func TestHarnessRendersAgentPromotionGate(t *testing.T) {
	m := demoManifest()
	m.Policy.Gates.BrainPromote = manifest.GateAgent
	rendered := harness.RenderWorkspace(m)
	if !strings.Contains(rendered, "| Promote a claim to canonical | agent promotion under mechanical evidence conditions |") {
		t.Fatalf("agent gate not described: %s", rendered)
	}
	if !strings.Contains(rendered, "| Deploy | **explicit human approval required** |") {
		t.Fatal("deploy gate changed")
	}
}
