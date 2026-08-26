package syncx

import (
	"errors"
	"strings"
	"testing"

	"github.com/takealook97/vat/internal/gitx"
	"github.com/takealook97/vat/internal/manifest"
)

// gitErrorDetail is the last thing standing between git's stderr and the
// terminal on the update path, and git quotes the remote it could not reach.
// It is unexported, so it is tested from inside the package rather than left as
// the one redaction point nothing exercised.

func TestGitErrorDetailRedactsTheRemoteGitQuotedBack(t *testing.T) {
	// Arrange: this is the shape of a real `git fetch` failure against a remote
	// carrying a token — the exact string that must never reach the terminal.
	failure := &gitx.CommandError{
		Args:   []string{"fetch", "origin"},
		Dir:    "/w/payments",
		Stderr: "fatal: unable to access 'https://x-token:ghp_SUPERSECRET@example.invalid/acme/payments.git/': Could not resolve host",
		Err:    errors.New("exit status 128"),
	}

	// Act
	detail := gitErrorDetail(failure)

	// Assert
	if strings.Contains(detail, "ghp_SUPERSECRET") {
		t.Fatalf("the credential survived redaction: %s", detail)
	}
	if !strings.Contains(detail, "***@example.invalid") {
		t.Errorf("detail = %q, want the authority masked and the host kept", detail)
	}
}

func TestGitErrorDetailSkipsBlankLinesAndKeepsTheFirstMeaningfulOne(t *testing.T) {
	// Arrange: git pads its stderr, and returning the first line verbatim used
	// to surface an empty string as the whole explanation.
	failure := &gitx.CommandError{
		Args:   []string{"pull"},
		Stderr: "\n\n  fatal: refusing to merge unrelated histories\nhint: something else\n",
		Err:    errors.New("exit status 128"),
	}

	// Act
	detail := gitErrorDetail(failure)

	// Assert
	if detail != "fatal: refusing to merge unrelated histories" {
		t.Errorf("detail = %q, want the first non-blank line", detail)
	}
}

func TestGitErrorDetailFallsBackToTheErrorItselfAndStillRedacts(t *testing.T) {
	// Arrange: not every failure is a CommandError, and the fallback path is
	// the one that would quietly stop redacting.
	plain := errors.New("dial https://user:ghp_TOKEN@example.invalid/acme/a.git: refused")

	// Act
	detail := gitErrorDetail(plain)

	// Assert
	if strings.Contains(detail, "ghp_TOKEN") {
		t.Fatalf("the credential survived the fallback path: %s", detail)
	}
}

func TestDisplayBranchNamesADetachedHeadRatherThanPrintingNothing(t *testing.T) {
	// Arrange & Act & Assert: an empty column reads as "no problem", which a
	// detached HEAD is not.
	if got := displayBranch(""); got != "(detached)" {
		t.Errorf("displayBranch(\"\") = %q, want \"(detached)\"", got)
	}
	if got := displayBranch("main"); got != "main" {
		t.Errorf("displayBranch(\"main\") = %q, want it unchanged", got)
	}
}

func TestSortResultsRestoresManifestOrderWhateverOrderTheyFinishedIn(t *testing.T) {
	// Arrange: the workers run in parallel and finish in arbitrary order, so
	// two identical runs would otherwise print their rows differently.
	order := []manifest.Repo{{Name: "payments"}, {Name: "identity"}, {Name: "notes"}}
	finished := []Result{{Repo: "notes"}, {Repo: "payments"}, {Repo: "identity"}}

	// Act
	sorted := SortResults(finished, order)

	// Assert
	want := []string{"payments", "identity", "notes"}
	for i, name := range want {
		if sorted[i].Repo != name {
			t.Errorf("position %d = %q, want %q", i, sorted[i].Repo, name)
		}
	}
	if finished[0].Repo != "notes" {
		t.Error("SortResults reordered its argument instead of returning a new slice")
	}
}

func TestSortResultsKeepsAResultTheManifestNoLongerNamesInsteadOfDroppingIt(t *testing.T) {
	// Arrange: a repository removed from the manifest mid-run must still be
	// reported. Silently losing its row would hide a failure.
	order := []manifest.Repo{{Name: "payments"}}
	finished := []Result{{Repo: "payments"}, {Repo: "vanished"}}

	// Act
	sorted := SortResults(finished, order)

	// Assert
	if len(sorted) != 2 {
		t.Fatalf("got %d results, want both", len(sorted))
	}
}
