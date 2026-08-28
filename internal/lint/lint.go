// Package lint turns the workspace's rules into checks that run.
//
// A rule that is only written down is a hope. The methodology this tool
// implements is mostly prose, and prose is exactly what degrades as a workspace
// grows: the repository added to the manifest but forgotten in .gitignore, the
// role body copied into two runtimes that then diverge, the claim whose
// evidence moved three months ago. Each rule here is one of those failures,
// made observable and, where it is safe, repairable.
package lint

import (
	"context"
	"fmt"
	"io/fs"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/takealook97/vat/internal/brain"
	"github.com/takealook97/vat/internal/changeset"
	"github.com/takealook97/vat/internal/fsx"
	"github.com/takealook97/vat/internal/gitx"
	"github.com/takealook97/vat/internal/manifest"
	"github.com/takealook97/vat/internal/workspace"
)

// Severity ranks a finding.
type Severity string

const (
	// SeverityError fails the command.
	SeverityError Severity = "error"
	// SeverityWarn reports without failing.
	SeverityWarn Severity = "warn"
)

// Finding is one rule violation.
type Finding struct {
	Rule     string   `json:"rule"`
	Severity Severity `json:"severity"`
	Subject  string   `json:"subject,omitempty"`
	Message  string   `json:"message"`
	Fix      string   `json:"fix,omitempty"`
	// Fixable marks a finding `vat lint --fix` can repair without judgement.
	Fixable bool `json:"fixable,omitempty"`
}

// Report is a whole lint run.
type Report struct {
	Findings []Finding `json:"findings"`
	Checked  int       `json:"rules_checked"`
}

// Errors counts findings that fail the run.
func (r Report) Errors() int {
	count := 0
	for _, finding := range r.Findings {
		if finding.Severity == SeverityError {
			count++
		}
	}
	return count
}

// Fixable counts findings that can be repaired automatically.
func (r Report) Fixable() int {
	count := 0
	for _, finding := range r.Findings {
		if finding.Fixable {
			count++
		}
	}
	return count
}

// Options configure a run.
type Options struct {
	// Offline skips rules that need to resolve a git revision.
	Offline bool
	// Now is the reference time, injected so tests are deterministic.
	Now time.Time
	// Only restricts the run to rules whose name contains one of these.
	Only []string
}

// Run evaluates every rule against the workspace.
func Run(ctx context.Context, ws *workspace.Workspace, opts Options) (Report, error) {
	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}
	// An empty slice rather than nil: --json consumers iterate the result and
	// should not have to special-case a null.
	report := Report{Findings: []Finding{}}
	add := func(findings ...Finding) {
		for _, finding := range findings {
			if !selected(finding.Rule, opts.Only) {
				continue
			}
			report.Findings = append(report.Findings, finding)
		}
	}

	// The count reflects the rules this run actually evaluated, so `--only`
	// does not claim to have checked the whole set.
	report.Checked = 0
	for _, rule := range RuleNames() {
		if selected(rule, opts.Only) {
			report.Checked++
		}
	}

	add(checkGitignore(ws)...)
	add(checkWorkspaceGit(ws)...)
	add(checkRepositories(ctx, ws)...)
	harnessFindings, err := checkHarness(ws)
	if err != nil {
		return report, err
	}
	add(harnessFindings...)
	add(checkTrustPolicy(ws)...)

	brainFindings, err := checkBrain(ctx, ws, opts, now)
	if err != nil {
		return report, err
	}
	add(brainFindings...)
	add(checkUnreferencedBrain(ws)...)

	changesetFindings, err := checkChangesets(ctx, ws, now)
	if err != nil {
		return report, err
	}
	add(changesetFindings...)

	sort.SliceStable(report.Findings, func(i, j int) bool {
		left, right := report.Findings[i], report.Findings[j]
		if left.Severity != right.Severity {
			return left.Severity == SeverityError
		}
		if left.Rule != right.Rule {
			return left.Rule < right.Rule
		}
		return left.Subject < right.Subject
	})
	return report, nil
}

