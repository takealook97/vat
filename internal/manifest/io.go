package manifest

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/takealook97/vat/internal/fsx"
)

// ErrNotFound is returned when no manifest exists at the given path.
var ErrNotFound = errors.New("no vat.yaml found")

// Load reads and validates the manifest at path.
func Load(path string) (Manifest, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Manifest{}, fmt.Errorf("%w at %s", ErrNotFound, path)
	}
	if err != nil {
		return Manifest{}, fmt.Errorf("read %s: %w", path, err)
	}
	return Parse(data)
}

// Parse decodes manifest bytes, applies defaults, and validates the result.
// Unknown fields are rejected so a typo in a policy key fails loudly instead of
// silently disabling the rule it was meant to configure.
func Parse(data []byte) (Manifest, error) {
	var parsed Manifest
	decoder := yaml.NewDecoder(strings.NewReader(string(data)))
	decoder.KnownFields(true)
	if err := decoder.Decode(&parsed); err != nil {
		return Manifest{}, fmt.Errorf("parse %s: %w", FileName, err)
	}
	normalised := withDefaults(parsed)
	if err := Validate(normalised); err != nil {
		return Manifest{}, err
	}
	return normalised, nil
}

// withDefaults returns a copy of m with unset fields filled in. It never
// mutates its argument.
func withDefaults(m Manifest) Manifest {
	out := m
	if out.Version == 0 {
		out.Version = SchemaVersion
	}
	if out.Workspace.DefaultBranch == "" {
		out.Workspace.DefaultBranch = "main"
	}
	if out.Policy.Sync.FastForwardOnly == nil {
		out.Policy.Sync.FastForwardOnly = boolPtr(true)
	}
	if out.Policy.Sync.Parallelism <= 0 {
		out.Policy.Sync.Parallelism = 8
	}
	if out.Policy.Brain.StaleAfterDays <= 0 {
		out.Policy.Brain.StaleAfterDays = 90
	}
	if out.Policy.Brain.ReviewSLADays <= 0 {
		out.Policy.Brain.ReviewSLADays = 30
	}
	if out.Policy.Changeset.MaxOpenDays <= 0 {
		out.Policy.Changeset.MaxOpenDays = 14
	}
	if out.Policy.Gates.Deploy == "" {
		out.Policy.Gates.Deploy = GateManual
	}
	if out.Policy.Gates.ExternalWrite == "" {
		out.Policy.Gates.ExternalWrite = GateManual
	}
	if out.Policy.Gates.BrainPromote == "" {
		out.Policy.Gates.BrainPromote = GateManual
	}
	repos := make([]Repo, len(out.Repos))
	for i, repo := range out.Repos {
		if repo.Role == "" {
			repo.Role = RoleProduct
		}
		repos[i] = repo
	}
	out.Repos = repos
	return out
}

