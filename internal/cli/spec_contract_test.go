package cli

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/takealook97/vat/internal/brain"
	"github.com/takealook97/vat/internal/changeset"
	"github.com/takealook97/vat/internal/manifest"
)

// docs/SPEC.md is the normative half of the documentation: it exists so that
// somebody who is not this program can write something that reads a vat
// workspace. That promise is worth nothing if the document drifts from the code
// — a specification nobody checks is a description of a program that used to
// exist, and its §9 claims outright that this suite compares the two.
//
// So these tests read the specification and assert its enumerations against the
// packages that own them. A value the code accepts and the specification omits
// is a value no independent implementation would know to handle.

const specPath = "../../docs/SPEC.md"

func readSpec(t *testing.T) string {
	t.Helper()
	content, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("read %s: %v", specPath, err)
	}
	return string(content)
}

// specNames the values the specification lists for one enumeration, taken from
// the backticked tokens the document uses for every one of them.
func specMentions(spec, value string) bool {
	return strings.Contains(spec, "`"+value+"`")
}

func TestTheSpecificationListsEveryRepositoryRole(t *testing.T) {
	// Arrange
	spec := readSpec(t)

	// Act & Assert
	for _, role := range manifest.Roles() {
		if !specMentions(spec, string(role)) {
			t.Errorf("%s does not list the role %q; an implementation reading a manifest "+
				"would not know to accept it", specPath, role)
		}
	}
}

func TestTheSpecificationListsEveryRecordStatus(t *testing.T) {
	// Arrange
	spec := readSpec(t)

	// Act & Assert
	for _, status := range brain.Statuses() {
		if !specMentions(spec, string(status)) {
			t.Errorf("%s does not list the record status %q; §5.3 requires a reader to "+
				"report a status it does not understand, which it cannot do for one nobody wrote down",
				specPath, status)
		}
	}
}

func TestTheSpecificationListsEveryClaimKind(t *testing.T) {
	// Arrange
	spec := readSpec(t)

	// Act & Assert
	for _, kind := range strings.Split(brain.ClaimKinds(), ", ") {
		if !specMentions(spec, kind) {
			t.Errorf("%s does not list the claim kind %q; §5.4 is the rule that decides "+
				"whether a claim expires, and it cannot apply to a kind it never names",
				specPath, kind)
		}
	}
}

func TestTheSpecificationListsEveryChangesetStatus(t *testing.T) {
	// Arrange
	spec := readSpec(t)
	statuses := []changeset.Status{
		changeset.StatusOpen, changeset.StatusVerified, changeset.StatusClosed,
		changeset.StatusRolledBack, changeset.StatusAbandoned,
	}

	// Act & Assert
	for _, status := range statuses {
		if !specMentions(spec, string(status)) {
			t.Errorf("%s does not list the changeset status %q", specPath, status)
		}
	}
}

// The version numbers are the whole basis on which a reader decides whether it
// understands a file. A specification that names a version the code does not
// write is worse than one that names none.
func TestTheSpecificationQuotesTheVersionsTheCodeWrites(t *testing.T) {
	// Arrange
	spec := readSpec(t)

	// Act & Assert
	if want := fmt.Sprintf("| brain | %d |", brain.SchemaVersion); !strings.Contains(spec, want) {
		t.Errorf("%s does not record the brain schema as %d; the table in §8 must match the code",
			specPath, brain.SchemaVersion)
	}
	if want := fmt.Sprintf("| manifest | %d |", manifest.SchemaVersion); !strings.Contains(spec, want) {
		t.Errorf("%s does not record the manifest version as %d", specPath, manifest.SchemaVersion)
	}
}

// Every record directory has to be named, or an implementation walks past a
// whole class of records without knowing it exists.
func TestTheSpecificationNamesEveryRecordDirectory(t *testing.T) {
	// Arrange
	spec := readSpec(t)

	// Act & Assert
	for _, kind := range brain.Kinds() {
		if !strings.Contains(spec, kind.Dir()+"/") {
			t.Errorf("%s does not name the %s directory", specPath, kind.Dir())
		}
	}
}