func selected(rule string, only []string) bool {
	if len(only) == 0 {
		return true
	}
	for _, filter := range only {
		if strings.Contains(rule, filter) {
			return true
		}
	}
	return false
}

// RuleNames lists every rule this package can report, for documentation and for
// `vat lint --list`.
func RuleNames() []string {
	return []string{
		"workspace/gitignore-drift",
		"workspace/ignore-region-duplicated",
		"workspace/not-a-repository",
		"repo/missing",
		"repo/not-a-repository",
		"repo/submodule-uninitialised",
		"repo/outside-workspace",
		"repo/nested",
		"repo/remote-mismatch",
		"repo/remote-missing",
		"repo/credential-in-remote",
		"repo/default-branch-missing",
		"repo/checks-missing",
		"harness/workspace-missing",
		"harness/workspace-drift",
		"harness/region-duplicated",
		"harness/workspace-oversized",
		"harness/repo-missing",
		"harness/repo-drift",
		"harness/adapter-drift",
		"harness/role-metadata",
		"harness/model-ambiguous",
		"harness/skill-metadata",
		"harness/runtime-unknown",
		"harness/definition-malformed",
		"harness/adapter-orphaned",
		"policy/trust-undeclared",
		"brain/not-initialised",
		"brain/schema-newer",
		ruleUnreferencedBrain,
		"brain/generated-drift",
		"brain/projection-unmanaged",
		"brain/source-revision-drift",
		"brain/source-repo-unknown",
		"brain/source-external-governed",
		ruleViewStale,
		"changeset/open-too-long",
		"changeset/invalid",
		"changeset/closed-unlanded",
		"changeset/rollback-point-missing",
	}
}

func checkGitignore(ws *workspace.Workspace) []Finding {
	if findings := checkGitignoreRegionCount(ws); findings != nil {
		return findings
	}
	missing, err := ws.GitignoreDrift(ws.Manifest)
	if err != nil {
		return []Finding{{
			Rule: "workspace/gitignore-drift", Severity: SeverityError,
			Message: err.Error(),
		}}
	}
	if len(missing) == 0 {
		return nil
	}
	// This is the cheapest catastrophic mistake in a repo-of-repos layout: the
	// next `git add .` at the root absorbs an entire nested clone into the
	// workspace's own history.
	return []Finding{{
		Rule: "workspace/gitignore-drift", Severity: SeverityError,
		Subject: strings.Join(missing, ", "),
		Message: fmt.Sprintf("%d governed %s not excluded by .gitignore; a workspace commit would swallow them",
			len(missing), plural(len(missing), "repository is", "repositories are")),
		Fix: "vat lint --fix", Fixable: true,
	}}
}

func checkWorkspaceGit(ws *workspace.Workspace) []Finding {
	if gitx.IsRepository(ws.Root) {
		return nil
	}
	return []Finding{{
		Rule: "workspace/not-a-repository", Severity: SeverityWarn,
		Subject: ws.Rel(ws.Root),
		Message: "the workspace root is not a git repository, so the manifest and harness are not versioned",
		Fix:     "git init",
	}}
}