// Validate reports every structural problem in a manifest at once, so a user
// editing vat.yaml by hand sees the full list rather than one error per run.
func Validate(m Manifest) error {
	var problems []string
	if m.Version > SchemaVersion {
		problems = append(problems, fmt.Sprintf(
			"version %d is newer than this vat understands (%d); upgrade vat",
			m.Version, SchemaVersion))
	}
	if strings.TrimSpace(m.Workspace.Name) == "" {
		problems = append(problems, "workspace.name is required")
	}
	// A template without the placeholder is not a template: every repository
	// created from it was handed the same origin, and two repositories sharing
	// an upstream fetch and push over each other with nothing reporting it.
	if template := strings.TrimSpace(m.Workspace.RemoteTemplate); template != "" &&
		!strings.Contains(template, "{name}") {
		problems = append(problems, fmt.Sprintf(
			"workspace.remote_template %q has no {name}; every repository would be given the same origin",
			template))
	}
	seenNames := map[string]bool{}
	seenPaths := map[string]bool{}
	// Two entries may share an upstream when they track different branches --
	// that is a worktree-per-branch layout. Sharing an upstream *and* a branch
	// is not a layout: they fetch and push over each other, and nothing else
	// here would report it.
	seenUpstreams := map[string]string{}
	for i, repo := range m.Repos {
		where := fmt.Sprintf("repos[%d]", i)
		if repo.Name != "" {
			where = fmt.Sprintf("repos[%d] (%s)", i, repo.Name)
		}
		switch {
		case strings.TrimSpace(repo.Name) == "":
			problems = append(problems, where+": name is required")
		case ValidateRepoName(repo.Name) != nil:
			problems = append(problems, where+": "+ValidateRepoName(repo.Name).Error())
		case seenNames[repo.Name]:
			problems = append(problems, where+": duplicate name")
		}
		seenNames[repo.Name] = true

		if strings.TrimSpace(repo.Origin) == "" {
			problems = append(problems, where+": origin is required")
		} else if HasEmbeddedCredential(repo.Origin) {
			// vat.yaml is committed. A token pasted into an origin would be
			// published by the next `git push` of the workspace root, and the
			// only place vat could report it is a message it must not print.
			problems = append(problems, where+
				": origin embeds a credential; store it in your git credential helper and record the plain URL")
		}
		if !repo.Role.Valid() {
			problems = append(problems, fmt.Sprintf("%s: unknown role %q (valid: %s)",
				where, repo.Role, joinRoleNames()))
		}
		dir := repo.Dir()
		if seenPaths[dir] {
			problems = append(problems, fmt.Sprintf("%s: two repositories resolve to the same directory %q", where, dir))
		}
		seenPaths[dir] = true

		if origin := strings.TrimSpace(repo.Origin); origin != "" {
			upstream := origin + "#" + repo.Branch(m.Workspace.DefaultBranch)
			if first, clash := seenUpstreams[upstream]; clash {
				problems = append(problems, fmt.Sprintf(
					"%s: shares an origin and a branch with %s; they would fetch and push over each other",
					where, first))
			} else {
				seenUpstreams[upstream] = repo.Name
			}
		}
		if !containedPath(dir) {
			problems = append(problems, fmt.Sprintf(
				"%s: path %q must stay inside the workspace", where, dir))
		}
		if repo.Access != "" && repo.Access != "public" && repo.Access != "private" {
			problems = append(problems, where+`: access must be "public" or "private"`)
		}
	}
	// These three are stated as guarantees in the methodology and the sync
	// implementation provides them unconditionally: it never merges, never
	// stashes, and never pushes. Nothing read the fields, so a workspace could
	// declare the opposite and be told it was fine -- a rule this tool states
	// and does not check is the exact failure it exists to prevent.
	if m.Policy.Sync.FastForwardOnly != nil && !*m.Policy.Sync.FastForwardOnly {
		problems = append(problems, "policy.sync.fast_forward_only must be true; sync never creates a merge commit")
	}
	if m.Policy.Sync.AllowAutostash {
		problems = append(problems, "policy.sync.allow_autostash must be false; sync never touches a dirty working tree")
	}
	if m.Policy.Sync.AllowAutoPush {
		problems = append(problems, "policy.sync.allow_auto_push must be false; publishing a local-ahead branch is a human decision")
	}
	if count := countRole(m, RoleBrain); count > 1 {
		problems = append(problems, "at most one repository may have role \"brain\"")
	}
	if m.Policy.Brain.Repo != "" {
		if _, ok := m.Find(m.Policy.Brain.Repo); !ok {
			problems = append(problems, fmt.Sprintf(
				"policy.brain.repo %q is not in repos", m.Policy.Brain.Repo))
		}
	}
	for name, value := range map[string]string{
		"policy.gates.deploy":         m.Policy.Gates.Deploy,
		"policy.gates.external_write": m.Policy.Gates.ExternalWrite,
		"policy.gates.brain_promote":  m.Policy.Gates.BrainPromote,
	} {
		if value != GateManual && value != GateAuto {
			problems = append(problems, fmt.Sprintf(`%s must be "manual" or "auto", got %q`, name, value))
		}
	}
	if len(problems) == 0 {
		return nil
	}
	return fmt.Errorf("%s is invalid:\n  - %s", FileName, strings.Join(problems, "\n  - "))
}

