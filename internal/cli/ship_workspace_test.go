package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// All three workspaces that run vat wrote their own scripts/shipping-gate.sh,
// and two of them are identical but for a marker string. They answer a question
// no vat command did: not "has this changeset landed" — which needs an
// identifier and judges one bundle — but "is this round closed", across every
// repository at once, with the knowledge repository judged alongside the
// products. One of those scripts says why in a comment: a round where the
// product went up and the canonical record stayed on somebody's laptop is not
// shipped.
//
// `vat ship` with no identifier is that question. It is the same verb and the
// same concept — has the work reached everybody else — widened from one
// changeset to the workspace.

// pushAll commits whatever adoption left behind and puts each repository's main
// on its upstream, which is what a closed round looks like.
func pushAll(t *testing.T, h *workspaceFixture, names ...string) {
	t.Helper()
	for _, name := range names {
		// The fixture's upstreams are ordinary clones with a work tree, which
		// git refuses to let anyone push the checked-out branch of.
		git(t, h.upstream(name), "config", "receive.denyCurrentBranch", "ignore")
		git(t, h.path(name), "push", "--quiet", "origin", "main")
	}
	// The root holds the manifest and the generated contracts, and adoption
	// leaves them uncommitted. A round is not closed while the roster is only
	// on one machine, which is what the gate says about it.
	upstream := filepath.Join(filepath.Dir(h.root), "upstream", "workspace.git")
	git(t, filepath.Dir(h.root), "init", "--quiet", "--bare", "--initial-branch", "main", upstream)
	git(t, h.root, "add", "-A")
	git(t, h.root, "commit", "--quiet", "-m", "adopt")
	git(t, h.root, "remote", "add", "origin", upstream)
	git(t, h.root, "push", "--quiet", "origin", "main")
}

// writeAndCommit lands a commit in a clone and nowhere else, which is what an
// unshipped round looks like.
func writeAndCommit(t *testing.T, h *workspaceFixture, name, file, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(h.path(name), file), []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	git(t, h.path(name), "add", "-A")
	git(t, h.path(name), "commit", "--quiet", "-m", "local only")
}

func TestShipWithNoChangesetJudgesTheWholeWorkspace(t *testing.T) {
	// Arrange: everything committed and pushed is a closed round.
	h := adoptedFixture(t, "payments", "console")
	pushAll(t, h, "payments", "console")

	// Act
	code, output := h.run("ship")

	// Assert
	if code != ExitOK {
		t.Fatalf("a workspace with nothing outstanding did not read as closed:\n%s", output)
	}
	for _, name := range []string{"payments", "console"} {
		if !strings.Contains(output, name) {
			t.Errorf("%s was not judged:\n%s", name, output)
		}
	}
}

func TestShipReportsARepositoryWhoseWorkIsStillLocal(t *testing.T) {
	// Arrange: the failure the scripts exist to catch. The tree is clean and the
	// branch is right, and the commit exists nowhere else.
	h := adoptedFixture(t, "payments", "console")
	pushAll(t, h, "payments", "console")
	writeAndCommit(t, h, "payments", "NOTES.md", "kept at home\n")

	// Act
	code, output := h.run("ship")

	// Assert
	if code == ExitOK {
		t.Fatalf("a commit that exists only on this machine passed the gate:\n%s", output)
	}
	if !strings.Contains(output, "payments") {
		t.Errorf("the finding does not name the repository that is behind:\n%s", output)
	}
	if !strings.Contains(output, "console") {
		t.Errorf("the repositories that are closed went unreported:\n%s", output)
	}
}

func TestShipReportsADirtyWorkingTreeAndChangesNothing(t *testing.T) {
	// Arrange: a gate that tidied up would be another way to deploy, not a gate.
	h := adoptedFixture(t, "payments")
	pushAll(t, h, "payments")
	scratch := filepath.Join(h.path("payments"), "UNSAVED.md")
	if err := os.WriteFile(scratch, []byte("half a thought\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Act
	code, output := h.run("ship")

	// Assert
	if code == ExitOK {
		t.Fatalf("an uncommitted change passed the gate:\n%s", output)
	}
	if _, err := os.Stat(scratch); err != nil {
		t.Errorf("the gate removed the work it was reporting: %v", err)
	}
}

func TestShipReportsEveryRepositoryInOnePass(t *testing.T) {
	// Arrange: this runs in a loop while a round is being closed, and one
	// finding per run makes that loop unusable.
	h := adoptedFixture(t, "payments", "console")
	pushAll(t, h, "payments", "console")
	writeAndCommit(t, h, "payments", "A.md", "a\n")
	writeAndCommit(t, h, "console", "B.md", "b\n")

	// Act
	_, output := h.run("ship")

	// Assert
	for _, name := range []string{"payments", "console"} {
		if !strings.Contains(output, name) {
			t.Errorf("%s was not reported in the same pass:\n%s", name, output)
		}
	}
}

func TestShipStillJudgesOneChangesetWhenGivenOne(t *testing.T) {
	// Arrange: widening the command must not take the original question away.
	h := adoptedFixture(t, "payments")
	pushAll(t, h, "payments")
	h.mustRun("changeset", "new", "Move cancellation to v2", "--repos", "payments")

	// Act
	code, output := h.run("ship", "CS-0001")

	// Assert: unverified, so it refuses — which is the pre-existing behaviour
	// and proves the identifier still selects the changeset question.
	if code == ExitOK {
		t.Fatalf("an unverified changeset was judged as shippable:\n%s", output)
	}
	if !strings.Contains(output, "CS-0001") {
		t.Errorf("the changeset question was not asked:\n%s", output)
	}
}
