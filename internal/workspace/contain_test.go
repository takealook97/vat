package workspace_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/takealook97/vat/internal/workspace"
)

func TestContainsRefusesTheRootAndAnythingOutsideIt(t *testing.T) {
	// Arrange: this is the guard standing between `repo remove --delete` and
	// os.RemoveAll, so it is asserted directly rather than only through the
	// manifest that normally prevents reaching it.
	root := t.TempDir()
	inside := filepath.Join(root, "payments")
	if err := os.MkdirAll(inside, 0o755); err != nil {
		t.Fatalf("create: %v", err)
	}
	outside := t.TempDir()

	cases := []struct {
		path  string
		below bool
	}{
		{inside, true},
		{filepath.Join(inside, "nested"), true},
		{root, false},
		{outside, false},
		{filepath.Dir(root), false},
	}

	for _, testCase := range cases {
		// Act & Assert
		if got := workspace.Contains(root, testCase.path); got != testCase.below {
			t.Errorf("Contains(%q, %q) = %v, want %v",
				root, testCase.path, got, testCase.below)
		}
	}
}

func TestContainsSeesThroughASymlinkThatPointsOutOfTheWorkspace(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("creating a symlink on Windows needs a privilege the test cannot assume")
	}
	// Arrange: the case textual containment cannot see. A link inside the
	// workspace satisfies every string comparison while every write through it
	// lands somewhere vat is not allowed to touch.
	root := t.TempDir()
	outside := t.TempDir()
	link := filepath.Join(root, "payments")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlinks are unavailable here: %v", err)
	}

	// Act & Assert
	if workspace.Contains(root, link) {
		t.Error("a symlink pointing outside the workspace was reported as inside it")
	}

	// A link to a directory that really is inside must still be accepted, or
	// the check would refuse legitimate layouts rather than dangerous ones.
	real := filepath.Join(root, "real")
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatalf("create: %v", err)
	}
	innerLink := filepath.Join(root, "alias")
	if err := os.Symlink(real, innerLink); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	if !workspace.Contains(root, innerLink) {
		t.Error("a symlink to a directory inside the workspace was reported as outside it")
	}
}

func TestContainsAnswersForAPathThatDoesNotExistYet(t *testing.T) {
	// Arrange: `vat repo new` asks before it creates anything, so the answer has
	// to be correct for a directory that is not there yet.
	root := t.TempDir()

	// Act & Assert
	if !workspace.Contains(root, filepath.Join(root, "not-created-yet")) {
		t.Error("a path that does not exist yet was reported as outside the workspace")
	}
	if workspace.Contains(root, filepath.Join(filepath.Dir(root), "elsewhere", "deep")) {
		t.Error("a path outside the workspace was accepted because it does not exist")
	}
}

func TestContainsRefusesARootThatCannotBeResolved(t *testing.T) {
	// Arrange: an unresolvable root must fail closed. Returning true here would
	// hand os.RemoveAll a path nothing had vouched for.
	missing := filepath.Join(t.TempDir(), "absent")

	// Act & Assert
	if workspace.Contains(missing, filepath.Join(missing, "child")) {
		t.Error("an unresolvable root was treated as containing its child")
	}
}

func TestTheWorkspaceMethodAndThePackageFunctionAgree(t *testing.T) {
	// Arrange: the method is what lint calls and the function is what the
	// commands call. Two entry points to one answer is only safe while they
	// cannot diverge.
	root := t.TempDir()
	inside := filepath.Join(root, "payments")
	if err := os.MkdirAll(inside, 0o755); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "vat.yaml"),
		[]byte("version: 1\nworkspace:\n  name: acme\n"), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	ws, err := workspace.OpenAt(root)
	if err != nil {
		t.Fatalf("OpenAt: %v", err)
	}

	// Act & Assert
	for _, path := range []string{inside, root, t.TempDir()} {
		if ws.Contains(path) != workspace.Contains(root, path) {
			t.Errorf("the method and the function disagree about %q", path)
		}
	}
	if !ws.Contains(inside) {
		t.Error("a directory inside the workspace was reported as outside it")
	}
}
