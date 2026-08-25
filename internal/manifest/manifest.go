// Package manifest owns vat.yaml: the single declaration of which repositories
// a workspace governs and under which policy. Every other package reads the
// workspace shape from here rather than hardcoding a repository list, so a new
// repository is added in exactly one place.
package manifest

import (
	"fmt"
	"sort"
	"strings"
)

// SchemaVersion is the manifest format vat writes. Older versions are read
// when vat knows how to upgrade them.
const SchemaVersion = 1

// FileName is the manifest's fixed name at the workspace root.
const FileName = "vat.yaml"

// Role classifies what a repository is canonical for. Checks, harness
// templates, and lint rules all branch on it.
type Role string

const (
	// RoleProduct is a repository that owns code and its implementation docs.
	RoleProduct Role = "product"
	// RoleBrain is the reviewed organisational-knowledge repository.
	RoleBrain Role = "brain"
	// RoleCredential is the encrypted-secrets repository. vat never reads its
	// plaintext and never prints values from it.
	RoleCredential Role = "credential"
	// RoleDocs is a documentation or site repository.
	RoleDocs Role = "docs"
	// RoleAgent is an agent's own identity and journal repository, kept
	// separate from brain on purpose.
	RoleAgent Role = "agent"
	// RoleInfra is infrastructure definitions.
	RoleInfra Role = "infra"
)

// Roles lists every valid role, in declaration order.
func Roles() []Role {
	return []Role{RoleProduct, RoleBrain, RoleCredential, RoleDocs, RoleAgent, RoleInfra}
}

// Valid reports whether r is a role vat understands.
func (r Role) Valid() bool {
	for _, known := range Roles() {
		if r == known {
			return true
		}
	}
	return false
}

// Manifest is the parsed vat.yaml.
type Manifest struct {
	Version   int       `yaml:"version" json:"version"`
	Workspace Workspace `yaml:"workspace" json:"workspace"`
	Policy    Policy    `yaml:"policy" json:"policy"`
	Repos     []Repo    `yaml:"repos" json:"repos"`
}

// Workspace holds identity and defaults that apply to every repository.
type Workspace struct {
	Name string `yaml:"name" json:"name"`
	// DefaultBranch is the branch vat fast-forwards when a repository does not
	// declare its own. Declaring it per repository is what stops a `master` or
	// `develop` repository from being silently skipped forever.
	DefaultBranch string `yaml:"default_branch" json:"default_branch"`
	// RemoteTemplate expands {name} into an origin URL for `vat repo new`.
	RemoteTemplate string `yaml:"remote_template,omitempty" json:"remote_template,omitempty"`
	// Description is free text shown in the generated workspace harness.
	Description string `yaml:"description,omitempty" json:"description,omitempty"`
}

// Policy is the machine-enforced half of the methodology. Anything expressed
// here is checked by `vat lint` or `vat doctor`; anything left to prose is not.
type Policy struct {
	Sync      SyncPolicy      `yaml:"sync" json:"sync"`
	Trust     TrustPolicy     `yaml:"trust" json:"trust"`
	Brain     BrainPolicy     `yaml:"brain" json:"brain"`
	Changeset ChangesetPolicy `yaml:"changeset" json:"changeset"`
	Gates     GatePolicy      `yaml:"gates" json:"gates"`
}

// SyncPolicy bounds what `vat sync` may do to a working tree.
type SyncPolicy struct {
	// FastForwardOnly keeps sync from ever creating a merge commit.
	FastForwardOnly bool `yaml:"fast_forward_only" json:"fast_forward_only"`
	// AllowAutostash must stay false for the methodology's guarantee that a
	// sync never discards local work to hold.
	AllowAutostash bool `yaml:"allow_autostash" json:"allow_autostash"`
	// AllowAutoPush must stay false: a local-ahead branch is a human decision.
	AllowAutoPush bool `yaml:"allow_auto_push" json:"allow_auto_push"`
	// Parallelism caps concurrent git network operations.
	Parallelism int `yaml:"parallelism" json:"parallelism"`
}

// TrustPolicy encodes the boundary between content that may carry instructions
// and content that is only ever data. It exists because an agent that reads
// many repositories plus search results is an indirect prompt-injection
// surface, and the boundary has to be written down to be enforceable.
type TrustPolicy struct {
	// Canonical sources may state facts and constrain behaviour.
	Canonical []string `yaml:"canonical" json:"canonical"`
	// SemiTrusted sources may state facts about themselves only.
	SemiTrusted []string `yaml:"semi_trusted" json:"semi_trusted"`
	// Untrusted sources are data. They never carry instructions and hold no
	// position in the harness precedence order.
	Untrusted []string `yaml:"untrusted" json:"untrusted"`
}