func checkRepositories(ctx context.Context, ws *workspace.Workspace) []Finding {
	var findings []Finding
	for _, repo := range ws.Manifest.Repos {
		if repo.Archived {
			continue
		}
		dir := ws.RepoPath(repo)
		if !fsx.IsDir(dir) {
			severity := SeverityWarn
			if repo.Required {
				severity = SeverityError
			}
			findings = append(findings, Finding{
				Rule: "repo/missing", Severity: severity, Subject: repo.Name,
				Message: "not cloned", Fix: "vat sync",
			})
			continue
		}
		// The manifest refuses a path that escapes textually and RepoPath
		// re-roots the join, but neither can see a symlink: a link inside the
		// workspace pointing out of it satisfies every string comparison while
		// every write through it lands somewhere vat may not touch. The
		// commands that create, move, and adopt a directory ask this question
		// before they act; nothing asked it of a workspace that already exists,
		// including one built by a version of vat that never asked at all.
		if !ws.Contains(dir) {
			findings = append(findings, Finding{
				Rule: "repo/outside-workspace", Severity: SeverityError, Subject: repo.Name,
				Message: fmt.Sprintf(
					"%s resolves outside the workspace, so a rendered contract or a deletion would land there instead",
					ws.Rel(dir)),
				Fix: "move the repository inside the workspace, or drop the entry with vat repo remove",
			})
			continue
		}
		// A governed repository inside another governed repository is the harm
		// workspace/gitignore-drift names, one level down: a commit in the
		// outer one swallows the inner one's whole tree and duplicates its
		// history, and the outer one reads as permanently dirty until it does.
		// Reported only when the outer repository does not already exclude it,
		// because somebody who has excluded it has thought about this and a
		// rule that fires on a correct workspace gets turned off.
		if outer, nested := enclosingRepo(ws, repo); nested && !gitx.Ignores(ctx, ws.RepoPath(outer), nestedRelative(outer, repo)) {
			findings = append(findings, Finding{
				Rule: "repo/nested", Severity: SeverityError, Subject: repo.Name,
				Message: fmt.Sprintf(
					"sits inside %s, which does not exclude it: a commit there would swallow this repository's whole tree",
					outer.Name),
				Fix: fmt.Sprintf("echo %s/ >> %s/.gitignore, or move the repository beside the others",
					nestedRelative(outer, repo), outer.Dir()),
			})
		}
		if !gitx.IsRepository(dir) {
			findings = append(findings, Finding{
				Rule: "repo/not-a-repository", Severity: SeverityError, Subject: repo.Name,
				Message: fmt.Sprintf("%s exists but holds no git repository", ws.Rel(dir)),
			})
			continue
		}
		// A submodule the clone never checked out is an empty directory that
		// every build reads as a missing dependency, and vat's clone does not
		// recurse. Without this, sync reports CURRENT, status reports clean,
		// and the canonical checks fail for a reason nothing in the tool names.
		//
		// Not repairable: checking out a submodule writes into a working tree
		// this tool does not own, and `--fix` repairs only what vat generated.
		if pending, err := gitx.UninitialisedSubmodules(ctx, dir); err == nil && len(pending) > 0 {
			findings = append(findings, Finding{
				Rule: "repo/submodule-uninitialised", Severity: SeverityWarn, Subject: repo.Name,
				Message: fmt.Sprintf("declares %s never checked out: %s",
					countOf(len(pending), "submodule", "submodules"), strings.Join(pending, ", ")),
				Fix: fmt.Sprintf("git -C %s submodule update --init --recursive", repo.Dir()),
			})
		}
		actual, err := gitx.RemoteURL(ctx, dir, "origin")
		if err != nil {
			// Nothing mismatches when there is no remote at all, and this is
			// the state `vat repo new --no-remote` deliberately leaves behind.
			// The manifest's own placeholder is not a URL, and handing it back
			// as one told the reader to create a remote that cannot be fetched.
			fix := fmt.Sprintf("git -C %s remote add origin %s",
				repo.Dir(), gitx.Redact(repo.Origin))
			if manifest.IsLocalOrigin(repo.Origin) {
				fix = fmt.Sprintf(
					"git -C %s remote add origin <url>, then record that url in %s",
					repo.Dir(), manifest.FileName)
			}
			findings = append(findings, Finding{
				Rule: "repo/remote-missing", Severity: SeverityWarn, Subject: repo.Name,
				Message: "no origin remote is configured, so it can never be fetched or pushed",
				Fix:     fix,
			})
		} else {
			// Checked before the mismatch rule rather than as part of it,
			// because NormaliseURL strips the userinfo it compares: a remote
			// carrying a token is *equal* to the plain manifest origin and this
			// loop reported nothing at all. The manifest has refused an
			// embedded credential since it was first validated, and every
			// command that takes a URL refuses one now, but a clone made before
			// those guards existed still has the token sitting in .git/config
			// where nothing looks.
			//
			// Never fixable. Rewriting a remote is the one thing this tool does
			// not do, and stripping the credential would break the push of
			// anyone who has no credential helper configured — a judgement
			// `--fix` promises not to make.
			if manifest.HasEmbeddedCredential(actual) {
				findings = append(findings, Finding{
					Rule: "repo/credential-in-remote", Severity: SeverityError, Subject: repo.Name,
					// Existence, permissions, and age only: the finding must not
					// become the one place the secret is disclosed.
					Message: "origin embeds a credential in its URL, readable by anything that can read .git/config and echoed by git's own errors",
					Fix: fmt.Sprintf("move it to a credential helper, then: git -C %s remote set-url origin %s",
						repo.Dir(), gitx.WithoutCredentials(repo.Origin)),
				})
			}
			if !gitx.SameRemote(actual, repo.Origin) {
				findings = append(findings, Finding{
					Rule: "repo/remote-mismatch", Severity: SeverityError, Subject: repo.Name,
					Message: fmt.Sprintf("origin is %s but the manifest says %s",
						gitx.Redact(actual), gitx.Redact(repo.Origin)),
				})
			}
		}
		if repo.DefaultBranch == "" && ws.Manifest.Workspace.DefaultBranch != "" {
			// A repository on master or develop, with the workspace default set
			// to main, is skipped by every sync forever and reported as
			// "on another branch" rather than as a problem.
			current, err := gitx.CurrentBranch(ctx, dir)
			if err == nil && current != "" && current != ws.Manifest.Workspace.DefaultBranch {
				findings = append(findings, Finding{
					Rule: "repo/default-branch-missing", Severity: SeverityWarn, Subject: repo.Name,
					Message: fmt.Sprintf(
						"checked out on %q while the workspace default is %q, and it declares no default_branch of its own; if %q is this repository's real default, sync skips it silently forever",
						current, ws.Manifest.Workspace.DefaultBranch, current),
					Fix: fmt.Sprintf("if that is its default, set default_branch: %s in %s; if it is a working branch, nothing to do",
						current, manifest.FileName),
				})
			}
		}
		if len(repo.Checks) == 0 && repo.Role == manifest.RoleProduct {
			findings = append(findings, Finding{
				Rule: "repo/checks-missing", Severity: SeverityWarn, Subject: repo.Name,
				Message: "declares no canonical checks, so a changeset cannot verify it",
				Fix:     fmt.Sprintf("add checks: to %s in %s", repo.Name, manifest.FileName),
			})
		}
	}
	return findings
}

