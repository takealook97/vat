package lint

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/takealook97/vat/internal/brain"
	"github.com/takealook97/vat/internal/fsx"
	"github.com/takealook97/vat/internal/harness"
	"github.com/takealook97/vat/internal/workspace"
)

// FixResult lists what a repair pass changed.
type FixResult struct {
	Changed []string `json:"changed"`
}

// Fix repairs the findings that can be repaired without judgement: regenerating
// what is generated, and re-excluding what should have been excluded.
//
// Nothing here touches a fact, a decision, or a working tree. A rule whose
// repair would require deciding what someone meant is reported and left alone —
// automatically resolving those is how a lint tool starts destroying work.
func Fix(ws *workspace.Workspace, now time.Time) (FixResult, error) {
	var result FixResult

	changed, err := ws.SyncGitignore(ws.Manifest)
	if err != nil {
		return result, err
	}
	if changed {
		result.Changed = append(result.Changed, ".gitignore")
	}

	rendered, err := RenderHarness(ws)
	if err != nil {
		return result, err
	}
	result.Changed = append(result.Changed, rendered...)

	if root, ok := ws.BrainPath(); ok && brain.IsBrain(root) {
		store, err := brain.Load(root)
		if err != nil {
			return result, err
		}
		build, err := brain.Build(store, now)
		if err != nil {
			return result, err
		}
		for _, name := range build.Changed {
			result.Changed = append(result.Changed, ws.Rel(filepath.Join(root, name)))
		}
	}
	return result, nil
}

// RenderHarness writes the workspace and per-repository generated regions and
// every runtime adapter, returning the workspace-relative paths that changed.
func RenderHarness(ws *workspace.Workspace) ([]string, error) {
	var changed []string

	rootPath := ws.Path("AGENTS.md")
	updated, err := applyRegion(rootPath, harness.RenderWorkspace(ws.Manifest), workspaceHarnessPreamble(ws))
	if err != nil {
		return nil, err
	}
	if updated {
		changed = append(changed, "AGENTS.md")
	}
	// Claude Code reads CLAUDE.md; pointing it at the same file keeps one
	// contract rather than two that drift.
	claudePath := ws.Path("CLAUDE.md")
	if !fsx.Exists(claudePath) {
		if err := fsx.WriteFileAtomic(claudePath, []byte("@AGENTS.md\n"), fsx.DefaultFileMode); err != nil {
			return nil, err
		}
		changed = append(changed, "CLAUDE.md")
	}

	for _, repo := range ws.Manifest.Repos {
		dir := ws.RepoPath(repo)
		if repo.Archived || !fsx.IsDir(dir) {
			continue
		}
		path := filepath.Join(dir, "AGENTS.md")
		updated, err := applyRegion(path, harness.RenderRepo(ws.Manifest, repo), repoHarnessPreamble(repo.Name))
		if err != nil {
			return nil, err
		}
		if updated {
			changed = append(changed, ws.Rel(path))
		}
	}

	roles, _, err := harness.LoadRoles(ws.Root)
	if err != nil {
		return nil, err
	}
	adapters, err := harness.WriteAdapters(ws.Root, roles)
	if err != nil {
		return nil, err
	}
	changed = append(changed, adapters...)

	skills, _, err := harness.LoadSkills(ws.Root)
	if err != nil {
		return nil, err
	}
	skillAdapters, err := harness.WriteSkillAdapters(ws.Root, skills)
	if err != nil {
		return nil, err
	}
	changed = append(changed, skillAdapters...)
	return changed, nil
}

func applyRegion(path, region, preamble string) (bool, error) {
	current, exists, err := fsx.ReadFileIfExists(path)
	if err != nil {
		return false, err
	}
	base := string(current)
	if !exists || strings.TrimSpace(base) == "" {
		base = preamble
	}
	next := harness.ApplyRegion(base, region)
	if next == string(current) {
		return false, nil
	}
	if err := fsx.WriteFileAtomic(path, []byte(next), fsx.DefaultFileMode); err != nil {
		return false, err
	}
	return true, nil
}

func workspaceHarnessPreamble(ws *workspace.Workspace) string {
	return fmt.Sprintf(`# %s

Hand-written notes go above the generated region. Everything below it is
rendered from `+"`vat.yaml`"+` by `+"`vat harness render`"+` and will be replaced.

Keep this file short. It is a map of where things are, not a copy of what is in
them: a bloated root contract can push the per-repository contracts out of an
agent's context entirely.
`, ws.Manifest.Workspace.Name)
}

func repoHarnessPreamble(name string) string {
	return fmt.Sprintf(`# %s

<!--
Write this repository's own contract here, above the generated region:

- What it is responsible for, and what it is not.
- What to read before editing.
- Which files must change together when a contract changes.
- What proves the work is done.
- What it must never do without explicit approval.

The region below is rendered from the workspace manifest. Do not edit it.
-->
`, name)
}