// containedPath reports whether a repository directory stays inside the
// workspace once the path is normalised.
//
// Checking for a leading ".." is not enough: "sub/../../outside" has no such
// prefix but resolves above the root, and everything downstream then operates
// there — cloning, writing a harness, and, with `repo remove --delete`,
// deleting it.
//
// The comparison is done in one slash-separated form on purpose. A manifest is
// committed and read on every machine, so a path rejected on one platform has
// to be rejected on all of them — and filepath's helpers are platform-specific:
// "/etc" is absolute on Unix and merely root-relative on Windows, and a "C:"
// prefix means nothing to filepath.VolumeName off Windows.
func containedPath(dir string) bool {
	trimmed := strings.TrimSpace(dir)
	if trimmed == "" {
		return false
	}
	slashed := strings.ReplaceAll(trimmed, `\`, "/")
	if strings.HasPrefix(slashed, "/") {
		return false
	}
	if len(slashed) >= 2 && slashed[1] == ':' && isASCIILetter(slashed[0]) {
		return false
	}
	cleaned := path.Clean(slashed)
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return false
	}
	// "." resolves to the workspace root. A repository whose directory is the
	// root turns every operation on it into an operation on the whole
	// workspace, and `repo remove --delete` into deleting all of it.
	return cleaned != "." && cleaned != "/"
}

func isASCIILetter(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func countRole(m Manifest, role Role) int {
	count := 0
	for _, repo := range m.Repos {
		if repo.Role == role {
			count++
		}
	}
	return count
}

// HasEmbeddedCredential reports whether a URL carries userinfo.
//
// "https://user:token@host/repo.git" is a working remote and a leaked secret
// the moment the manifest is committed. git keeps credentials in a helper for
// exactly this reason, and the manifest records identity, not access.
func HasEmbeddedCredential(url string) bool {
	_, rest, ok := strings.Cut(strings.TrimSpace(url), "://")
	if !ok {
		return false
	}
	authority, _, _ := strings.Cut(rest, "/")
	return strings.Contains(authority, "@")
}

// ValidateRepoName reports whether a name may be used for a repository.
//
// It is exported because a command that creates directories has to ask before
// it touches the disk, not after: `vat repo new ../escaped` used to initialise a
// repository outside the workspace and only then fail this check on save,
// leaving the files behind. Callers and the manifest share this one definition
// so an early check can never disagree with the one that gates the write.
func ValidateRepoName(name string) error {
	if strings.TrimSpace(name) == "" {
		return errors.New("name is required")
	}
	// A repository name becomes a directory name and a path segment in a remote
	// URL. Role names and record ids are vat's own artefacts and cap at 64, but
	// this one has to match what a git host already accepts, and 100 is the
	// longest GitHub allows. Without a cap, `repo new` accepted a 200-character
	// name and left the failure to the filesystem.
	if len(name) > 100 {
		return errors.New("name may not be longer than 100 characters")
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '-', r == '_', r == '.':
		default:
			return errors.New("name may contain only letters, digits, '.', '_', and '-'")
		}
	}
	if strings.HasPrefix(name, ".") {
		return errors.New("name may contain only letters, digits, '.', '_', and '-'")
	}
	return nil
}

// Save validates and writes the manifest atomically, with a header comment
// pointing at the documentation.
func Save(path string, m Manifest) error {
	if err := Validate(m); err != nil {
		return err
	}
	body, err := Marshal(m)
	if err != nil {
		return err
	}
	return fsx.WriteFileAtomic(path, body, fsx.DefaultFileMode)
}

// Marshal renders a manifest to its on-disk YAML form.
func Marshal(m Manifest) ([]byte, error) {
	var builder strings.Builder
	builder.WriteString("# vat workspace manifest — https://github.com/takealook97/vat\n")
	builder.WriteString("# The single declaration of which repositories this workspace governs.\n")
	builder.WriteString("# Edit by hand or through `vat repo add|new|adopt|remove`, then run `vat lint`.\n")

	encoded, err := yaml.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("encode %s: %w", FileName, err)
	}
	builder.Write(encoded)
	return []byte(builder.String()), nil
}

// WithRepo returns a copy of m with repo added or, when a repository of the
// same name exists, replaced. The receiver is never mutated.
func WithRepo(m Manifest, repo Repo) Manifest {
	repos := make([]Repo, len(m.Repos), len(m.Repos)+1)
	copy(repos, m.Repos)
	for i := range repos {
		if repos[i].Name == repo.Name {
			repos[i] = repo
			out := m
			out.Repos = repos
			return out
		}
	}
	repos = append(repos, repo)
	out := m
	out.Repos = repos
	return out
}

// WithoutRepo returns a copy of m with the named repository removed, and
// reports whether it was present.
func WithoutRepo(m Manifest, name string) (Manifest, bool) {
	out := m
	repos := make([]Repo, 0, len(m.Repos))
	found := false
	for _, repo := range m.Repos {
		if repo.Name == name {
			found = true
			continue
		}
		repos = append(repos, repo)
	}
	out.Repos = repos
	return out, found
}