// fixHarness is the command that regenerates every drifted contract.
const fixHarness = "vat harness render"

// rootHarnessBudget is the byte ceiling vat warns at for the workspace
// AGENTS.md. Agent runtimes accumulate context files from the home directory
// downward and stop once a budget is reached, so an oversized root file does
// not merely waste context — it silently truncates the per-repository contracts
// that were supposed to load after it.
const rootHarnessBudget = 12 * 1024

func checkTrustPolicy(ws *workspace.Workspace) []Finding {
	if len(ws.Manifest.Policy.Trust.Untrusted) > 0 {
		return nil
	}
	// An agent reading issue threads, fetched pages, and search results is
	// reading text strangers can write. If nothing is declared untrusted, the
	// generated harness cannot tell it which text is data.
	return []Finding{{
		Rule: "policy/trust-undeclared", Severity: SeverityWarn, Subject: "policy.trust",
		Message: "no untrusted sources declared; the harness cannot state which content is data rather than instruction",
		Fix:     fmt.Sprintf("add policy.trust.untrusted to %s", manifest.FileName),
	}}
}

// checkUnreferencedBrain reports a directory holding a scaffolded brain that
// the manifest does not point at.
//
// `vat brain init <directory>` writes wherever it is pointed, so a workspace can
// end up with a complete brain layout that no `vat brain` command can reach —
// every one of them resolves the brain through the manifest. Nothing else
// notices: brain/not-initialised fires only for a repository already declared as
// the brain, which this directory is not.
//
// The marker file is the whole test. Matching on the record directories instead
// would report any repository that happens to keep a `decisions/` folder, and a
// rule that cries wolf is worse than no rule.
func checkUnreferencedBrain(ws *workspace.Workspace) []Finding {
	declared, hasBrain := ws.BrainPath()
	var findings []Finding

	// `vat brain init <directory>` writes wherever it is pointed, including
	// somewhere nested, so a scan of the root's immediate children missed the
	// case the rule exists for.
	walkErr := filepath.WalkDir(ws.Root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			// Reporting nothing because a directory could not be read is how a
			// rule silently stops running. Say so and carry on with the rest.
			findings = append(findings, Finding{
				Rule: ruleUnreferencedBrain, Severity: SeverityWarn, Subject: ws.Rel(path),
				Message: "could not be read, so any brain layout inside it is unchecked: " + err.Error(),
			})
			return fs.SkipDir
		}
		if !entry.IsDir() {
			return nil
		}
		if name := entry.Name(); path != ws.Root && strings.HasPrefix(name, ".") {
			return fs.SkipDir
		}
		if depthBelow(ws.Root, path) > brainScanDepth {
			return fs.SkipDir
		}
		if hasBrain && sameDirectory(declared, path) {
			return fs.SkipDir
		}
		if !fsx.Exists(filepath.Join(path, brain.MarkerFile)) {
			return nil
		}
		rel := ws.Rel(path)
		fix := fmt.Sprintf("vat brain adopt %s", rel)
		if _, governed := ws.Manifest.Find(rel); !governed {
			fix = fmt.Sprintf("vat repo adopt %s, then vat brain adopt %s", rel, rel)
		}
		findings = append(findings, Finding{
			Rule: ruleUnreferencedBrain, Severity: SeverityWarn, Subject: rel,
			Message: "holds a brain layout but is not the workspace's knowledge repository; no vat brain command reaches it",
			Fix:     fix,
		})
		// A brain contains no second brain, and descending into one would walk
		// every record directory for nothing.
		return fs.SkipDir
	})
	if walkErr != nil {
		findings = append(findings, Finding{
			Rule: ruleUnreferencedBrain, Severity: SeverityWarn, Subject: ws.Rel(ws.Root),
			Message: "the workspace could not be scanned for unreachable brain layouts: " + walkErr.Error(),
		})
	}
	return findings
}

