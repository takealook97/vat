package harness

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/takealook97/vat/internal/frontmatter"
	"github.com/takealook97/vat/internal/fsx"
)

// A skill is a procedure an agent loads on demand; a role is who is running.
// They drift for the same reason and are kept honest the same way: the body
// lives once under .agents/skills, and each runtime gets a generated pointer
// that carries only what that runtime needs to discover it.
//
// Copying the procedure into every runtime directory is the failure this
// prevents. Two copies of a deployment skill that disagree by one step is worse
// than one copy nobody has read, because both look authoritative.

// Skill is one runtime-neutral procedure.
type Skill struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	// Runtimes selects which adapters are generated. Empty targets all of them.
	Runtimes []string `yaml:"runtimes,omitempty"`

	// Body is the procedure itself, never copied into an adapter.
	Body string `yaml:"-"`
	// Dir is the skill's directory, relative to the repository root.
	Dir string `yaml:"-"`
}

// SkillFile is the canonical filename inside a skill directory, and the name
// every runtime that supports skills already looks for.
const SkillFile = "SKILL.md"

// ErrInvalidSkillName is returned for a skill whose name could escape the
// directories adapters are written into. It is separate from the role sentinel
// so the message names what the reader actually has in front of them: reporting
// "invalid role name" for a file under .agents/skills sends them to the wrong
// directory.
var ErrInvalidSkillName = errors.New("invalid skill name")

// TargetsRuntime reports whether an adapter should be generated for a runtime.
func (s Skill) TargetsRuntime(runtime string) bool {
	if len(s.Runtimes) == 0 {
		return true
	}
	for _, name := range s.Runtimes {
		if strings.EqualFold(name, runtime) {
			return true
		}
	}
	return false
}

// LoadSkills reads every skill definition under root.
func LoadSkills(root string) ([]Skill, []Malformed, error) {
	dir := filepath.Join(root, SkillsDir)
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("read %s: %w", dir, err)
	}
	skills := make([]Skill, 0, len(entries))
	var malformed []Malformed
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(dir, entry.Name(), SkillFile)
		if _, err := os.Stat(path); err != nil {
			// A directory of references with no SKILL.md is not a skill. It is
			// also not an error worth stopping the whole harness for.
			continue
		}
		skill, err := LoadSkill(path)
		if err != nil {
			// See Malformed: an escaping name is a refusal, not a finding.
			if errors.Is(err, ErrInvalidSkillName) {
				return nil, malformed, err
			}
			malformed = append(malformed, Malformed{
				Path: filepath.Join(SkillsDir, entry.Name(), SkillFile), Problem: err.Error(),
			})
			continue
		}
		skills = append(skills, skill)
	}
	byName := map[string][]string{}
	for _, skill := range skills {
		byName[skill.Name] = append(byName[skill.Name],
			filepath.Join(SkillsDir, skill.Dir, SkillFile))
	}
	if err := refuseDuplicateNames(byName); err != nil {
		return nil, malformed, err
	}
	sort.Slice(skills, func(i, j int) bool { return skills[i].Name < skills[j].Name })
	return skills, malformed, nil
}

// LoadSkill reads one skill definition.
func LoadSkill(path string) (Skill, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Skill{}, fmt.Errorf("read %s: %w", path, err)
	}
	doc := frontmatter.Split(string(data))
	var skill Skill
	if err := doc.Decode(&skill); err != nil {
		return Skill{}, fmt.Errorf("%s: %w", path, err)
	}
	skill.Body = doc.Body
	skill.Dir = filepath.Base(filepath.Dir(path))
	if skill.Name == "" {
		skill.Name = skill.Dir
	}
	// The name becomes a directory under every runtime, so it is validated the
	// same way a role name is: this is the value that decides where vat writes.
	if !ValidRoleName(skill.Name) {
		return Skill{}, fmt.Errorf("%w %q in %s: use letters, digits, '-', and '_' only",
			ErrInvalidSkillName, skill.Name, path)
	}
	return skill, nil
}

// RenderSkillAdapters returns every runtime adapter a skill should have.
func RenderSkillAdapters(skill Skill) []Adapter {
	var adapters []Adapter
	if skill.TargetsRuntime("claude") {
		adapters = append(adapters, Adapter{
			Runtime: "claude",
			Path:    filepath.Join(ClaudeSkillDir, skill.Name, SkillFile),
			Content: renderClaudeSkill(skill),
		})
	}
	return adapters
}

func renderClaudeSkill(skill Skill) string {
	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString("name: " + skill.Name + "\n")
	b.WriteString("description: " + yamlScalar(skill.Description) + "\n")
	b.WriteString("---\n\n")
	b.WriteString("<!-- " + adapterWarning(SkillsDir) + " -->\n\n")
	fmt.Fprintf(&b, "Read `%s/%s/%s` in full and follow it exactly.\n",
		SkillsDir, skill.Dir, SkillFile)
	b.WriteString("It is the canonical definition of this skill; this file only makes it\n")
	b.WriteString("discoverable in one runtime, and carries none of the procedure itself.\n")
	return b.String()
}

// WriteSkillAdapters renders and writes every skill adapter under root,
// returning the relative paths that changed.
func WriteSkillAdapters(root string, skills []Skill) ([]string, error) {
	var changed []string
	for _, skill := range skills {
		for _, adapter := range RenderSkillAdapters(skill) {
			// The name reached this from a file on disk, so the destination is
			// checked independently of the validation upstream: an adapter is
			// written whole, with no markers to bound the damage.
			if !withinRoot(adapter.Path) {
				return nil, fmt.Errorf("%w: adapter for %q would be written to %s",
					ErrInvalidSkillName, skill.Name, adapter.Path)
			}
			path := filepath.Join(root, adapter.Path)
			current, _, err := fsx.ReadFileIfExists(path)
			if err != nil {
				return nil, err
			}
			if string(current) == adapter.Content {
				continue
			}
			if err := fsx.WriteFileAtomic(path, []byte(adapter.Content), fsx.DefaultFileMode); err != nil {
				return nil, err
			}
			changed = append(changed, adapter.Path)
		}
	}
	sort.Strings(changed)
	return changed, nil
}

// SkillAdapterDrift returns the skill adapters that no longer match their
// canonical definition.
func SkillAdapterDrift(root string, skills []Skill) ([]string, error) {
	var drifted []string
	for _, skill := range skills {
		for _, adapter := range RenderSkillAdapters(skill) {
			path := filepath.Join(root, adapter.Path)
			current, exists, err := fsx.ReadFileIfExists(path)
			if err != nil {
				return nil, err
			}
			if !exists || string(current) != adapter.Content {
				drifted = append(drifted, adapter.Path)
			}
		}
	}
	sort.Strings(drifted)
	return drifted, nil
}
