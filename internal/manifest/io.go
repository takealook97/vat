package manifest

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
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
		out.Policy.Gates.Deploy = "manual"
	}
	if out.Policy.Gates.ExternalWrite == "" {
		out.Policy.Gates.ExternalWrite = "manual"
	}
	if out.Policy.Gates.BrainPromote == "" {
		out.Policy.Gates.BrainPromote = "manual"
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
	seenNames := map[string]bool{}
	seenPaths := map[string]bool{}
	for i, repo := range m.Repos {
		where := fmt.Sprintf("repos[%d]", i)
		if repo.Name != "" {
			where = fmt.Sprintf("repos[%d] (%s)", i, repo.Name)
		}
		if strings.TrimSpace(repo.Name) == "" {
			problems = append(problems, where+": name is required")
		} else if !validRepoName(repo.Name) {
			problems = append(problems, where+": name may contain only letters, digits, '.', '_', and '-'")
		} else if seenNames[repo.Name] {
			problems = append(problems, where+": duplicate name")
		}
		seenNames[repo.Name] = true

		if strings.TrimSpace(repo.Origin) == "" {
			problems = append(problems, where+": origin is required")
		}
		if !repo.Role.Valid() {
			problems = append(problems, fmt.Sprintf("%s: unknown role %q (valid: %s)",
				where, repo.Role, joinRoles()))
		}
		dir := repo.Dir()
		if seenPaths[dir] {
			problems = append(problems, fmt.Sprintf("%s: two repositories resolve to the same directory %q", where, dir))
		}
		seenPaths[dir] = true
		if filepath.IsAbs(dir) || strings.HasPrefix(dir, "..") {
			problems = append(problems, where+": path must stay inside the workspace")
		}
		if repo.Access != "" && repo.Access != "public" && repo.Access != "private" {
			problems = append(problems, where+`: access must be "public" or "private"`)
		}
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
		if value != "manual" && value != "auto" {
			problems = append(problems, fmt.Sprintf(`%s must be "manual" or "auto", got %q`, name, value))
		}
	}
	if len(problems) == 0 {
		return nil
	}
	return fmt.Errorf("%s is invalid:\n  - %s", FileName, strings.Join(problems, "\n  - "))
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

func joinRoles() string {
	names := make([]string, 0, len(Roles()))
	for _, role := range Roles() {
		names = append(names, string(role))
	}
	return strings.Join(names, ", ")
}

func validRepoName(name string) bool {
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '-', r == '_', r == '.':
		default:
			return false
		}
	}
	return !strings.HasPrefix(name, ".")
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
	out := m
	repos := make([]Repo, len(m.Repos))
	copy(repos, m.Repos)
	for i := range repos {
		if repos[i].Name == repo.Name {
			repos[i] = repo
			out.Repos = repos
			return out
		}
	}
	out.Repos = append(repos, repo)
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