// ruleUnreferencedBrain names the rule the walk below reports at each of its
// several exits.
const ruleUnreferencedBrain = "brain/unreferenced"

// brainScanDepth bounds the walk. A brain nested deeper than this is not a
// layout anybody arrived at by accident, and an unbounded walk of a workspace
// of clones reads every source tree in it.
const brainScanDepth = 3

func depthBelow(root, path string) int {
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == "." {
		return 0
	}
	return len(strings.Split(rel, string(filepath.Separator)))
}

// sameDirectory compares two paths that are both inside the workspace,
// resolving symlinks so the declared brain is not reported as unreachable when
// the workspace root is reached through one.
func sameDirectory(a, b string) bool {
	if filepath.Clean(a) == filepath.Clean(b) {
		return true
	}
	resolvedA, errA := filepath.EvalSymlinks(a)
	resolvedB, errB := filepath.EvalSymlinks(b)
	return errA == nil && errB == nil && resolvedA == resolvedB
}

func checkBrain(ctx context.Context, ws *workspace.Workspace, opts Options, now time.Time) ([]Finding, error) {
	root, ok := ws.BrainPath()
	if !ok || !fsx.IsDir(root) {
		return nil, nil
	}
	// A repository declared as the brain but never initialised has nothing to
	// check yet. Reporting its missing projections as drift would greet every
	// new workspace with two failures it cannot act on.
	if !brain.IsBrain(root) {
		return []Finding{{
			Rule: "brain/not-initialised", Severity: SeverityWarn, Subject: ws.Rel(root),
			Message: "declared as the knowledge repository but has no records yet",
			Fix:     "vat brain init",
		}}, nil
	}
	// A brain written against a newer schema is one this build cannot judge.
	// `vat brain check` refuses to for the reason its own comment gives —
	// reporting on fields it cannot see would make the records look clean
	// because half of what governs them was invisible — and lint read it
	// silently and certified it, in the command this project puts in CI.
	if declared, ok := brain.DeclaredSchema(root); ok && declared > brain.SchemaVersion {
		return []Finding{{
			Rule: "brain/schema-newer", Severity: SeverityError, Subject: ws.Rel(root),
			Message: fmt.Sprintf(
				"written against schema %d; this build understands %d, so these checks cannot say whether it is sound",
				declared, brain.SchemaVersion),
			Fix: "upgrade vat, or run these checks with the build that wrote it",
		}}, nil
	}

	store, err := brain.Load(root)
	if err != nil {
		return nil, err
	}

	var findings []Finding
	// Reported before drift, and never as drift: vat refuses to write these,
	// so offering `vat brain build` would name a repair that does nothing.
	// This is the state a knowledge repository older than vat starts in.
	unmanaged, err := brain.Unmanaged(root)
	if err != nil {
		return nil, err
	}
	for _, name := range unmanaged {
		findings = append(findings, Finding{
			Rule: "brain/projection-unmanaged", Severity: SeverityError, Subject: name,
			Message: "occupies the name of a generated projection but was not written by vat",
			Fix:     "move or delete it, then `vat brain build`",
		})
	}
	drifted, err := brain.Drift(store, now)
	if err != nil {
		return nil, err
	}
	for _, name := range drifted {
		findings = append(findings, Finding{
			Rule: "brain/generated-drift", Severity: SeverityError, Subject: name,
			Message: "generated projection does not match the atomic records",
			Fix:     "vat brain build", Fixable: true,
		})
	}

	// Before the offline gate: dating a file needs `git log`, which reads the
	// clone and nothing else.
	findings = append(findings, checkViews(ctx, root, ws.Manifest.Policy.Brain.ReviewSLADays)...)

	if opts.Offline {
		return findings, nil
	}
	findings = append(findings, checkSourceRevisions(ctx, ws, store)...)
	return findings, nil
}

