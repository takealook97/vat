package harness

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/takealook97/vat/internal/frontmatter"
	"github.com/takealook97/vat/internal/fsx"
)

// Everybody who would benefit from this already has agent files. They were
// written by hand, into one runtime's directory, and the step where adoption
// stops is moving them somewhere else and rewriting their front matter. This
// finds them and does that move, so the reason not to adopt is an argument about
// the model rather than an afternoon of copying files.
//
// Only the Markdown adapters are candidates. A Codex adapter keeps its prose
// inside a TOML string, and turning that back into a Markdown body is a
// conversion with judgement in it — the kind this tool refuses to make silently.

// Adoption is one hand-written adapter and the canonical file it would become.
type Adoption struct {
	// Adapter is the hand-written file, relative to the workspace root.
	Adapter string
	// Canonical is where its body belongs, relative to the workspace root.
	Canonical string
	// Kind is "role" or "skill".
	Kind string
	// Name is the definition's name once adopted.
	Name string
	// Refusal says why this cannot be adopted, and is empty when it can.
	Refusal string
	// content is the canonical file that would be written.
	content string
}

// Adoptable finds every hand-written adapter that could become a definition.
//
// A file carrying GeneratedMarker is vat's own output and is skipped. So is one
// whose canonical file already exists: that is drift, which adapter-drift
// already reports, and overwriting the canonical copy would destroy the very
// thing this tool treats as the source.
func Adoptable(root string) ([]Adoption, error) {
	found, err := candidates(root)
	if err != nil {
		return nil, err
	}
	sort.Slice(found, func(i, j int) bool { return found[i].Adapter < found[j].Adapter })
	return refuseCollisions(found), nil
}

// refuseCollisions stops two adapters from being adopted into one canonical
// file. Sorted order decides which keeps the destination, so the outcome does
// not depend on the order a directory happened to be walked in.
//
// Without this the second write silently replaces the first, and what it
// destroys is a body somebody wrote by hand — the one thing adoption exists to
// preserve.
func refuseCollisions(found []Adoption) []Adoption {
	claimed := map[string]string{}
	refused := make([]Adoption, 0, len(found))
	for _, adoption := range found {
		if adoption.Refusal == "" {
			if first, taken := claimed[adoption.Canonical]; taken {
				adoption.Refusal = adoption.Canonical + " is already claimed by " + first
			} else {
				claimed[adoption.Canonical] = adoption.Adapter
			}
		}
		refused = append(refused, adoption)
	}
	return refused
}

func candidates(root string) ([]Adoption, error) {
	var found []Adoption
	for _, dir := range []struct{ adapters, kind string }{
		{ClaudeAgentDir, "role"},
		{ClaudeSkillDir, "skill"},
	} {
		base := filepath.Join(root, dir.adapters)
		err := filepath.WalkDir(base, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				if os.IsNotExist(err) || os.IsPermission(err) {
					return nil
				}
				return err
			}
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
				return nil
			}
			if dir.kind == "skill" && entry.Name() != SkillFile {
				// A skill directory holds references and scripts beside its
				// SKILL.md; only the procedure itself is a definition.
				return nil
			}
			content, err := os.ReadFile(path)
			if err != nil {
				if os.IsPermission(err) {
					return nil
				}
				return err
			}
			if strings.Contains(string(content), GeneratedMarker) {
				return nil
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			adoption := planAdoption(root, filepath.ToSlash(rel), dir.kind, string(content))
			found = append(found, adoption)
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return found, nil
}

func planAdoption(root, adapter, kind, content string) Adoption {
	name := adapterName(adapter, kind)
	adoption := Adoption{Adapter: adapter, Kind: kind, Name: name}
	if !ValidRoleName(name) {
		adoption.Refusal = "the name is not usable as a directory: letters, digits, '-', and '_' only"
		return adoption
	}
	doc := frontmatter.Split(content)
	if kind == "role" {
		adoption.Canonical = filepath.ToSlash(filepath.Join(RolesDir, name+".md"))
		adoption.content = adoptedRole(name, doc)
	} else {
		adoption.Canonical = filepath.ToSlash(filepath.Join(SkillsDir, name, SkillFile))
		adoption.content = adoptedSkill(name, doc)
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(adoption.Canonical))); err == nil {
		adoption.Refusal = adoption.Canonical + " already exists"
	}
	return adoption
}

func adapterName(adapter, kind string) string {
	if kind == "skill" {
		return filepath.Base(filepath.Dir(filepath.FromSlash(adapter)))
	}
	return strings.TrimSuffix(filepath.Base(filepath.FromSlash(adapter)), ".md")
}

// adoptedRole writes the front matter a role needs, keeping the body verbatim.
//
// The runtime it came from is recorded rather than assumed. A bare model is
// honoured only by a role targeting a single runtime, so a Claude file adopted
// as though it also targeted Codex would name a model Codex cannot resolve and
// trip model-ambiguous on its first lint. Widening it is a decision the person
// who owns the role makes.
func adoptedRole(name string, doc frontmatter.Document) string {
	var declared struct {
		Description string `yaml:"description"`
		Model       string `yaml:"model"`
	}
	_ = doc.Decode(&declared)

	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString("name: " + name + "\n")
	b.WriteString("description: " + yamlScalar(declared.Description) + "\n")
	if declared.Model != "" {
		b.WriteString("model: " + yamlScalar(declared.Model) + "\n")
	}
	// No writes: adoption never grants a capability the definition did not
	// state, and read-only is what a role with no declared write target gets.
	b.WriteString("reads: [\"*\"]\n")
	b.WriteString("runtimes: [" + runtimeClaude + "]\n")
	b.WriteString("---\n\n")
	b.WriteString(adoptedBody(name, doc.Body))
	return b.String()
}

func adoptedSkill(name string, doc frontmatter.Document) string {
	var declared struct {
		Description string `yaml:"description"`
	}
	_ = doc.Decode(&declared)

	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString("name: " + name + "\n")
	b.WriteString("description: " + yamlScalar(declared.Description) + "\n")
	b.WriteString("---\n\n")
	b.WriteString(adoptedBody(name, doc.Body))
	return b.String()
}

// adoptedBody keeps the prose exactly as written. A body that was only front
// matter gets a heading rather than an empty file, so the canonical copy is
// somewhere to write rather than a blank nobody notices.
func adoptedBody(name, body string) string {
	trimmed := strings.TrimSpace(body)
	if trimmed == "" {
		return fmt.Sprintf("# %s\n\n_Adopted from a runtime adapter that carried no body._\n", name)
	}
	return trimmed + "\n"
}

// Adopt writes the canonical files for every adoption that carries no refusal,
// returning the ones it wrote. Adapters are not regenerated here; the caller
// renders them, so one render covers adoption and everything else that changed.
func Adopt(root string, adoptions []Adoption) ([]Adoption, error) {
	var written []Adoption
	for _, adoption := range adoptions {
		if adoption.Refusal != "" {
			continue
		}
		path := filepath.Join(root, filepath.FromSlash(adoption.Canonical))
		if !withinRoot(adoption.Canonical) {
			return nil, fmt.Errorf("%w: %q would be written to %s",
				ErrInvalidSkillName, adoption.Name, adoption.Canonical)
		}
		if err := fsx.WriteFileAtomic(path, []byte(adoption.content), fsx.DefaultFileMode); err != nil {
			return nil, err
		}
		written = append(written, adoption)
	}
	return written, nil
}
