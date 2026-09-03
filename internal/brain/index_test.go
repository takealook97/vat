package brain_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/takealook97/vat/internal/brain"
)

// graphDoc is what an index outside this repository decodes: the two fields it
// needs and nothing else, so the test fails for the same reason such a reader
// would.
type graphDoc struct {
	SchemaVersion int `json:"schema_version"`
	Nodes         []struct {
		ID          string `json:"id"`
		Path        string `json:"path"`
		Status      string `json:"status"`
		ContentHash string `json:"content_hash"`
	} `json:"nodes"`
}

func buildGraph(t *testing.T, root string) graphDoc {
	t.Helper()
	store, err := brain.Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if _, err := brain.Build(store, reference); err != nil {
		t.Fatalf("Build: %v", err)
	}
	content, err := os.ReadFile(filepath.Join(root, "graph.json"))
	if err != nil {
		t.Fatalf("read graph.json: %v", err)
	}
	var graph graphDoc
	if err := json.Unmarshal(content, &graph); err != nil {
		t.Fatalf("decode graph.json: %v", err)
	}
	return graph
}

func hashOf(t *testing.T, root, relative string) string {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(relative)))
	if err != nil {
		t.Fatalf("read %s: %v", relative, err)
	}
	// Recomputed from the outside rather than through the production helper, so
	// the algorithm, the encoding and the prefix are pinned by something that
	// would not follow them if they changed.
	normalised := strings.ReplaceAll(strings.TrimPrefix(string(content), "\ufeff"), "\r\n", "\n")
	sum := sha256.Sum256([]byte(normalised))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// rewrite replaces a record file's bytes without going through writeRecord, so
// a test can put something on disk that no well-behaved editor would produce.
func rewrite(t *testing.T, root, relative string, transform func(string) string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", relative, err)
	}
	if err := os.WriteFile(path, []byte(transform(string(content))), 0o644); err != nil {
		t.Fatalf("write %s: %v", relative, err)
	}
}

// The layer's own documentation invites an index to read graph.json, and until
// now the file said neither which contract it was written against nor which
// records had changed since the last pass. An index had to open every file in
// the repository every time, and had no way to notice that the field meanings
// had moved under it.
func TestTheGraphCarriesTheContractVersionAndAHashPerRecord(t *testing.T) {
	// Arrange
	root, _ := newStore(t)
	writeRecord(t, root, "decisions/0001-adopt.md", `
id: D-0001
status: accepted
date: 2026-08-01
`, "# Adopt the thing\n\nBecause of the reason.")

	// Act
	graph := buildGraph(t, root)

	// Assert
	if graph.SchemaVersion != brain.SchemaVersion {
		t.Errorf("graph schema_version = %d, want %d", graph.SchemaVersion, brain.SchemaVersion)
	}
	if len(graph.Nodes) == 0 {
		t.Fatal("graph carries no nodes")
	}
	for _, node := range graph.Nodes {
		if want := hashOf(t, root, node.Path); node.ContentHash != want {
			t.Errorf("node %s content_hash = %q, want %q", node.ID, node.ContentHash, want)
		}
	}
}

// A status change is the one edit an index must never miss — a superseded
// record that keeps answering questions is the failure this whole layer exists
// to prevent — and it happens in the header, not the prose.
func TestAStatusChangeChangesTheContentHash(t *testing.T) {
	// Arrange
	root, _ := newStore(t)
	writeRecord(t, root, "decisions/0001-adopt.md", `
id: D-0001
status: accepted
date: 2026-08-01
`, "# Adopt the thing\n\nBecause of the reason.")
	before := buildGraph(t, root)

	// Act
	writeRecord(t, root, "decisions/0001-adopt.md", `
id: D-0001
status: superseded
date: 2026-08-01
`, "# Adopt the thing\n\nBecause of the reason.")
	after := buildGraph(t, root)

	// Assert
	if len(before.Nodes) != 1 || len(after.Nodes) != 1 {
		t.Fatalf("expected one node per build, got %d then %d", len(before.Nodes), len(after.Nodes))
	}
	if before.Nodes[0].ContentHash == after.Nodes[0].ContentHash {
		t.Error("the hash survived a status change, so an index would keep citing a superseded record")
	}
}

