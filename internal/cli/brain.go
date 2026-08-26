package cli

import (
	"strings"

	"github.com/takealook97/vat/internal/brain"
	"github.com/takealook97/vat/internal/fsx"
	"github.com/takealook97/vat/internal/workspace"
)

// The knowledge command tree and the two helpers every subcommand needs:
// opening the brain repository the workspace adopted, and reading the
// lifecycle clock out of its policy.

func brainCommand() *Command {
	return &Command{
		Name:    "brain",
		Summary: "The reviewed-knowledge layer: facts with provenance and an expiry",
		Usage: "vat brain <init|new|build|check|query|review|sweep|promote|supersede|" +
			"quarantine|revoke|resolve|archive|adopt>",
		Long: `Hold what the organisation believes is true, and know when it stopped
being verified.

Code says what a system does. It does not say what the organisation is trying to
achieve, what was already tried, or why a decision was made — and those are
exactly what is lost first, and most expensively.

The brain stores one fact per file. A statement about the present carries the
repository that owns it, the exact revision it was read from, and the date it
was observed. When nobody re-checks it within the policy window, it is demoted
to stale automatically: it is not deleted, and it is not still true, it is
unverified. That single mechanism is what keeps a knowledge repository from
becoming a confident liar over a few years.`,
		Subcommands: []*Command{
			brainInitCommand(),
			brainNewCommand(),
			brainBuildCommand(),
			brainCheckCommand(),
			brainQueryCommand(),
			brainReviewCommand(),
			brainSweepCommand(),
			brainPromoteCommand(),
			brainSupersedeCommand(),
			brainQuarantineCommand(),
			brainRevokeCommand(),
			brainResolveCommand(),
			brainArchiveCommand(),
			brainAdoptCommand(),
		},
	}
}

// openBrain resolves the brain repository the workspace has adopted.
func openBrain(env *Env) (*workspace.Workspace, *brain.Store, error) {
	ws, err := env.Workspace()
	if err != nil {
		return nil, nil, err
	}
	root, ok := ws.BrainPath()
	if !ok {
		return ws, nil, usageErrorf(
			"this workspace has no brain repository.\n" +
				"  Create one:  vat repo new brain --role brain\n" +
				"  Adopt one:   vat brain adopt <directory>")
	}
	if !fsx.IsDir(root) {
		return ws, nil, usageErrorf("the brain repository is not cloned; run `vat sync`")
	}
	store, err := brain.Load(root)
	if err != nil {
		return ws, nil, err
	}
	return ws, store, nil
}

func brainPolicy(ws *workspace.Workspace) brain.CheckPolicy {
	return brain.CheckPolicy{
		StaleAfterDays: ws.Manifest.Policy.Brain.StaleAfterDays,
		ReviewSLADays:  ws.Manifest.Policy.Brain.ReviewSLADays,
	}
}

func truncate(text string, width int) string {
	runes := []rune(strings.TrimSpace(text))
	if len(runes) <= width {
		return string(runes)
	}
	return string(runes[:width-1]) + "…"
}
