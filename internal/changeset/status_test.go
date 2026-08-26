package changeset_test

import (
	"testing"

	"github.com/takealook97/vat/internal/changeset"
)

// Status.Open decides which changesets `vat lint` chases and which it leaves
// alone. A status wrongly reported as closed is a multi-repository change that
// nothing ever asks about again.
func TestOnlyUnfinishedStatusesCountAsOpen(t *testing.T) {
	// Arrange & Act & Assert
	open := []changeset.Status{changeset.StatusOpen, changeset.StatusVerified}
	closed := []changeset.Status{
		changeset.StatusClosed, changeset.StatusRolledBack, changeset.StatusAbandoned, "",
	}

	for _, status := range open {
		if !status.Open() {
			t.Errorf("%q is unfinished work but Open() said otherwise", status)
		}
	}
	for _, status := range closed {
		if status.Open() {
			t.Errorf("%q is finished but Open() reported it as unfinished", status)
		}
	}
}

func TestParticipantIsFoundByNameAndAbsenceIsReported(t *testing.T) {
	// Arrange: "absent" and "present but empty" are different answers, and a
	// caller recording a check result must not conflate them.
	set := changeset.Changeset{
		ID: "CS-0001",
		Repositories: []changeset.Participant{
			{Name: "payments"},
			{Name: "identity"},
		},
	}

	// Act
	found, ok := set.Participant("identity")
	_, missing := set.Participant("notes")

	// Assert
	if !ok {
		t.Fatal("an enrolled repository was not found")
	}
	if found.Name != "identity" {
		t.Errorf("Participant returned %q, want identity", found.Name)
	}
	if missing {
		t.Error("a repository that was never enrolled was reported as found")
	}
}