// checkSourceRevisions is the rule the knowledge layer exists for.
//
// A claim records the revision it was read from. When the owning repository has
// moved on, the claim is not automatically false — a typo commit changes
// nothing — but it is no longer known to be true. Reporting the drift turns
// "someone should re-check this eventually" into a specific, dated item.
func checkSourceRevisions(ctx context.Context, ws *workspace.Workspace, store *brain.Store) []Finding {
	var findings []Finding
	for _, record := range store.CurrentStateClaims() {
		if record.Status != brain.StatusActive {
			continue
		}
		repoName, revision, _, ok := record.SourceParts()
		if !ok {
			continue
		}
		repo, known := ws.Manifest.Find(repoName)
		if record.SourceExternal {
			// Declared as a system this workspace does not govern. Nothing here
			// can resolve its revision, which is the point of saying so.
			if known {
				findings = append(findings, Finding{
					Rule: "brain/source-external-governed", Severity: SeverityError, Subject: record.ID,
					Message: fmt.Sprintf("source_external is set, but %q is governed by %s",
						repoName, manifest.FileName),
					Fix: "remove source_external, or point source_ref at the system that is actually external",
				})
			}
			continue
		}
		if !known {
			findings = append(findings, Finding{
				Rule: "brain/source-repo-unknown", Severity: SeverityWarn, Subject: record.ID,
				Message: fmt.Sprintf("source_ref names %q, which is not in %s", repoName, manifest.FileName),
				// The remedy this rule used to leave unstated was the one people
				// took: adding the system to the roster, which stops the warning
				// by making the workspace claim to sync, diagnose, and ship a
				// repository it does not own.
				Fix: "set source_external: true if it is deliberately outside this workspace; enrol it only if the workspace really governs it",
			})
			continue
		}
		dir := ws.RepoPath(repo)
		if !gitx.IsRepository(dir) {
			continue
		}
		if !gitx.RevisionExists(ctx, dir, revision) {
			findings = append(findings, Finding{
				Rule: "brain/source-revision-drift", Severity: SeverityWarn, Subject: record.ID,
				Message: fmt.Sprintf("source revision %s no longer resolves in %s", short(revision), repoName),
			})
			continue
		}
		head, err := gitx.HeadRevision(ctx, dir)
		if err != nil || strings.HasPrefix(head, revision) {
			continue
		}
		count, err := gitx.Run(ctx, dir, "rev-list", "--count", revision+"..HEAD")
		if err != nil {
			continue
		}
		if count == "0" {
			continue
		}
		findings = append(findings, Finding{
			Rule: "brain/source-revision-drift", Severity: SeverityWarn, Subject: record.ID,
			Message: fmt.Sprintf("%s has moved %s commits since this was observed at %s; re-check, do not assume it broke",
				repoName, count, short(revision)),
			Fix: fmt.Sprintf("vat brain review  # then re-verify %s", record.ID),
		})
	}
	return findings
}

