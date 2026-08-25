package gitx_test

import (
	"strings"
	"testing"

	"github.com/takealook97/vat/internal/gitx"
)

func TestSameRemoteAcceptsTheSpellingsOfOneRepository(t *testing.T) {
	// Arrange: SSH and HTTPS are two authenticated routes to the same place.
	pairs := [][2]string{
		{"https://github.com/acme/payments.git", "git@github.com:acme/payments.git"},
		{"https://github.com/acme/payments", "ssh://git@github.com/acme/payments.git"},
		{"https://github.com/acme/payments/", "https://github.com/acme/payments"},
		{"https://GitHub.com/acme/payments", "https://github.com/acme/payments"},
	}

	for _, pair := range pairs {
		// Act & Assert
		if !gitx.SameRemote(pair[0], pair[1]) {
			t.Errorf("SameRemote(%q, %q) = false, want true", pair[0], pair[1])
		}
	}
}

func TestSameRemoteKeepsPathCaseSignificant(t *testing.T) {
	// Arrange: on a case-sensitive host these are different repositories, and
	// treating them as one would let a lookalike pass the supply-chain check.
	const declared = "https://example.com/acme/Payments.git"
	const actual = "https://example.com/acme/payments.git"

	// Act & Assert
	if gitx.SameRemote(declared, actual) {
		t.Error("two paths differing only in case compared equal")
	}
}

func TestSameRemoteRefusesToFoldHTTPIntoHTTPS(t *testing.T) {
	// Arrange: silently accepting a downgrade to an unauthenticated transport
	// is exactly the substitution this comparison exists to catch.
	const secure = "https://example.com/acme/payments.git"
	const insecure = "http://example.com/acme/payments.git"

	// Act & Assert
	if gitx.SameRemote(secure, insecure) {
		t.Error("http compared equal to https")
	}
}

func TestSameRemoteIgnoresCredentialsEmbeddedInTheURL(t *testing.T) {
	// Arrange: the same repository, one spelling carrying a token.
	const plain = "https://example.com/acme/payments.git"
	const withToken = "https://x-token:ghp_EXAMPLE@example.com/acme/payments.git"

	// Act & Assert
	if !gitx.SameRemote(plain, withToken) {
		t.Error("a URL carrying credentials was treated as a different repository")
	}
}

func TestSameRemoteRejectsADifferentHost(t *testing.T) {
	// Act & Assert
	if gitx.SameRemote("https://github.com/acme/payments", "https://evil.example/acme/payments") {
		t.Error("different hosts compared equal")
	}
}

func TestRedactRemovesCredentialsButKeepsTheRepositoryIdentifiable(t *testing.T) {
	// Arrange: vat reports a remote mismatch by printing both URLs, and that
	// report must not become the one place a credential is disclosed.
	const withToken = "https://x-token:ghp_SUPERSECRETVALUE@example.com/acme/payments.git"

	// Act
	got := gitx.Redact(withToken)

	// Assert
	if strings.Contains(got, "ghp_SUPERSECRETVALUE") {
		t.Fatalf("Redact leaked the token: %q", got)
	}
	if strings.Contains(got, "x-token") {
		t.Errorf("Redact leaked the user name: %q", got)
	}
	for _, want := range []string{"example.com", "acme/payments"} {
		if !strings.Contains(got, want) {
			t.Errorf("Redact removed %q, which is needed to identify the repository: %q", want, got)
		}
	}
}

func TestRedactLeavesACleanURLUnchanged(t *testing.T) {
	// Arrange
	cases := []string{
		"https://example.com/acme/payments.git",
		"git@example.com:acme/payments.git",
		"/srv/git/payments.git",
	}

	for _, url := range cases {
		// Act & Assert
		if got := gitx.Redact(url); got != url {
			t.Errorf("Redact(%q) = %q, want it unchanged", url, got)
		}
	}
}

func TestDivergenceDescribesTheThreeInterestingStates(t *testing.T) {
	// Arrange
	diverged := gitx.Divergence{Ahead: 2, Behind: 3}
	behind := gitx.Divergence{Behind: 3}
	synced := gitx.Divergence{}

	// Act & Assert
	if !diverged.Diverged() {
		t.Error("both sides holding commits was not reported as diverged")
	}
	if behind.Diverged() {
		t.Error("being behind was reported as diverged")
	}
	if !synced.InSync() {
		t.Error("equal refs were not reported as in sync")
	}
	if behind.InSync() {
		t.Error("being behind was reported as in sync")
	}
}

func TestIsRepositoryIsFalseForAPlainDirectory(t *testing.T) {
	// Act & Assert
	if gitx.IsRepository(t.TempDir()) {
		t.Error("a directory with no .git was reported as a repository")
	}
}

func TestAvailableFindsGit(t *testing.T) {
	// The whole tool is unusable without it, so this is worth asserting.
	if !gitx.Available() {
		t.Skip("git is not installed in this environment")
	}
}
