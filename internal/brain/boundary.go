package brain

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/takealook97/vat/internal/fsx"
)

// confined refuses a write destination that resolves outside root once the
// symbolic links already on disk are followed.
//
// Checking the root alone was not enough. A `history/` or `decisions/` that is
// a link to a directory elsewhere carried `brain init` and `brain new` out of
// the directory they were told to write in — for a standalone brain, out of
// the only boundary there is. Every destination is checked before anything is
// written, so a refusal leaves the tree as it was. A record written through
// such a link would not even be read back: the loader does not follow a linked
// kind directory.
func confined(root string, destinations ...string) error {
	base := fsx.ResolveExisting(root)
	for _, destination := range destinations {
		relative, err := filepath.Rel(base, fsx.ResolveExisting(destination))
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return fmt.Errorf("refused: %s leads outside the brain through a symbolic link; nothing was written",
				relTo(root, destination))
		}
	}
	return nil
}
