// Package evidence defines the packet handed to a worker before it starts and
// the record it must return.
//
// The packet exists because delegation without a written contract produces work
// that is plausible and wrong: the worker infers the goal, invents an
// acceptance criterion, and reports success against its own invention. Naming
// the objective, the non-goals, the acceptance test, and the checks up front is
// what makes "done" checkable by someone other than the worker.
package evidence

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/takealook97/vat/internal/fsx"
)

// Dir is the workspace directory holding evidence packets.
const Dir = "evidence"

// Packet is the contract given to a worker.
type Packet struct {
	ID        string `yaml:"id" json:"id"`
	Objective string `yaml:"objective" json:"objective"`
	CreatedAt string `yaml:"created_at" json:"created_at"`

	// Repositories the worker may write to. Everything else is read-only,
	// including repositories it can obviously see.
	Repositories []string `yaml:"repositories" json:"repositories"`
	// NonGoals are the things that would look like progress and are not in
	// scope. Stating them is what stops scope from expanding silently.
	NonGoals []string `yaml:"non_goals,omitempty" json:"non_goals,omitempty"`
	// Contracts are the interfaces this work must honour or change deliberately.
	Contracts []string `yaml:"contracts,omitempty" json:"contracts,omitempty"`
	// Acceptance is the observable outcome that settles whether it worked.
	Acceptance []string `yaml:"acceptance" json:"acceptance"`
	// CanonicalChecks are the commands that prove it, run by the coordinator
	// rather than trusted from the worker's report.
	CanonicalChecks []string `yaml:"canonical_checks,omitempty" json:"canonical_checks,omitempty"`
	// EvidenceRefs point at the decisions that authorised the work.
	EvidenceRefs []string `yaml:"evidence_refs,omitempty" json:"evidence_refs,omitempty"`
	// RollbackPoints record where each repository stood before the work began.
	RollbackPoints map[string]string `yaml:"rollback_points,omitempty" json:"rollback_points,omitempty"`
	// ReleaseAuthority is false unless a human has explicitly granted it.
	// Judgement authority never implies it.
	ReleaseAuthority bool `yaml:"release_authority" json:"release_authority"`
	// Changeset links the packet to the multi-repository completion record.
	Changeset string `yaml:"changeset,omitempty" json:"changeset,omitempty"`
	Notes     string `yaml:"notes,omitempty" json:"notes,omitempty"`
}

// Path returns a packet's file path relative to the workspace root.
func Path(id string) string { return filepath.Join(Dir, id+".yaml") }

// ValidateID reports whether an identifier is safe to build a filename from.
//
// The id is chosen by the caller and pasted into a path, so an unchecked
// "../escape" wrote the packet outside the evidence directory -- where nothing
// lists it -- and a longer traversal left the workspace entirely.
func ValidateID(id string) error {
	if strings.TrimSpace(id) == "" {
		return errors.New("an identifier is required")
	}
	if len(id) > 64 {
		return errors.New("an identifier may not be longer than 64 characters")
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '-', r == '_', r == '.':
		default:
			return fmt.Errorf("identifier %q may contain only letters, digits, '.', '_', and '-'", id)
		}
	}
	if strings.HasPrefix(id, ".") {
		return fmt.Errorf("identifier %q may not begin with '.'", id)
	}
	// The identifier becomes a filename, so it is held to the rule every name
	// that becomes one is: a workspace whose records only its author can check
	// out is not the shared account this layer exists to be.
	if err := fsx.PortableName(id); err != nil {
		return fmt.Errorf("identifier %q: %w", id, err)
	}
	return nil
}

// New builds an empty packet.
func New(id, objective string, repositories []string, now time.Time) Packet {
	return Packet{
		ID:               id,
		Objective:        objective,
		CreatedAt:        now.Format("2006-01-02"),
		Repositories:     repositories,
		ReleaseAuthority: false,
	}
}

// Save writes a packet atomically.
func Save(root string, packet Packet) error {
	// Checked here rather than at the command, so no caller can write a packet
	// to a path of its own choosing.
	if err := ValidateID(packet.ID); err != nil {
		return err
	}
	encoded, err := yaml.Marshal(packet)
	if err != nil {
		return fmt.Errorf("encode packet %s: %w", packet.ID, err)
	}
	header := "# vat evidence packet — the contract a worker is given before starting.\n" +
		"# A worker's own report of success is not evidence. The checks below are.\n"
	return fsx.WriteFileAtomic(filepath.Join(root, Path(packet.ID)),
		append([]byte(header), encoded...), fsx.DefaultFileMode)
}

