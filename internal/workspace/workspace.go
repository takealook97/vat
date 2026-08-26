// Package workspace resolves the workspace root and gives the rest of vat a
// single, validated view of it: where the manifest is, where each repository
// should live, and which files vat owns.
package workspace

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/takealook97/vat/internal/manifest"
)

// ErrNoWorkspace is returned when no vat.yaml exists in the current directory
// or any ancestor.
var ErrNoWorkspace = errors.New("not inside a vat workspace")

// EnvRoot lets a user or a CI job pin the workspace root explicitly.
const EnvRoot = "VAT_WORKSPACE"

// Workspace is a loaded workspace: its root directory and its manifest.
type Workspace struct {
	Root     string
	Manifest manifest.Manifest
}

// Find walks up from start looking for a manifest and returns the directory
// holding it. VAT_WORKSPACE, when set, wins over the walk.
func Find(start string) (string, error) {
	if pinned := os.Getenv(EnvRoot); pinned != "" {
		if _, err := os.Stat(filepath.Join(pinned, manifest.FileName)); err != nil {
			return "", fmt.Errorf("%s=%s has no %s", EnvRoot, pinned, manifest.FileName)
		}
		return filepath.Clean(pinned), nil
	}
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", start, err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, manifest.FileName)); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("%w: no %s in %s or any parent directory",
				ErrNoWorkspace, manifest.FileName, start)
		}
		dir = parent
	}
}

// Open finds and loads the workspace containing start.
func Open(start string) (*Workspace, error) {
	root, err := Find(start)
	if err != nil {
		return nil, err
	}
	return OpenAt(root)
}

// OpenAt loads the workspace rooted at an exact directory.
func OpenAt(root string) (*Workspace, error) {
	loaded, err := manifest.Load(filepath.Join(root, manifest.FileName))
	if err != nil {
		return nil, err
	}
	return &Workspace{Root: filepath.Clean(root), Manifest: loaded}, nil
}

// ManifestPath returns the absolute path of vat.yaml.
func (w *Workspace) ManifestPath() string {
	return filepath.Join(w.Root, manifest.FileName)
}

// RepoPath returns the absolute directory a repository should occupy, and can
// never point outside the workspace.
//
// The manifest already rejects an escaping path, but this is the function every
// destructive operation resolves through — cloning, writing a harness, deleting
// a directory. Rooting the join here means a path that somehow bypassed
// validation still cannot reach outside.
func (w *Workspace) RepoPath(repo manifest.Repo) string {
	return w.Path(repo.Dir())
}

// Path joins workspace-relative parts onto the root, containing the result.
// "../../etc" resolves to <root>/etc rather than escaping.
func (w *Workspace) Path(parts ...string) string {
	joined := filepath.Join(parts...)
	contained := filepath.Clean(string(filepath.Separator) + joined)
	return filepath.Join(w.Root, strings.TrimPrefix(contained, string(filepath.Separator)))
}

// Rel makes an absolute path readable in output by expressing it relative to
// the workspace root.
func (w *Workspace) Rel(path string) string {
	rel, err := filepath.Rel(w.Root, path)
	if err != nil {
		return path
	}
	return rel
}

// StateDir is where vat keeps derived, regenerable local state such as the
// metrics ledger. Nothing canonical is ever stored here.
func (w *Workspace) StateDir() string { return filepath.Join(w.Root, ".vat") }

// ChangesetsDir holds the multi-repository completion records.
func (w *Workspace) ChangesetsDir() string { return filepath.Join(w.Root, "changesets") }

// EvidenceDir holds evidence packets handed to workers.
func (w *Workspace) EvidenceDir() string { return filepath.Join(w.Root, "evidence") }

// BrainPath returns the absolute path of the brain repository, when the
// workspace has adopted one.
func (w *Workspace) BrainPath() (string, bool) {
	repo, ok := w.Manifest.BrainRepo()
	if !ok {
		return "", false
	}
	return w.RepoPath(repo), true
}

// SaveManifest validates and writes the manifest back to disk.
func (w *Workspace) SaveManifest(next manifest.Manifest) error {
	return manifest.Save(w.ManifestPath(), next)
}

// Select resolves a selector against the manifest.
func (w *Workspace) Select(sel manifest.Selector) ([]manifest.Repo, error) {
	return w.Manifest.Select(sel)
}

// Exists reports whether a repository's directory is present on disk.
func (w *Workspace) Exists(repo manifest.Repo) bool {
	info, err := os.Stat(w.RepoPath(repo))
	return err == nil && info.IsDir()
}
