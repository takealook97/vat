package doctor

import (
	"fmt"

	"github.com/takealook97/vat/internal/manifest"
)

// checkToolCurrency reports whether a newer vat has been published than the one
// running.
//
// `requires.vat` cannot answer this. It asks whether the binary is acceptable
// to the workspace, and a range like `>=0.6.1 <0.7.0` is satisfied by 0.6.1 for
// as long as that range stands — so every release inside it arrives unannounced
// to the people who wrote it.
//
// The newest version is supplied rather than looked up here, for the reason
// DriftedClaims is: answering it needs the network, and this package is one the
// command layer assembles rather than one that reaches out on its own. That
// also makes the check silent offline, which is correct — a lookup that did not
// happen is not a finding.
func checkToolCurrency(running, latest string) []Finding {
	if running == "" || latest == "" {
		return nil
	}
	// A build carrying no release version — `go install` from a branch, or a
	// local `make build` — is behind nothing, and telling its owner to upgrade
	// would name an action that does not apply to how they got it. Both parses
	// below fail on such a version, and failing to answer is the answer.
	atLeastLatest, err := manifest.ParseConstraint(">=" + latest)
	if err != nil {
		return nil
	}
	current, err := atLeastLatest.Allows(running)
	if err != nil {
		return nil
	}
	if current {
		return []Finding{{
			Section: sectionTools, Subject: "vat", Status: StatusOK,
			Detail: running + " is the newest release",
		}}
	}
	return []Finding{{
		Section: sectionTools, Subject: "vat", Status: StatusWarn,
		Detail: fmt.Sprintf("running %s; %s is published", running, latest),
		Fix:    "vat upgrade",
	}}
}
