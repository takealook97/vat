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
	add(checkUnreferencedBrain(ws)...)

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
		"repo/outside-workspace",
		"repo/remote-mismatch",
		"repo/remote-missing",
		"repo/credential-in-remote",
		"repo/default-branch-missing",
		"repo/checks-missing",
		"harness/workspace-missing",
		"harness/workspace-drift",
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
		ruleUnreferencedBrain,
		"brain/generated-drift",
		"brain/source-revision-drift",
		"brain/source-repo-unknown",
		"changeset/open-too-long",
		"changeset/invalid",
		"changeset/closed-unlanded",
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

	roles, malformedRoles, err := harness.LoadRoles(ws.Root)
	if err != nil {
		return nil, err
	}
	findings = append(findings, malformedDefinitions(malformedRoles)...)
	for _, role := range roles {
		if strings.TrimSpace(role.Description) == "" {
			findings = append(findings, Finding{
				Rule: "harness/role-metadata", Severity: SeverityWarn, Subject: role.Name,
				Message: "role has no description, so runtime adapters cannot advertise it",
			})
		}
		// A model name belongs to one vendor. Copying `model: opus` into a
		// Codex adapter produced a file naming a model Codex cannot resolve —
		// generated by the very tool whose job is to stop two runtimes from
		// drifting apart. No adapter names a model it cannot honour now, which
		// makes the role's declaration silently unused until it is split.
		findings = append(findings, unknownRuntimes(role.Name, "role", role.Runtimes, harness.RuntimeNames())...)
		if role.ModelIsAmbiguous() {
			findings = append(findings, Finding{
				Rule: "harness/model-ambiguous", Severity: SeverityWarn, Subject: role.Name,
				Message: fmt.Sprintf("one model for %s; a model name belongs to a single vendor, so no adapter can honour it",
					strings.Join(role.TargetedRuntimes(), " and ")),
				Fix: fmt.Sprintf("replace model: with a models: map, one entry per runtime, in %s/%s.md",
					harness.RolesDir, role.Name),
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
			Message: "runtime adapter no longer matches its role definition in " + harness.RolesDir,
			Fix:     fixHarness, Fixable: true,
		})
	}

	skills, malformedSkills, err := harness.LoadSkills(ws.Root)
	if err != nil {
		return nil, err
	}
	findings = append(findings, malformedDefinitions(malformedSkills)...)
	for _, skill := range skills {
		findings = append(findings, unknownRuntimes(skill.Name, "skill", skill.Runtimes, harness.SkillRuntimeNames())...)
		// A skill with no description is a skill no runtime can offer. It is
		// on disk, it is generated into every adapter, and it is invisible to
		// the agent that needed it.
		if strings.TrimSpace(skill.Description) == "" {
			findings = append(findings, Finding{
				Rule: "harness/skill-metadata", Severity: SeverityWarn, Subject: skill.Name,
				Message: "skill has no description, so no runtime can advertise it",
				Fix:     fmt.Sprintf("add description: to %s/%s/%s", harness.SkillsDir, skill.Dir, harness.SkillFile),
			})
		}
	}
	skillDrift, err := harness.SkillAdapterDrift(ws.Root, skills)
	if err != nil {
		return nil, err
	}
	for _, path := range skillDrift {
		findings = append(findings, Finding{
			Rule: "harness/adapter-drift", Severity: SeverityWarn, Subject: path,
			Message: "runtime adapter no longer matches its skill definition in " + harness.SkillsDir,
			Fix:     fixHarness, Fixable: true,
		})
	}

	// Deliberately not fixable. The repair is deleting a file, and vat does not
	// delete: the adapter may be the only remaining copy of a definition
	// somebody removed by accident, and a tool that tidies it away turns a
	// recoverable mistake into a silent one. The finding names the file.
	orphans, err := harness.OrphanedAdapters(ws.Root, roles, skills)
	if err != nil {
		return nil, err
	}
	for _, path := range orphans {
		findings = append(findings, Finding{
			Rule: "harness/adapter-orphaned", Severity: SeverityWarn, Subject: path,
			Message: "generated adapter for a definition that no longer exists; a session still " +
				"loads it and it points at a missing file",
			Fix: "restore the definition it was generated from, or delete " + path,
		})
	}
	return findings, nil
}

// malformedDefinitions reports a role or skill file that could not be read.
//
// Aborting the load instead meant one unparseable file withdrew the adapters of
// every other definition beside it, and reported only the first problem — so a
// second typo was invisible until the first was fixed.
func malformedDefinitions(malformed []harness.Malformed) []Finding {
	findings := make([]Finding, 0, len(malformed))
	for _, entry := range malformed {
		findings = append(findings, Finding{
			Rule: "harness/definition-malformed", Severity: SeverityError, Subject: entry.Path,
			Message: entry.Problem,
		})
	}
	return findings
}

// unknownRuntimes reports a runtime name no adapter is generated for.
//
// `runtimes:` decides which adapters exist, and a value that selects none —
// a typo for `claude`, or a runtime vat does not support yet — silently
// produces nothing. Nothing else notices: there is no adapter, so there is no
// drift; the metadata is present, so role-metadata passes; and a bare `model`
// binds to nothing, so model-ambiguous passes too. The definition sits on disk,
// inert, while every diagnostic reports the harness healthy.
//
// The supported list is a parameter because it is not the same for both kinds.
// Codex is a runtime vat generates a role adapter for and no skill adapter at
// all, so `runtimes: [codex]` is a correctly spelled name on a role and an
// inert definition on a skill. Checking skills against the role list is how
// that case went unreported: the rule documented as catching a value that
// generates no adapter was reading the wrong list to decide.
func unknownRuntimes(subject, kind string, declared, supported []string) []Finding {
	var findings []Finding
	for _, name := range declared {
		known := false
		for _, runtime := range supported {
			if strings.EqualFold(name, runtime) {
				known = true
				break
			}
		}
		if known {
			continue
		}
		findings = append(findings, Finding{
			Rule: "harness/runtime-unknown", Severity: SeverityWarn, Subject: subject,
			Message: fmt.Sprintf("declares runtime %q, which generates no %s adapter; it is one of %s or nothing at all",
				name, kind, strings.Join(supported, ", ")),
			Fix: "correct the runtimes: list, or drop it to target every runtime",
		})
	}
	return findings
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
