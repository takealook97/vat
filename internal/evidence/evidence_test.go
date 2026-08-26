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

func TestListReturnsPacketsNewestFirst(t *testing.T) {
	// Arrange
	root := t.TempDir()
	for _, id := range []string{"EP-001", "EP-003", "EP-002"} {
		packet := evidence.New(id, "objective "+id, []string{"payments"}, reference)
		if err := evidence.Save(root, packet); err != nil {
			t.Fatalf("Save returned an error: %v", err)
		}
	}

	// Act
	packets, err := evidence.List(root)

	// Assert
	if err != nil {
		t.Fatalf("List returned an error: %v", err)
	}
	want := []string{"EP-003", "EP-002", "EP-001"}
	if len(packets) != len(want) {
		t.Fatalf("packet count = %d, want %d", len(packets), len(want))
	}
	for i, id := range want {
		if packets[i].ID != id {
			t.Errorf("packets[%d] = %s, want %s", i, packets[i].ID, id)
		}
	}
}

func TestListOnAWorkspaceWithNoPacketsIsEmpty(t *testing.T) {
	// Act
	packets, err := evidence.List(t.TempDir())

	// Assert
	if err != nil {
		t.Fatalf("List returned an error: %v", err)
	}
	if len(packets) != 0 {
		t.Errorf("packets = %+v, want none", packets)
	}
}

func TestLoadReportsAMissingPacketRatherThanReturningAnEmptyOne(t *testing.T) {
	// Act
	_, err := evidence.Load(t.TempDir(), "EP-404")

	// Assert
	if err == nil {
		t.Fatal("Load invented a packet that does not exist")
	}
	if !strings.Contains(err.Error(), "EP-404") {
		t.Errorf("the error does not name the packet: %v", err)
	}
}

func TestValidateReportsARepositoryWithNoReturnPoint(t *testing.T) {
	// Arrange
	packet := evidence.New("EP-001", "Do a thing", []string{"payments", "console"}, reference)
	packet.Acceptance = []string{"the thing happens"}
	packet.CanonicalChecks = []string{"make check"}
	packet.RollbackPoints = map[string]string{"payments": "abcdef123456"}

	// Act
	problems := strings.Join(evidence.Validate(packet), "\n")

	// Assert
	if !strings.Contains(problems, "console") {
		t.Errorf("the repository with no return point was not named: %q", problems)
	}
	if strings.Contains(problems, "payments") {
		t.Errorf("a repository that has a return point was reported: %q", problems)
	}
}

func TestMarkdownAnnouncesReleaseAuthorityWhenItIsGranted(t *testing.T) {
	// Arrange
	packet := evidence.New("EP-001", "Ship it", []string{"payments"}, reference)
	packet.Acceptance = []string{"it is live"}
	packet.ReleaseAuthority = true

	// Act
	briefing := evidence.Markdown(packet)

	// Assert
	if !strings.Contains(briefing, "Release is authorised") {
		t.Errorf("the briefing does not state that release is authorised:\n%s", briefing)
	}
	if strings.Contains(briefing, "No release authority") {
		t.Errorf("the briefing contradicts itself:\n%s", briefing)
	}
}

func TestMarkdownListsNonGoalsAndContracts(t *testing.T) {
	// Arrange: stating what is out of scope is what stops scope expanding
	// silently.
	packet := evidence.New("EP-001", "Do a thing", []string{"payments"}, reference)
	packet.Acceptance = []string{"the thing happens"}
	packet.NonGoals = []string{"changing refund timing"}
	packet.Contracts = []string{"POST /orders/{id}/cancel"}
	packet.RollbackPoints = map[string]string{"payments": "abcdef123456"}

	// Act
	briefing := evidence.Markdown(packet)

	// Assert
	for _, want := range []string{"Not in scope", "changing refund timing", "Contracts to honour", "abcdef123456"} {
		if !strings.Contains(briefing, want) {
			t.Errorf("the briefing omits %q", want)
		}
	}
}

func TestPathIsStableForAnIdentifier(t *testing.T) {
	// Act & Assert
	if got := evidence.Path("EP-001"); !strings.HasSuffix(got, "EP-001.yaml") {
		t.Errorf("Path = %q, want it to end with EP-001.yaml", got)
	}
}

func TestAnIdentifierThatCouldBecomeAPathIsRefused(t *testing.T) {
	// Arrange: the id is chosen by the caller and pasted into a filename. An
	// unchecked "../escape" wrote the packet where nothing lists it, and a
	// longer traversal left the workspace entirely.
	refused := []string{"", "   ", "../../../pwned", "nested/id", ".hidden"}
	accepted := []string{"EV-0001", "release_gate", "ev.1", "a"}

	// Act & Assert
	for _, id := range refused {
		if err := evidence.ValidateID(id); err == nil {
			t.Errorf("ValidateID(%q) accepted an id that becomes a path", id)
		}
	}
	for _, id := range accepted {
		if err := evidence.ValidateID(id); err != nil {
			t.Errorf("ValidateID(%q) refused an ordinary identifier: %v", id, err)
		}
	}
}

func TestSaveAndLoadRefuseAnIdentifierThatCouldBecomeAPath(t *testing.T) {
	// Arrange: the package, not the command, is where this has to be enforced.
	root := t.TempDir()

	// Act & Assert
	if err := evidence.Save(root, evidence.Packet{ID: "../../../pwned", Objective: "x"}); err == nil {
		t.Error("Save wrote a packet to a caller-chosen path")
	}
	if _, err := evidence.Load(root, "../../../pwned"); err == nil {
		t.Error("Load read from a caller-chosen path")
	}
}
