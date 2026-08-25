package evidence_test

import (
	"strings"
	"testing"
	"time"

	"github.com/takealook97/vat/internal/evidence"
)

var reference = time.Date(2026, 8, 25, 0, 0, 0, 0, time.UTC)

func TestReleaseAuthorityIsFalseUnlessItIsGranted(t *testing.T) {
	// Arrange & Act
	packet := evidence.New("EP-001", "Do a thing", []string{"payments"}, reference)

	// Assert
	if packet.ReleaseAuthority {
		t.Error("a new packet granted release authority by default")
	}
}

func TestValidateNamesWhatWouldMakeCompletionUncheckable(t *testing.T) {
	// Arrange
	packet := evidence.New("EP-001", "", nil, reference)

	// Act
	problems := strings.Join(evidence.Validate(packet), "\n")

	// Assert
	for _, want := range []string{"objective is empty", "write boundary", "acceptance criterion", "canonical checks"} {
		if !strings.Contains(problems, want) {
			t.Errorf("Validate omits %q:\n%s", want, problems)
		}
	}
}

func TestSaveAndLoadRoundTripAPacket(t *testing.T) {
	// Arrange
	root := t.TempDir()
	packet := evidence.New("EP-001", "Add idempotency keys", []string{"payments"}, reference)
	packet.Acceptance = []string{"a repeated request creates one charge"}
	packet.CanonicalChecks = []string{"make check"}
	packet.RollbackPoints = map[string]string{"payments": "abcdef123456"}

	// Act
	if err := evidence.Save(root, packet); err != nil {
		t.Fatalf("Save returned an error: %v", err)
	}
	loaded, err := evidence.Load(root, "EP-001")

	// Assert
	if err != nil {
		t.Fatalf("Load returned an error: %v", err)
	}
	if loaded.Objective != packet.Objective {
		t.Errorf("objective = %q, want %q", loaded.Objective, packet.Objective)
	}
	if loaded.RollbackPoints["payments"] != "abcdef123456" {
		t.Errorf("return points lost in round trip: %v", loaded.RollbackPoints)
	}
	if len(evidence.Validate(loaded)) != 0 {
		t.Errorf("a complete packet reported problems: %v", evidence.Validate(loaded))
	}
}

func TestMarkdownStatesTheWriteBoundaryAndWithholdsRelease(t *testing.T) {
	// Arrange
	packet := evidence.New("EP-001", "Do a thing", []string{"payments"}, reference)
	packet.Acceptance = []string{"the thing happens"}

	// Act
	briefing := evidence.Markdown(packet)

	// Assert
	for _, want := range []string{"Write only inside", "payments", "No release authority", "not evidence"} {
		if !strings.Contains(briefing, want) {
			t.Errorf("the briefing omits %q", want)
		}
	}
}
