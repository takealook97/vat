package brain_test

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/takealook97/vat/internal/brain"
)

func TestCurrentRoutesToCanonicalViewsThatExist(t *testing.T) {
	// Arrange: these are the maintained views vat itself scaffolds around the
	// atomic records. A generated entry point that does not name them strands
	// current state and execution order outside its own reading contract.
	root, store := newStore(t)
	for _, name := range []string{"STATUS.md", "ROADMAP.md", "AGENT_OPERATING_MODEL.md"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("# maintained view\n"), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	// Act
	index := brain.RenderCurrent(store, reference)

	// Assert
	// The link text is the file, as it is in every other table this index
	// renders. The question is already the row's first column, and repeating it
	// as the link made the row say the same thing twice.
	for _, link := range []string{"| Current state | [STATUS.md](STATUS.md) |",
		"| Execution order | [ROADMAP.md](ROADMAP.md) |",
		"| Agent operating model | [AGENT_OPERATING_MODEL.md](AGENT_OPERATING_MODEL.md) |"} {
		if !strings.Contains(index, link) {
			t.Errorf("CURRENT.md does not route to %s:\n%s", link, index)
		}
	}
}

func TestCurrentRecognisesThePortfolioStatusNameUsedByAnAdoptedBrain(t *testing.T) {
	// Arrange: an adopted knowledge repository may already have a more precise
	// name for the current-state view. Adoption must not require renaming its
	// canonical document merely to make the new index route to it.
	root, store := newStore(t)
	if err := os.Remove(filepath.Join(root, "STATUS.md")); err != nil {
		t.Fatalf("remove scaffolded status: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "PORTFOLIO_STATUS.md"), []byte("# portfolio\n"), 0o644); err != nil {
		t.Fatalf("write portfolio status: %v", err)
	}

	// Act
	index := brain.RenderCurrent(store, reference)

	// Assert
	if !strings.Contains(index, "| Current state | [PORTFOLIO_STATUS.md](PORTFOLIO_STATUS.md) |") {
		t.Errorf("CURRENT.md does not route to an adopted current-state view:\n%s", index)
	}
}

// The case this file exists for: a knowledge repository that predates vat.
// Adopting one used to overwrite whatever it already kept at CURRENT.md,
// because the only question asked was whether the content matched what vat
// would render — which a file vat never wrote never does.

func TestBuildLeavesAProjectionItDidNotWrite(t *testing.T) {
	// Arrange: an index somebody maintained by hand, in the place vat
	// generates its own.
	root, _ := newStore(t)
	writeRecord(t, root, "decisions/D-0001-x.md", "id: D-0001\nstatus: active", "# D-0001 — Something")
	handWritten := "# Cortex — current state\n\nMaintained by hand since 2023.\n"
	path := filepath.Join(root, brain.CurrentFile)
	if err := os.WriteFile(path, []byte(handWritten), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Act
	result, err := brain.Build(reload(t, root), reference)

	// Assert
	if err != nil {
		t.Fatalf("Build returned an error: %v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(after) != handWritten {
		t.Errorf("Build overwrote a file vat did not write:\n%s", after)
	}
	if !slices.Contains(result.Skipped, brain.CurrentFile) {
		t.Errorf("Skipped = %v, want it to name %s", result.Skipped, brain.CurrentFile)
	}
	if slices.Contains(result.Changed, brain.CurrentFile) {
		t.Errorf("Changed = %v, want it to leave out the file that was left alone", result.Changed)
	}
	// The projection vat does own is still written: one foreign file must not
	// stop the rest of the repository being brought up to date.
	if !slices.Contains(result.Changed, brain.GraphFile) {
		t.Errorf("Changed = %v, want it to include %s", result.Changed, brain.GraphFile)
	}
}

func TestBuildRewritesAProjectionCarryingItsOwnProvenance(t *testing.T) {
	// Arrange: vat's own output, hand-edited. This is drift, not a foreign
	// file, and the difference is the marker the render puts in it.
	root, _ := newStore(t)
	writeRecord(t, root, "decisions/D-0001-x.md", "id: D-0001\nstatus: active", "# D-0001 — Something")
	if _, err := brain.Build(reload(t, root), reference); err != nil {
		t.Fatalf("first Build returned an error: %v", err)
	}
	path := filepath.Join(root, brain.CurrentFile)
	generated, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	edited := string(generated) + "\nedited by hand\n"
	if err := os.WriteFile(path, []byte(edited), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Act
	result, err := brain.Build(reload(t, root), reference)

	// Assert
	if err != nil {
		t.Fatalf("Build returned an error: %v", err)
	}
	if !slices.Contains(result.Changed, brain.CurrentFile) {
		t.Errorf("Changed = %v, want vat's own projection rebuilt", result.Changed)
	}
	if len(result.Skipped) != 0 {
		t.Errorf("Skipped = %v, want nothing skipped", result.Skipped)
	}
}

func TestAForeignProjectionIsReportedAsUnmanagedRatherThanDrift(t *testing.T) {
	// Arrange
	root, _ := newStore(t)
	writeRecord(t, root, "decisions/D-0001-x.md", "id: D-0001\nstatus: active", "# D-0001 — Something")
	if err := os.WriteFile(filepath.Join(root, brain.CurrentFile),
		[]byte("# hand written\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Act
	unmanaged, err := brain.Unmanaged(root)
	if err != nil {
		t.Fatalf("Unmanaged returned an error: %v", err)
	}
	drift, err := brain.Drift(reload(t, root), reference)
	if err != nil {
		t.Fatalf("Drift returned an error: %v", err)
	}

	// Assert: reporting it as drift would offer `vat brain build` as the fix,
	// and the fix would destroy the file.
	if !slices.Contains(unmanaged, brain.CurrentFile) {
		t.Errorf("Unmanaged = %v, want it to name %s", unmanaged, brain.CurrentFile)
	}
	if slices.Contains(drift, brain.CurrentFile) {
		t.Errorf("Drift = %v, want a foreign file reported as unmanaged instead", drift)
	}
}

func TestAForeignGraphIsRecognisedByItsMissingProvenance(t *testing.T) {
	// Arrange: valid JSON that is not vat's graph.
	root, _ := newStore(t)
	if err := os.WriteFile(filepath.Join(root, brain.GraphFile),
		[]byte("{\"nodes\": []}\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Act
	unmanaged, err := brain.Unmanaged(root)

	// Assert
	if err != nil {
		t.Fatalf("Unmanaged returned an error: %v", err)
	}
	if !slices.Contains(unmanaged, brain.GraphFile) {
		t.Errorf("Unmanaged = %v, want it to name %s", unmanaged, brain.GraphFile)
	}
}

func TestUnmanagedSaysNothingAboutAProjectionThatIsNotThere(t *testing.T) {
	// Arrange
	root := t.TempDir()

	// Act
	unmanaged, err := brain.Unmanaged(root)

	// Assert
	if err != nil {
		t.Fatalf("Unmanaged returned an error: %v", err)
	}
	if len(unmanaged) != 0 {
		t.Errorf("Unmanaged = %v, want nothing for a directory with no projections", unmanaged)
	}
}

func TestCurrentNamesNewDecisionsTheCitationRankingWouldHide(t *testing.T) {
	// Arrange: the ranked table keeps what the repository leans on hardest,
	// which is the right cut for a bounded index and the wrong one for "what
	// was decided lately". A decision taken yesterday is cited by nothing yet,
	// so ranking can only hide it — and an index that cannot show the newest
	// decision gets read as stale.
	root, _ := newStore(t)
	for i := 1; i <= 20; i++ {
		id := fmt.Sprintf("D-%04d", i)
		writeRecord(t, root, "decisions/"+id+"-x.md",
			"id: "+id+"\nstatus: active", "# "+id+" — Decision "+id)
	}
	// Every early decision is cited, so citation ranking fills the table with
	// them and the newest twenty are pushed out.
	refs := make([]string, 0, 15)
	for i := 1; i <= 15; i++ {
		refs = append(refs, fmt.Sprintf("D-%04d", i))
	}
	writeRecord(t, root, "goals/G-0001-x.md",
		"id: G-0001\nstatus: active\nrefs: ["+strings.Join(refs, ", ")+"]", "# G-0001 — A goal")

	// Act
	index := brain.RenderCurrent(reload(t, root), reference)

	// Assert
	if !strings.Contains(index, "D-0020") {
		t.Errorf("the newest decision is unreachable from the index:\n%s", index)
	}
	if !strings.Contains(index, "Ranked by how many records cite them.") {
		t.Errorf("the ranking is applied but never stated:\n%s", index)
	}
	// Bounded, still. Returning to a row per record is the failure the index
	// exists to prevent.
	if newest := strings.Count(index, "\n- `D-"); newest > 5 {
		t.Errorf("the recency list has %d entries, want at most 5:\n%s", newest, index)
	}
}
