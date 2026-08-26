package version_test

import (
	"runtime"
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

// A binary built with `go install` carries none of the linker stamps, and every
// accessor is then supposed to fall back to what the Go toolchain recorded.
// Those fallbacks are the paths a user hits when they did not build from a
// release, so they are the ones most worth pinning: a released binary at least
// has someone watching the release job.
func TestEveryAccessorFallsBackWhenNothingWasStampedIn(t *testing.T) {
	// Arrange
	stamped := [3]string{version.Version, version.Commit, version.Date}
	t.Cleanup(func() {
		version.Version, version.Commit, version.Date = stamped[0], stamped[1], stamped[2]
	})
	version.Version, version.Commit, version.Date = "", "", ""

	// Act & Assert: Short never returns an empty string, because it is printed
	// in `vat version` and in every bug report pasted from it.
	if short := version.Short(); short == "" {
		t.Error("Short() is empty with nothing stamped in")
	}
	// Revision and BuildDate may legitimately be empty under `go test`, which
	// records no vcs settings. What must not happen is a panic or a stale read
	// of the cleared stamp.
	if revision := version.Revision(); revision == stamped[1] && stamped[1] != "" {
		t.Errorf("Revision() returned the cleared stamp %q", revision)
	}
	if date := version.BuildDate(); date == stamped[2] && stamped[2] != "" {
		t.Errorf("BuildDate() returned the cleared stamp %q", date)
	}

	// Long has to stay one readable line whatever is missing: it is what a user
	// pastes into an issue, and "vat  (commit , built , ...)" tells nobody
	// anything.
	long := version.Long()
	for _, want := range []string{"vat ", "commit ", "built ", runtime.GOOS, runtime.GOARCH} {
		if !strings.Contains(long, want) {
			t.Errorf("Long() = %q, missing %q", long, want)
		}
	}
	if strings.Contains(long, "commit ,") || strings.Contains(long, "built ,") {
		t.Errorf("Long() left a field blank rather than saying unknown: %q", long)
	}
}

// A long commit hash is truncated so the line stays readable; a short one must
// survive intact rather than being padded or cut.
func TestLongTruncatesACommitWithoutLosingAShortOne(t *testing.T) {
	// Arrange
	stamped := version.Commit
	t.Cleanup(func() { version.Commit = stamped })

	// Act & Assert
	version.Commit = "0123456789abcdef0123"
	if long := version.Long(); !strings.Contains(long, "commit 0123456789ab,") {
		t.Errorf("Long() did not truncate a full hash to twelve characters: %q", long)
	}
	version.Commit = "abc1234"
	if long := version.Long(); !strings.Contains(long, "commit abc1234,") {
		t.Errorf("Long() mangled a short commit: %q", long)
	}
}