// checkRollbackPoints reports a recorded return point the repository no longer
// holds.
//
// It is the one field in a changeset that cannot be reconstructed: every other
// value can be read back off git, and the revision a repository stood at before
// the change began cannot, which is why it is captured at enrolment. A rewritten
// history leaves the record asserting a way back that is gone, in exactly the
// voice of one that is there. The knowledge layer has had this check since it
// existed — brain/source-revision-drift — and the completion layer, whose whole
// promise is the return point, did not.
func checkRollbackPoints(ctx context.Context, ws *workspace.Workspace, set changeset.Changeset) []Finding {
	var findings []Finding
	for _, participant := range set.Repositories {
		if participant.RollbackPoint == "" {
			// Absent is changeset/invalid's finding, under the workspace's own
			// policy. Reporting it twice says nothing new.
			continue
		}
		repo, governed := ws.Manifest.Find(participant.Name)
		if !governed {
			continue
		}
		dir := ws.RepoPath(repo)
		if !gitx.IsRepository(dir) {
			// A repository that is not on this machine says nothing about
			// whether its history still holds the revision. Reporting it would
			// fail lint for every changeset naming a clone somebody has not
			// made yet.
			continue
		}
		if gitx.RevisionExists(ctx, dir, participant.RollbackPoint) {
			continue
		}
		findings = append(findings, Finding{
			Rule: "changeset/rollback-point-missing", Severity: SeverityError,
			Subject: set.ID + " · " + participant.Name,
			Message: fmt.Sprintf("return point %s is not in this repository any more, so the recorded way back does not exist",
				short(participant.RollbackPoint)),
			Fix: "recover the revision, or record why the way back was lost",
		})
	}
	return findings
}

