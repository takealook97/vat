package workspace

import (
	"path/filepath"
	"strings"
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
	resolvedTarget := resolveExisting(target)
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

// resolveExisting resolves the longest existing prefix of a path and rejoins
// whatever remains, so a not-yet-created directory still resolves through any
// symlinked ancestor.
func resolveExisting(target string) string {
	absolute, err := filepath.Abs(target)
	if err != nil {
		return filepath.Clean(target)
	}
	remainder := ""
	current := absolute
	for {
		if resolved, err := filepath.EvalSymlinks(current); err == nil {
			return filepath.Join(resolved, remainder)
		}
		parent := filepath.Dir(current)
		if parent == current {
			return absolute
		}
		remainder = filepath.Join(filepath.Base(current), remainder)
		current = parent
	}
}