// BrainPolicy sets the lifecycle clock for reviewed knowledge, so the review
// queue cannot grow without bound.
type BrainPolicy struct {
	// Dir is the repository name that holds brain, empty when the workspace
	// has not adopted the knowledge layer yet.
	Repo string `yaml:"repo,omitempty" json:"repo,omitempty"`
	// StaleAfterDays demotes an active current-state claim to stale once its
	// observation is older than this.
	StaleAfterDays int `yaml:"stale_after_days" json:"stale_after_days"`
	// ReviewSLADays is how long a claim may sit in the review queue before vat
	// reports the queue itself as failing.
	ReviewSLADays int `yaml:"review_sla_days" json:"review_sla_days"`
	// RequirePromotionGate forbids an agent from writing a canonical record
	// without an explicit promotion step.
	RequirePromotionGate bool `yaml:"require_promotion_gate" json:"require_promotion_gate"`
}

// ChangesetPolicy bounds the multi-repository atomicity records.
type ChangesetPolicy struct {
	// MaxOpenDays reports a changeset left open longer than this. An open
	// changeset means repositories are mid-contract-change with no closing
	// evidence.
	MaxOpenDays int `yaml:"max_open_days" json:"max_open_days"`
	// RequireRollbackPoint refuses to verify a changeset whose repositories
	// have no recorded pre-change revision.
	RequireRollbackPoint bool `yaml:"require_rollback_point" json:"require_rollback_point"`
}

// Gate settings. A gate is either crossed by a human or not gated at all;
// there is deliberately no middle value.
const (
	// GateManual requires explicit human approval for the action.
	GateManual = "manual"
	// GateAuto allows automation to perform the action.
	GateAuto = "auto"
)

// GatePolicy separates judgement authority from mutation capability. A role
// that may decide something still needs the matching gate to act on it.
type GatePolicy struct {
	// Deploy, ExternalWrite, and BrainPromote are each "manual" or "auto".
	Deploy        string `yaml:"deploy" json:"deploy"`
	ExternalWrite string `yaml:"external_write" json:"external_write"`
	BrainPromote  string `yaml:"brain_promote" json:"brain_promote"`
}

// Repo is one governed repository.
type Repo struct {
	Name   string `yaml:"name" json:"name"`
	Origin string `yaml:"origin" json:"origin"`
	Role   Role   `yaml:"role" json:"role"`
	// Path is the directory under the workspace root; defaults to Name.
	Path string `yaml:"path,omitempty" json:"path,omitempty"`
	// Group lets commands operate on a slice of the workspace.
	Group string `yaml:"group,omitempty" json:"group,omitempty"`
	// DefaultBranch overrides the workspace default.
	DefaultBranch string `yaml:"default_branch,omitempty" json:"default_branch,omitempty"`
	// Required makes a missing clone a failure rather than a warning.
	Required bool `yaml:"required" json:"required"`
	// Access is "public" or "private"; it is metadata for humans and for the
	// lint rule that forbids publishing a private repository's paths.
	Access string `yaml:"access,omitempty" json:"access,omitempty"`
	// Checks are the canonical commands that prove this repository is healthy.
	// `vat changeset verify` runs exactly these.
	Checks []string `yaml:"checks,omitempty" json:"checks,omitempty"`
	// Archived keeps a repository in the manifest for the record while
	// excluding it from sync, status, and exec.
	Archived bool `yaml:"archived,omitempty" json:"archived,omitempty"`
	// Description is free text rendered into the workspace harness.
	Description string `yaml:"description,omitempty" json:"description,omitempty"`
}

// Dir returns the repository's directory name relative to the workspace root.
func (r Repo) Dir() string {
	if r.Path != "" {
		return r.Path
	}
	return r.Name
}

// Branch returns the branch vat fast-forwards for this repository.
func (r Repo) Branch(fallback string) string {
	if r.DefaultBranch != "" {
		return r.DefaultBranch
	}
	if fallback != "" {
		return fallback
	}
	return "main"
}

