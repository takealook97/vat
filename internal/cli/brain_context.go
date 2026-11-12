package cli

import (
	"errors"
	"path/filepath"

	"github.com/takealook97/vat/internal/brain"
	"github.com/takealook97/vat/internal/fsx"
	"github.com/takealook97/vat/internal/manifest"
	"github.com/takealook97/vat/internal/workspace"
)

// Which brain a command acts on, and under whose policy.
//
// A brain was reachable only through a workspace manifest, so a service that
// kept one directory of records had to generate a vat.yaml naming a single
// repository nobody synchronised, purely to satisfy the lookup. A brain can now
// stand alone: named with --brain-dir, or found by its marker above the current
// directory. A workspace still wins wherever there is one, and a manifest that
// fails to load is reported rather than quietly treated as no workspace at all —
// falling back would run a governed brain under the wrong policy.

// BrainContext is everything a brain command needs to act.
type BrainContext struct {
	// Workspace is nil for a standalone brain. Every operation that needs a
	// repository list — pinning evidence, reading a source revision — checks
	// for that and refuses rather than guessing.
	Workspace *workspace.Workspace
	Store     *brain.Store
	Policy    brain.CheckPolicy
	// RequirePromotionGate is the supersede rule: a replacement waits for
	// promotion rather than becoming citable on arrival.
	RequirePromotionGate bool
}

// Standalone reports whether no workspace governs this brain.
func (c BrainContext) Standalone() bool { return c.Workspace == nil }

// resolveBrain opens the brain this invocation applies to.
func resolveBrain(env *Env) (BrainContext, error) {
	if env.BrainDir != "" {
		root := explicitBrainRoot(env)
		ws, err := workspaceAround(env, root)
		if err != nil {
			return BrainContext{}, err
		}
		if ws != nil {
			if declared, ok := ws.BrainPath(); ok && sameDir(declared, root) {
				return openWorkspaceBrain(ws)
			}
		}
		return openStandaloneBrain(root)
	}
	ws, err := env.Workspace()
	if err == nil {
		return openWorkspaceBrain(ws)
	}
	if !noWorkspace(err) || env.Root != "" {
		return BrainContext{}, err
	}
	root, found := findMarker(env.Cwd)
	if !found {
		return BrainContext{}, err
	}
	return openStandaloneBrain(root)
}

// noWorkspace separates "there is no manifest" from "there is one and it is
// broken". Only the first may fall through to a standalone brain.
func noWorkspace(err error) bool {
	return errors.Is(err, workspace.ErrNoWorkspace) || errors.Is(err, manifest.ErrNotFound)
}

// explicitBrainRoot resolves --brain-dir against the directory vat was run in.
func explicitBrainRoot(env *Env) string {
	root := env.BrainDir
	if !filepath.IsAbs(root) {
		root = filepath.Join(env.Cwd, root)
	}
	return filepath.Clean(root)
}

// workspaceAround returns the workspace that contains root, or nil when there
// is none. A brain named explicitly is governed by the workspace only when it
// is the brain that workspace declares; anything else would apply a policy the
// workspace never stated for that directory.
func workspaceAround(env *Env, root string) (*workspace.Workspace, error) {
	ws, err := env.Workspace()
	if err != nil {
		if noWorkspace(err) && env.Root == "" {
			return nil, nil
		}
		return nil, err
	}
	if !workspace.Contains(ws.Root, root) {
		return nil, nil
	}
	return ws, nil
}

// findMarker searches upward from start for a directory holding a brain
// marker.
func findMarker(start string) (string, bool) {
	if start == "" {
		return "", false
	}
	dir := filepath.Clean(start)
	for {
		if fsx.Exists(filepath.Join(dir, brain.MarkerFile)) {
			return dir, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

func openWorkspaceBrain(ws *workspace.Workspace) (BrainContext, error) {
	root, ok := ws.BrainPath()
	if !ok {
		return BrainContext{}, usageErrorf(
			"this workspace has no brain repository.\n" +
				"  Create one:  vat repo new brain --role brain\n" +
				"  Adopt one:   vat brain adopt <directory>")
	}
	if !fsx.IsDir(root) {
		return BrainContext{}, usageErrorf("the brain repository is not cloned; run `vat sync`")
	}
	// The refusal belongs here rather than in each of the ten commands that
	// pass through. Checking only that the directory existed let `vat brain
	// build` write an index into a repository that is not a brain — and
	// `vat doctor`, which had advised exactly that, then reported the section
	// healthy. A directory declared as the brain is not one until it is
	// initialised.
	if !brain.IsBrain(root) {
		return BrainContext{}, usageErrorf(
			"%s is declared as the knowledge repository but has no records yet.\n"+
				"  Initialise it:  vat brain init\n"+
				"  Adopt one:      vat brain adopt <directory>", ws.Rel(root))
	}
	store, err := brain.Load(root)
	if err != nil {
		return BrainContext{}, err
	}
	return BrainContext{
		Workspace:            ws,
		Store:                store,
		Policy:               brainPolicy(ws),
		RequirePromotionGate: ws.Manifest.Policy.Brain.RequirePromotionGate,
	}, nil
}

func openStandaloneBrain(root string) (BrainContext, error) {
	if !fsx.IsDir(root) {
		return BrainContext{}, usageErrorf("%s is not a directory", root)
	}
	if !brain.IsBrain(root) {
		return BrainContext{}, usageErrorf(
			"%s is not a brain yet.\n  Initialise it:  vat --brain-dir %s brain init", root, root)
	}
	declared, err := brain.ReadMarkerPolicy(root)
	if err != nil {
		return BrainContext{}, err
	}
	policy := declared.Resolve()
	store, err := brain.Load(root)
	if err != nil {
		return BrainContext{}, err
	}
	return BrainContext{
		Store:                store,
		Policy:               policy.CheckPolicy(),
		RequirePromotionGate: policy.RequirePromotionGate,
	}, nil
}

// refuseStandalone is the one wording for an operation that needs the
// repository list a workspace manifest provides.
func refuseStandalone(operation string) error {
	return usageErrorf(
		"%s needs a workspace: a standalone brain has no %s naming the repositories evidence lives in",
		operation, manifest.FileName)
}

func brainPolicy(ws *workspace.Workspace) brain.CheckPolicy {
	return brain.CheckPolicy{
		StaleAfterDays: ws.Manifest.Policy.Brain.StaleAfterDays,
		ReviewSLADays:  ws.Manifest.Policy.Brain.ReviewSLADays,
		PromotionGate:  ws.Manifest.Policy.Gates.BrainPromote,

		RequireExternalEvidence: ws.Manifest.Policy.Brain.RequireExternalEvidence,
	}
}
