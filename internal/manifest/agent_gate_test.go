package manifest_test

import (
	"testing"

	"github.com/takealook97/vat/internal/manifest"
)

func TestAgentGateIsLimitedToBrainPromotion(t *testing.T) {
	m := manifest.Default("fixture")
	m.Policy.Gates.BrainPromote = manifest.GateAgent
	if err := manifest.Validate(m); err != nil {
		t.Fatalf("brain agent gate refused: %v", err)
	}
	m.Policy.Gates.Deploy = manifest.GateAgent
	if err := manifest.Validate(m); err == nil {
		t.Fatal("agent deploy gate accepted")
	}
	m.Policy.Gates.Deploy = manifest.GateManual
	m.Policy.Gates.ExternalWrite = manifest.GateAgent
	if err := manifest.Validate(m); err == nil {
		t.Fatal("agent external write gate accepted")
	}
}
