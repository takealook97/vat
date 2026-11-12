package workspace

import (
	"path/filepath"
	"strings"

	"github.com/takealook97/vat/internal/fsx"
)

// Contains reports whether target sits inside root and is not root itself.
//
// Both sides are resolved through symlinks first, so a link pointing out of the
// workspace is caught: textual containment cannot see one, and `vat repo adopt`
// on such a link once wrote a generated contract into a repository outside the
// only directory vat is allowed to write to.
//
// The target usually exists, but the answer has to be correct for a path that
// does not, so resolution falls back to the nearest existing ancestor and
// re-appends the remainder.
//
// It lives here, beside root discovery, because the commands that create,
// delete, move, or adopt a directory and the lint rule that audits the result
// must not be able to disagree about what "inside the workspace" means.
func Contains(root, target string) bool {
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return false
	}
	resolvedTarget := fsx.ResolveExisting(target)
	relative, err := filepath.Rel(resolvedRoot, resolvedTarget)
	if err != nil {
		return false
	}
	if relative == "." || relative == "" {
		return false
	}
	return relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

// Contains reports whether target sits strictly below this workspace's root.
func (w *Workspace) Contains(target string) bool { return Contains(w.Root, target) }
