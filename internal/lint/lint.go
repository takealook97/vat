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
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/takealook97/vat/internal/brain"
	"github.com/takealook97/vat/internal/changeset"
	"github.com/takealook97/vat/internal/fsx"
	"github.com/takealook97/vat/internal/gitx"
	"github.com/takealook97/vat/internal/harness"
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

	changesetFindings, err := checkChangesets(ws, now)
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
		"workspace/not-a-repository",
		"repo/missing",
		"repo/not-a-repository",
		"repo/remote-mismatch",
		"repo/remote-missing",
		"repo/default-branch-missing",
		"repo/checks-missing",
		"harness/workspace-missing",
		"harness/workspace-drift",
		"harness/workspace-oversized",
		"harness/repo-missing",
		"harness/repo-drift",
		"harness/adapter-drift",
		"harness/role-metadata",
		"policy/trust-undeclared",
		"brain/not-initialised",
		"brain/generated-drift",
		"brain/source-revision-drift",
		"brain/source-repo-unknown",
		"changeset/open-too-long",
		"changeset/invalid",
	}
}

func checkGitignore(ws *workspace.Workspace) []Finding {
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
		if !gitx.IsRepository(dir) {
			findings = append(findings, Finding{
				Rule: "repo/not-a-repository", Severity: SeverityError, Subject: repo.Name,
				Message: fmt.Sprintf("%s exists but holds no git repository", ws.Rel(dir)),
			})
			continue
		}
		actual, err := gitx.RemoteURL(ctx, dir, "origin")
		if err != nil {
			// Nothing mismatches when there is no remote at all, and this is
			// the state `vat repo new --no-remote` deliberately leaves behind.
			findings = append(findings, Finding{
				Rule: "repo/remote-missing", Severity: SeverityWarn, Subject: repo.Name,
				Message: "no origin remote is configured, so it can never be fetched or pushed",
				Fix: fmt.Sprintf("git -C %s remote add origin %s",
					repo.Dir(), gitx.Redact(repo.Origin)),
			})
		} else if !gitx.SameRemote(actual, repo.Origin) {
			findings = append(findings, Finding{
				Rule: "repo/remote-mismatch", Severity: SeverityError, Subject: repo.Name,
				Message: fmt.Sprintf("origin is %s but the manifest says %s",
					gitx.Redact(actual), gitx.Redact(repo.Origin)),
			})
		}
		if repo.DefaultBranch == "" && ws.Manifest.Workspace.DefaultBranch != "" {
			// A repository on master or develop, with the workspace default set
			// to main, is skipped by every sync forever and reported as
			// "on another branch" rather than as a problem.
			current, err := gitx.CurrentBranch(ctx, dir)
			if err == nil && current != "" && current != ws.Manifest.Workspace.DefaultBranch {
				findings = append(findings, Finding{
					Rule: "repo/default-branch-missing", Severity: SeverityWarn, Subject: repo.Name,
					Message: fmt.Sprintf("checked out on %q but declares no default_branch, so sync will skip it silently", current),
					Fix:     fmt.Sprintf("set default_branch: %s in %s", current, manifest.FileName),
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

func checkHarness(ws *workspace.Workspace) ([]Finding, error) {
	var findings []Finding

	rootPath := ws.Path("AGENTS.md")
	content, exists, err := fsx.ReadFileIfExists(rootPath)
	if err != nil {
		return nil, err
	}
	region := harness.RenderWorkspace(ws.Manifest)
	switch {
	case !exists:
		findings = append(findings, Finding{
			Rule: "harness/workspace-missing", Severity: SeverityError, Subject: "AGENTS.md",
			Message: "the workspace has no agent contract",
			Fix:     fixHarness, Fixable: true,
		})
	case !harness.RegionMatches(string(content), region):
		findings = append(findings, Finding{
			Rule: "harness/workspace-drift", Severity: SeverityError, Subject: "AGENTS.md",
			Message: "the generated region no longer matches the manifest",
			Fix:     fixHarness, Fixable: true,
		})
	}
	if exists && len(content) > rootHarnessBudget {
		findings = append(findings, Finding{
			Rule: "harness/workspace-oversized", Severity: SeverityWarn, Subject: "AGENTS.md",
			Message: fmt.Sprintf("%d bytes; past roughly %d KiB a runtime may stop loading the per-repository contracts below it. Keep this file a map, not a copy",
				len(content), rootHarnessBudget/1024),
		})
	}

	for _, repo := range ws.Manifest.Repos {
		if repo.Archived || !fsx.IsDir(ws.RepoPath(repo)) {
			continue
		}
		path := filepath.Join(ws.RepoPath(repo), "AGENTS.md")
		repoContent, repoExists, err := fsx.ReadFileIfExists(path)
		if err != nil {
			return nil, err
		}
		repoRegion := harness.RenderRepo(ws.Manifest, repo)
		if !repoExists {
			findings = append(findings, Finding{
				Rule: "harness/repo-missing", Severity: SeverityWarn, Subject: repo.Name,
				Message: "has no AGENTS.md, so a session opened inside it sees no contract",
				Fix:     fixHarness, Fixable: true,
			})
			continue
		}
		if !harness.RegionMatches(string(repoContent), repoRegion) {
			findings = append(findings, Finding{
				Rule: "harness/repo-drift", Severity: SeverityWarn, Subject: repo.Name,
				Message: "the generated region no longer matches the manifest",
				Fix:     fixHarness, Fixable: true,
			})
		}
	}

	roles, err := harness.LoadRoles(ws.Root)
	if err != nil {
		return nil, err
	}
	for _, role := range roles {
		if strings.TrimSpace(role.Description) == "" {
			findings = append(findings, Finding{
				Rule: "harness/role-metadata", Severity: SeverityWarn, Subject: role.Name,
				Message: "role has no description, so runtime adapters cannot advertise it",
			})
		}
	}
	drifted, err := harness.AdapterDrift(ws.Root, roles)
	if err != nil {
		return nil, err
	}
	for _, path := range drifted {
		findings = append(findings, Finding{
			Rule: "harness/adapter-drift", Severity: SeverityWarn, Subject: path,
			Message: "runtime adapter no longer matches its role definition in .agents/roles",
			Fix:     fixHarness, Fixable: true,
		})
	}
	return findings, nil
}

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
	store, err := brain.Load(root)
	if err != nil {
		return nil, err
	}

	var findings []Finding
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
		if !known {
			findings = append(findings, Finding{
				Rule: "brain/source-repo-unknown", Severity: SeverityWarn, Subject: record.ID,
				Message: fmt.Sprintf("source_ref names %q, which is not in %s", repoName, manifest.FileName),
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

func checkChangesets(ws *workspace.Workspace, now time.Time) ([]Finding, error) {
	sets, err := changeset.LoadAll(ws.Root)
	if err != nil {
		return nil, err
	}
	policy := ws.Manifest.Policy.Changeset
	var findings []Finding
	for _, set := range sets {
		for _, problem := range changeset.Validate(set, policy.RequireRollbackPoint) {
			findings = append(findings, Finding{
				Rule: "changeset/invalid", Severity: SeverityError, Subject: set.ID,
				Message: problem,
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
