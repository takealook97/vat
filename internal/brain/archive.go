package brain

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/takealook97/vat/internal/fsx"
)

// ArchiveMove is one record leaving the working set.
type ArchiveMove struct {
	ID      string `json:"id"`
	Status  Status `json:"status"`
	From    string `json:"from"`
	To      string `json:"to"`
	Applied bool   `json:"applied"`
}

// Archive moves records that have reached an end state into archive/, keeping
// the directory layout so a reader can still tell a decision from a gap.
//
// Two things depend on terminal records living somewhere separate. The entry
// point is meant to be a fixed-size place to start, and it cannot be while
// every record ever written stays in the working directories. And an external
// index can only exclude withdrawn and replaced claims cheaply — by directory —
// if they are actually in one; a superseded decision surfacing as an answer is
// the failure this layer exists to prevent.
//
// Nothing is deleted and nothing is rewritten except the relative links the
// move itself invalidates. An archived record is still loaded, so the
// supersession chain it belongs to is still checked from both ends.
func Archive(store *Store, apply bool) ([]ArchiveMove, error) {
	moves := []ArchiveMove{}
	for _, record := range SortRecords(store.Records) {
		if !record.Status.Terminal() || record.Archived {
			continue
		}
		destination := path.Join(archiveDir, record.Path)
		move := ArchiveMove{ID: record.ID, Status: record.Status, From: record.Path, To: destination}
		if apply {
			if err := moveRecord(store.Root, record, destination); err != nil {
				return nil, err
			}
			move.Applied = true
		}
		moves = append(moves, move)
	}
	return moves, nil
}

func moveRecord(root string, record Record, destination string) error {
	source := filepath.Join(root, filepath.FromSlash(record.Path))
	target := filepath.Join(root, filepath.FromSlash(destination))
	if fsx.Exists(target) {
		return fmt.Errorf("%s already exists; resolve it before archiving %s", destination, record.ID)
	}
	if err := fsx.EnsureDir(filepath.Dir(target)); err != nil {
		return err
	}
	data, err := os.ReadFile(source)
	if err != nil {
		return fmt.Errorf("read %s: %w", record.Path, err)
	}
	repointed := repointLinks(string(data), path.Dir(record.Path), path.Dir(destination))
	if err := fsx.WriteFileAtomic(target, []byte(repointed), fsx.DefaultFileMode); err != nil {
		return err
	}
	// The copy is written and fsynced before the original goes, so an
	// interruption leaves the record in two places rather than in none.
	if err := os.Remove(source); err != nil {
		return fmt.Errorf("remove %s: %w", record.Path, err)
	}
	return nil
}

// repointLinks rewrites the relative Markdown links in a record that has moved
// from one directory to another, so they resolve to the same files afterwards.
func repointLinks(content, from, to string) string {
	if from == to {
		return content
	}
	return markdownLink.ReplaceAllStringFunc(content, func(match string) string {
		groups := markdownLink.FindStringSubmatch(match)
		target := strings.TrimSpace(groups[1])
		if !isRelativeLink(target) {
			return match
		}
		anchor := ""
		if hash := strings.Index(target, "#"); hash >= 0 {
			target, anchor = target[:hash], target[hash:]
		}
		if target == "" {
			return match
		}
		return strings.Replace(match, groups[1], relativePath(to, path.Join(from, target))+anchor, 1)
	})
}

func isRelativeLink(target string) bool {
	switch {
	case target == "",
		strings.Contains(target, "://"),
		strings.HasPrefix(target, "#"),
		strings.HasPrefix(target, "/"),
		strings.HasPrefix(target, "mailto:"):
		return false
	}
	return true
}

// relativePath expresses target relative to base, both rooted at the brain
// directory. filepath.Rel is not used: these are record paths, which are
// forward-slashed on every operating system.
func relativePath(base, target string) string {
	baseParts := splitPath(base)
	targetParts := splitPath(target)
	common := 0
	for common < len(baseParts) && common < len(targetParts) && baseParts[common] == targetParts[common] {
		common++
	}
	parts := make([]string, 0, len(baseParts)-common+len(targetParts)-common)
	for i := common; i < len(baseParts); i++ {
		parts = append(parts, "..")
	}
	parts = append(parts, targetParts[common:]...)
	if len(parts) == 0 {
		return "."
	}
	return path.Join(parts...)
}

func splitPath(value string) []string {
	cleaned := path.Clean(value)
	if cleaned == "." || cleaned == "/" {
		return nil
	}
	return strings.Split(strings.Trim(cleaned, "/"), "/")
}
