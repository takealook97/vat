package manifest_test

import (
	"strings"
	"testing"

	"github.com/takealook97/vat/internal/manifest"
)

// A schema version says which file format vat is reading. It says nothing
// about which commands exist, what they print, or what they refuse to write —
// and those are what a workspace actually depends on. `requires.vat` is where
// that dependency is declared.

func TestAConstraintAcceptsTheVersionsInsideItsRange(t *testing.T) {
	// Arrange
	cases := []struct {
		constraint string
		version    string
		want       bool
	}{
		{">=0.3.0", "0.3.0", true},
		{">=0.3.0", "0.2.9", false},
		{">=0.3.0", "1.0.0", true},
		{">=0.3.0 <0.4.0", "0.3.7", true},
		{">=0.3.0 <0.4.0", "0.4.0", false},
		{">=0.3.0", "v0.3.1", true},
		{"=0.3.0", "0.3.0", true},
		{"=0.3.0", "0.3.1", false},
		{">0.3.0", "0.3.0", false},
		{"<=0.3.0", "0.3.0", true},
		// A prerelease is not the release it leads to. Treating v0.3.0-rc1 as
		// 0.3.0 would let a release candidate satisfy a floor written for the
		// finished version.
		{">=0.3.0", "0.3.0-rc1", false},
		{">=0.3.0-rc1", "0.3.0", true},
		// Build metadata is not part of the ordering.
		{">=0.3.0", "0.3.0+darwin", true},
		// What `git describe` produces, which is what every build between two
		// releases calls itself. "4 commits after v0.3.0" is not a candidate
		// for 0.3.0: it has everything 0.3.0 has and more, and reading it as a
		// prerelease would refuse the exact build somebody is dogfooding.
		{">=0.3.0", "v0.3.0-4-g2ad652e", true},
		{">=0.3.0", "v0.3.0-4-g2ad652e-dirty", true},
		{">=0.3.0", "v0.3.0-dirty", true},
		{">=0.3.0", "v0.2.9-4-g2ad652e", false},
		{"<0.4.0", "v0.3.0-4-g2ad652e", true},
	}

	for _, c := range cases {
		constraint, err := manifest.ParseConstraint(c.constraint)
		if err != nil {
			t.Fatalf("ParseConstraint(%q) returned an error: %v", c.constraint, err)
		}

		// Act
		got, err := constraint.Allows(c.version)

		// Assert
		if err != nil {
			t.Fatalf("Allows(%q) against %q returned an error: %v", c.version, c.constraint, err)
		}
		if got != c.want {
			t.Errorf("%q allows %q = %v, want %v", c.constraint, c.version, got, c.want)
		}
	}
}

func TestAConstraintThisFormatDoesNotHaveIsRefusedWhereItIsWritten(t *testing.T) {
	// Arrange: caret and tilde ranges are npm's, not this format's. Accepting
	// the text and ignoring the meaning is how a workspace ends up believing it
	// pinned something.
	for _, constraint := range []string{"^0.3.0", "~0.3.0", "0.3.0", ">=", ">=x.y.z", ">= 0.3.0 || <0.2"} {
		// Act
		_, err := manifest.ParseConstraint(constraint)

		// Assert
		if err == nil {
			t.Errorf("ParseConstraint(%q) accepted a constraint this format does not have", constraint)
		}
	}
}

func TestAManifestRequiringANewerVatIsRefusedWithBothVersionsNamed(t *testing.T) {
	// Arrange
	m := manifest.Manifest{
		Version:   1,
		Workspace: manifest.Workspace{Name: "acme", DefaultBranch: "main"},
		Requires:  manifest.Requires{Vat: ">=0.3.0"},
	}

	// Act
	err := manifest.CheckToolVersion(m, "0.2.1")

	// Assert
	if err == nil {
		t.Fatal("a workspace requiring a newer vat was accepted by an older one")
	}
	for _, want := range []string{">=0.3.0", "0.2.1"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, missing %q; a reader cannot act on a version it is not told",
				err, want)
		}
	}
}

func TestAToolVersionThatCannotBeReadIsNotTreatedAsAViolation(t *testing.T) {
	// Arrange: a `go build` binary calls itself "dev". Refusing to open a
	// workspace with one would break every contributor's checkout to enforce a
	// constraint that cannot be evaluated either way.
	m := manifest.Manifest{
		Version:   1,
		Workspace: manifest.Workspace{Name: "acme", DefaultBranch: "main"},
		Requires:  manifest.Requires{Vat: ">=0.3.0"},
	}

	// Act
	err := manifest.CheckToolVersion(m, "dev")

	// Assert
	if err != nil {
		t.Errorf("an unstamped build was refused: %v", err)
	}
}

func TestAManifestWithNoRequirementAcceptsAnyBuild(t *testing.T) {
	// Arrange
	m := manifest.Manifest{
		Version:   1,
		Workspace: manifest.Workspace{Name: "acme", DefaultBranch: "main"},
	}

	// Act
	err := manifest.CheckToolVersion(m, "0.1.0")

	// Assert
	if err != nil {
		t.Errorf("a workspace declaring no requirement was refused: %v", err)
	}
}

func TestAnUnreadableRequirementIsRefusedWhenTheManifestIsValidated(t *testing.T) {
	// Arrange: the constraint is checked where it is written, not only on the
	// machine whose version happens to fail it. A typo that silently allows
	// every version is the one outcome this field must never have.
	data := []byte("version: 1\nworkspace:\n  name: acme\nrequires:\n  vat: \"^0.3.0\"\n")

	// Act
	_, err := manifest.Parse(data)

	// Assert
	if err == nil {
		t.Fatal("a constraint this format cannot express was accepted")
	}
	if !strings.Contains(err.Error(), "requires.vat") {
		t.Errorf("error = %q, want it to name the field", err)
	}
}
