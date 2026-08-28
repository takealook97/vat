package cli

import (
	"github.com/takealook97/vat/internal/changeset"
	"github.com/takealook97/vat/internal/workspace"
)

// participantTarget is the working tree a changeset participant names, and
// what the record needs to know about it.
type participantTarget struct {
	// Dir is the absolute path of the working tree.
	Dir string
	// Branch is the branch this participant ships from.
	Branch string
	// Checks are the canonical commands that prove it healthy.
	Checks []string
}

// resolveParticipant maps a participant name onto a working tree.
//
// One name resolves outside `repos:`: changeset.WorkspaceParticipant, the
// workspace root. Every other unknown name stays unresolved, or a mistyped
// repository would enrol something silently — which is the failure the manifest
// exists to make impossible.
//
// It is shared by enrolment, verification, and landing rather than repeated at
// each: three copies of this lookup is how three commands come to disagree
// about which tree a record describes.
func resolveParticipant(ws *workspace.Workspace, name string) (participantTarget, bool) {
	if name == changeset.WorkspaceParticipant {
		return participantTarget{
			Dir:    ws.Root,
			Branch: ws.Manifest.Workspace.DefaultBranch,
			Checks: ws.Manifest.Workspace.Checks,
		}, true
	}
	repo, ok := ws.Manifest.Find(name)
	if !ok {
		return participantTarget{}, false
	}
	return participantTarget{
		Dir:    ws.RepoPath(repo),
		Branch: repo.Branch(ws.Manifest.Workspace.DefaultBranch),
		Checks: repo.Checks,
	}, true
}