// Load reads one packet.
func Load(root, id string) (Packet, error) {
	if err := ValidateID(id); err != nil {
		return Packet{}, err
	}
	data, err := os.ReadFile(filepath.Join(root, Path(id)))
	if err != nil {
		return Packet{}, fmt.Errorf("read packet %s: %w", id, err)
	}
	var packet Packet
	decoder := yaml.NewDecoder(strings.NewReader(string(data)))
	decoder.KnownFields(true)
	if err := decoder.Decode(&packet); err != nil {
		return Packet{}, fmt.Errorf("parse %s: %w", Path(id), err)
	}
	return packet, nil
}

// List reads every packet in the workspace.
func List(root string) ([]Packet, error) {
	entries, err := os.ReadDir(filepath.Join(root, Dir))
	if os.IsNotExist(err) {
		return []Packet{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", Dir, err)
	}
	packets := []Packet{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}
		packet, err := Load(root, strings.TrimSuffix(entry.Name(), ".yaml"))
		if err != nil {
			return nil, err
		}
		packets = append(packets, packet)
	}
	sort.SliceStable(packets, func(i, j int) bool { return packets[i].ID > packets[j].ID })
	return packets, nil
}

// Validate reports what a packet is missing to be usable.
func Validate(packet Packet) []string {
	var problems []string
	if strings.TrimSpace(packet.Objective) == "" {
		problems = append(problems, "objective is empty; the worker will infer one")
	}
	if len(packet.Repositories) == 0 {
		problems = append(problems, "no repositories in scope; the write boundary is undefined")
	}
	if len(packet.Acceptance) == 0 {
		problems = append(problems,
			"no acceptance criterion; completion would be whatever the worker says it is")
	}
	if len(packet.CanonicalChecks) == 0 {
		problems = append(problems,
			"no canonical checks; there is nothing to verify the report against")
	}
	for _, repo := range packet.Repositories {
		if packet.RollbackPoints[repo] == "" {
			problems = append(problems,
				fmt.Sprintf("no rollback point recorded for %s", repo))
		}
	}
	return problems
}

// Markdown renders a packet as the briefing text to paste into an agent
// session.
func Markdown(packet Packet) string {
	var b strings.Builder
	b.WriteString("# " + packet.ID + " — " + packet.Objective + "\n\n")

	b.WriteString("## Scope\n\nWrite only inside:\n\n")
	for _, repo := range packet.Repositories {
		b.WriteString("- `" + repo + "`\n")
	}
	b.WriteString("\nEvery other repository is readable and read-only. Reading one is not\n")
	b.WriteString("permission to change it.\n\n")

	if len(packet.NonGoals) > 0 {
		b.WriteString("## Not in scope\n\n")
		for _, item := range packet.NonGoals {
			b.WriteString("- " + item + "\n")
		}
		b.WriteString("\n")
	}
	if len(packet.Contracts) > 0 {
		b.WriteString("## Contracts to honour\n\n")
		for _, item := range packet.Contracts {
			b.WriteString("- " + item + "\n")
		}
		b.WriteString("\n")
	}

	b.WriteString("## Acceptance\n\nThis is done when, and only when:\n\n")
	for _, item := range packet.Acceptance {
		b.WriteString("- " + item + "\n")
	}
	b.WriteString("\n")

	if len(packet.CanonicalChecks) > 0 {
		b.WriteString("## Checks\n\n```bash\n")
		for _, check := range packet.CanonicalChecks {
			b.WriteString(check + "\n")
		}
		b.WriteString("```\n\n")
	}

	b.WriteString("## Authority\n\n")
	if packet.ReleaseAuthority {
		b.WriteString("Release is authorised for this packet.\n\n")
	} else {
		b.WriteString("No release authority. Do not deploy, publish, or write to any external\n")
		b.WriteString("system. Stop and report when the work is ready for review.\n\n")
	}
	if len(packet.RollbackPoints) > 0 {
		b.WriteString("## Rollback points\n\n")
		names := make([]string, 0, len(packet.RollbackPoints))
		for name := range packet.RollbackPoints {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			fmt.Fprintf(&b, "- `%s` at `%s`\n", name, packet.RollbackPoints[name])
		}
		b.WriteString("\n")
	}
	b.WriteString("## Reporting\n\n")
	b.WriteString("Report the diff, the revision, the checks you ran, and what you could not\n")
	b.WriteString("verify. Saying the work is complete is not evidence that it is.\n")
	return b.String()
}