// Under git's default on Windows a checked-out record comes back with CRLF. If
// that reached the hash, every record would look changed to an index and the
// committed graph.json would drift on a platform that edited nothing.
func TestALineEndingDoesNotChangeTheContentHash(t *testing.T) {
	// Arrange
	root, _ := newStore(t)
	relative := "decisions/0001-adopt.md"
	writeRecord(t, root, relative, `
id: D-0001
status: accepted
date: 2026-08-01
`, "# Adopt the thing\n\nBecause of the reason.")
	before := buildGraph(t, root)

	// Act
	path := filepath.Join(root, filepath.FromSlash(relative))
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read record: %v", err)
	}
	crlf := strings.ReplaceAll(string(content), "\n", "\r\n")
	if err := os.WriteFile(path, []byte(crlf), 0o644); err != nil {
		t.Fatalf("write record: %v", err)
	}
	after := buildGraph(t, root)

	// Assert
	if len(before.Nodes) != 1 || len(after.Nodes) != 1 {
		t.Fatalf("expected one node per build, got %d then %d", len(before.Nodes), len(after.Nodes))
	}
	if before.Nodes[0].ContentHash == "" {
		t.Fatal("no hash was written, so this guard would pass on two empty strings")
	}
	if before.Nodes[0].ContentHash != after.Nodes[0].ContentHash {
		t.Error("a line ending changed the hash, so a Windows checkout would look like an edit to every record")
	}
}

// The parser strips a leading byte order mark before it reads a header, and for
// a while the hash did not. Saving a record in an editor that writes one —
// Notepad, or PowerShell's Set-Content — changed the hash while every field vat
// projects stayed identical: a file reported as edited that nobody edited, and
// an index re-reading it for nothing.
func TestAByteOrderMarkIsNotAChangeToARecord(t *testing.T) {
	// Arrange
	root, _ := newStore(t)
	relative := "decisions/0001-adopt.md"
	writeRecord(t, root, relative, `
id: D-0001
status: accepted
date: 2026-08-01
`, "# Adopt the thing\n\nBecause of the reason.")
	before := buildGraph(t, root)

	// Act
	rewrite(t, root, relative, func(content string) string { return "\ufeff" + content })
	after := buildGraph(t, root)

	// Assert
	if len(before.Nodes) != 1 || len(after.Nodes) != 1 {
		t.Fatalf("expected one node per build, got %d then %d", len(before.Nodes), len(after.Nodes))
	}
	if before.Nodes[0].ContentHash == "" {
		t.Fatal("no hash was written, so this guard would pass on two empty strings")
	}
	if before.Nodes[0].ContentHash != after.Nodes[0].ContentHash {
		t.Error("a byte order mark changed the hash, so re-saving a record in an editor looks like an edit")
	}
}

// The header is not the only place a record changes. An index that re-read only
// on a header change would keep serving the previous wording of a lesson.
func TestAnEditToTheBodyChangesTheContentHash(t *testing.T) {
	// Arrange
	root, _ := newStore(t)
	relative := "decisions/0001-adopt.md"
	header := `
id: D-0001
status: accepted
date: 2026-08-01
`
	writeRecord(t, root, relative, header, "# Adopt the thing\n\nBecause of the reason.")
	before := buildGraph(t, root)

	// Act
	writeRecord(t, root, relative, header, "# Adopt the thing\n\nBecause of a different reason.")
	after := buildGraph(t, root)

	// Assert
	if len(before.Nodes) != 1 || len(after.Nodes) != 1 {
		t.Fatalf("expected one node per build, got %d then %d", len(before.Nodes), len(after.Nodes))
	}
	if before.Nodes[0].ContentHash == after.Nodes[0].ContentHash {
		t.Error("the hash survived a rewritten body, so an index would serve the previous wording")
	}
}
