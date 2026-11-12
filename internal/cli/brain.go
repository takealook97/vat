package cli

import (
	"strings"
)

// The knowledge command tree. Which brain a subcommand acts on, and under
// whose policy, is resolved in brain_context.go.

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

func truncate(text string, width int) string {
	runes := []rune(strings.TrimSpace(text))
	if len(runes) <= width {
		return string(runes)
	}
	return string(runes[:width-1]) + "…"
}