// Default returns a manifest with the policy vat recommends for a new
// workspace. Callers add repositories to it.
func Default(name string) Manifest {
	return Manifest{
		Version: SchemaVersion,
		Workspace: Workspace{
			Name:          name,
			DefaultBranch: "main",
		},
		Policy: Policy{
			Sync: SyncPolicy{
				FastForwardOnly: true,
				AllowAutostash:  false,
				AllowAutoPush:   false,
				Parallelism:     8,
			},
			Trust: TrustPolicy{
				Canonical:   []string{"brain", "credential"},
				SemiTrusted: []string{"workspace-repos"},
				Untrusted:   []string{"search-results", "embeddings", "web", "issue-comments", "model-output"},
			},
			Brain: BrainPolicy{
				StaleAfterDays:       90,
				ReviewSLADays:        30,
				RequirePromotionGate: true,
			},
			Changeset: ChangesetPolicy{
				MaxOpenDays:          14,
				RequireRollbackPoint: true,
			},
			Gates: GatePolicy{
				Deploy:        GateManual,
				ExternalWrite: GateManual,
				BrainPromote:  GateManual,
			},
		},
	}
}

// Active returns the repositories that participate in day-to-day commands,
// excluding archived ones.
func (m Manifest) Active() []Repo {
	active := make([]Repo, 0, len(m.Repos))
	for _, repo := range m.Repos {
		if !repo.Archived {
			active = append(active, repo)
		}
	}
	return active
}

// Find returns the repository with the given name.
func (m Manifest) Find(name string) (Repo, bool) {
	for _, repo := range m.Repos {
		if repo.Name == name {
			return repo, true
		}
	}
	return Repo{}, false
}

// Groups returns every distinct group name, sorted.
func (m Manifest) Groups() []string {
	seen := map[string]bool{}
	for _, repo := range m.Repos {
		if repo.Group != "" {
			seen[repo.Group] = true
		}
	}
	groups := make([]string, 0, len(seen))
	for group := range seen {
		groups = append(groups, group)
	}
	sort.Strings(groups)
	return groups
}

// BrainRepo returns the repository that holds the knowledge layer, preferring
// the explicit policy setting and falling back to the single repository whose
// role is brain.
func (m Manifest) BrainRepo() (Repo, bool) {
	if m.Policy.Brain.Repo != "" {
		return m.Find(m.Policy.Brain.Repo)
	}
	for _, repo := range m.Repos {
		if repo.Role == RoleBrain {
			return repo, true
		}
	}
	return Repo{}, false
}

// Selector filters the repositories a command acts on.
type Selector struct {
	Names          []string
	Groups         []string
	Roles          []string
	IncludeArchive bool
}

// Empty reports whether the selector would match everything.
func (s Selector) Empty() bool {
	return len(s.Names) == 0 && len(s.Groups) == 0 && len(s.Roles) == 0
}

// Select returns the repositories matching the selector, preserving manifest
// order. An empty selector matches every non-archived repository.
func (m Manifest) Select(sel Selector) ([]Repo, error) {
	pool := m.Repos
	selected := make([]Repo, 0, len(pool))
	unmatched := map[string]bool{}
	for _, name := range sel.Names {
		unmatched[name] = true
	}
	for _, repo := range pool {
		if repo.Archived && !sel.IncludeArchive {
			// An explicitly named archived repository is still selectable, so
			// that `vat repo remove <archived>` keeps working.
			if !containsFold(sel.Names, repo.Name) {
				continue
			}
		}
		if !sel.Empty() && !matches(repo, sel) {
			continue
		}
		delete(unmatched, repo.Name)
		selected = append(selected, repo)
	}
	// Always a slice, never nil: `--json` consumers iterate the result.
	if len(unmatched) > 0 {
		missing := make([]string, 0, len(unmatched))
		for name := range unmatched {
			missing = append(missing, name)
		}
		sort.Strings(missing)
		return nil, fmt.Errorf("no such repository in %s: %s", FileName, strings.Join(missing, ", "))
	}
	return selected, nil
}

func matches(repo Repo, sel Selector) bool {
	if containsFold(sel.Names, repo.Name) {
		return true
	}
	if containsFold(sel.Groups, repo.Group) {
		return true
	}
	if containsFold(sel.Roles, string(repo.Role)) {
		return true
	}
	return false
}

func containsFold(list []string, want string) bool {
	if want == "" {
		return false
	}
	for _, item := range list {
		if strings.EqualFold(item, want) {
			return true
		}
	}
	return false
}