func checkChangesets(ctx context.Context, ws *workspace.Workspace, now time.Time) ([]Finding, error) {
	sets, err := changeset.LoadAll(ws.Root)
	if err != nil {
		return nil, err
	}
	policy := ws.Manifest.Policy.Changeset
	var findings []Finding
	for _, set := range sets {
		findings = append(findings, checkRollbackPoints(ctx, ws, set)...)
		for _, problem := range changeset.Validate(set, policy.RequireRollbackPoint) {
			findings = append(findings, Finding{
				Rule: "changeset/invalid", Severity: SeverityError, Subject: set.ID,
				Message: problem,
			})
		}
		// `--force` exists because a real workspace sometimes has to close a
		// record the tool cannot fully confirm. What it must not do is make
		// that indistinguishable from a change that actually shipped.
		//
		// The test is the recorded waiver, not absent landing evidence. Absence
		// also describes every changeset closed before vat recorded landing at
		// all, and keying on it reported the whole history of every upgrading
		// workspace with nothing anybody could do about it. There is no fix
		// line for the same reason: `vat ship` refuses a closed changeset, and
		// naming a command that will not run is the defect this rule is about.
		if set.Status == changeset.StatusClosed && set.LandingWaived {
			var unlanded []string
			for _, participant := range set.Repositories {
				if !participant.Landed() {
					unlanded = append(unlanded, participant.Name)
				}
			}
			findings = append(findings, Finding{
				Rule: "changeset/closed-unlanded", Severity: SeverityWarn, Subject: set.ID,
				Message: fmt.Sprintf(
					"closed without landing evidence for %s; the gate was waived, not met",
					strings.Join(unlanded, ", ")),
			})
		}
		if !set.Status.Open() || policy.MaxOpenDays <= 0 {
			continue
		}
		if age := set.AgeDays(now); age > policy.MaxOpenDays {
			findings = append(findings, Finding{
				Rule: "changeset/open-too-long", Severity: SeverityWarn, Subject: set.ID,
				Message: fmt.Sprintf("open for %d days, past the %d-day limit; repositories are mid-contract-change with no closing evidence",
					age, policy.MaxOpenDays),
			})
		}
	}
	return findings, nil
}

func short(revision string) string {
	if len(revision) > 8 {
		return revision[:8]
	}
	return revision
}

func plural(count int, singular, many string) string {
	if count == 1 {
		return singular
	}
	return many
}

// enclosingRepo returns the governed repository whose directory contains this
// one, if any.
func enclosingRepo(ws *workspace.Workspace, inner manifest.Repo) (manifest.Repo, bool) {
	innerDir := path.Clean(filepath.ToSlash(inner.Dir()))
	for _, outer := range ws.Manifest.Repos {
		outerDir := path.Clean(filepath.ToSlash(outer.Dir()))
		if outerDir == innerDir {
			continue
		}
		if strings.HasPrefix(innerDir, outerDir+"/") {
			return outer, true
		}
	}
	return manifest.Repo{}, false
}

// nestedRelative is the inner repository's path as the outer one sees it.
func nestedRelative(outer, inner manifest.Repo) string {
	outerDir := path.Clean(filepath.ToSlash(outer.Dir()))
	innerDir := path.Clean(filepath.ToSlash(inner.Dir()))
	return strings.TrimPrefix(innerDir, outerDir+"/")
}

// checkGitignoreRegionCount reports a second managed region, and reports it
// instead of drift: drift is about the region vat maintains, and this is about
// the one it does not. The last matching pattern in a .gitignore decides, so
// the abandoned copy overrides the maintained one — `vat repo remove` reports
// success while the tree it stopped governing stays invisible to git.
//
// Not repairable. Which region is the real one is a judgement, and the
// abandoned one may hold something a person put there.
func checkGitignoreRegionCount(ws *workspace.Workspace) []Finding {
	content, exists, err := fsx.ReadFileIfExists(ws.GitignorePath())
	if err != nil || !exists {
		// An unreadable .gitignore is the drift check's error to report, so
		// that one failure is not reported twice under two rule names.
		return nil
	}
	if workspace.CountGitignoreRegions(string(content)) <= 1 {
		return nil
	}
	return []Finding{{
		Rule: "workspace/ignore-region-duplicated", Severity: SeverityError, Subject: ".gitignore",
		Message: "more than one managed region; vat maintains the first and never looks at the rest, " +
			"and the last matching pattern in a .gitignore is the one that decides",
		Fix: "delete the regions vat does not maintain, keeping any rules of your own inside them",
	}}
}

// countOf renders a count with the right noun, so a finding reads as a sentence
// rather than as a number followed by a plural that does not agree with it.
func countOf(n int, singular, plural string) string {
	if n == 1 {
		return "1 " + singular
	}
	return fmt.Sprintf("%d %s", n, plural)
}
