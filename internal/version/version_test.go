package version_test

import (
	"strings"
	"testing"

	"github.com/takealook97/vat/internal/version"
)

func TestShortFallsBackWhenNothingWasStamped(t *testing.T) {
	// Arrange: a `go install` build carries no linker stamps. Reporting an
	// empty version would make the command useless for its one job.
	got := version.Short()

	// Assert
	if strings.TrimSpace(got) == "" {
		t.Error("Short() returned an empty version")
	}
}

func TestLongNamesTheToolAndThePlatform(t *testing.T) {
	// Act
	got := version.Long()

	// Assert
	if !strings.HasPrefix(got, "vat ") {
		t.Errorf("Long() = %q, want it to start with the tool name", got)
	}
	for _, want := range []string{"commit", "built", "go1."} {
		if !strings.Contains(got, want) {
			t.Errorf("Long() = %q, missing %q", got, want)
		}
	}
}

func TestLongNeverReportsAnEmptyField(t *testing.T) {
	// Arrange: an unstamped build must still print something for every field
	// rather than a gap that reads as corruption.
	got := version.Long()

	// Assert
	if strings.Contains(got, "commit ,") || strings.Contains(got, "built ,") {
		t.Errorf("Long() left a field empty: %q", got)
	}
}
