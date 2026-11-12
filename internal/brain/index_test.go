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
	if _, err := brain.Build(store, brain.CheckPolicy{StaleAfterDays: 90}); err != nil {
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
	if graph.SchemaVersion != brain.GraphSchemaVersion {
		t.Errorf("graph schema_version = %d, want %d", graph.SchemaVersion, brain.GraphSchemaVersion)
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

// fullNode is what a reader without the binary decodes when it lists records
// from graph.json alone.
type fullNode struct {
	ID             string         `json:"id"`
	Status         string         `json:"status"`
	CitableUntil   string         `json:"citable_until"`
	ClaimKind      string         `json:"claim_kind"`
	Date           string         `json:"date"`
	PromotedVia    string         `json:"promoted_via"`
	SourceURL      string         `json:"source_url"`
	SourceQuote    string         `json:"source_quote"`
	SourceExternal bool           `json:"source_external"`
	Extensions     map[string]any `json:"extensions"`
}

func buildFullGraph(t *testing.T, root string) (map[string]fullNode, []byte) {
	t.Helper()
	buildGraph(t, root)
	content, err := os.ReadFile(filepath.Join(root, "graph.json"))
	if err != nil {
		t.Fatal(err)
	}
	var graph struct {
		Nodes []fullNode `json:"nodes"`
	}
	if err := json.Unmarshal(content, &graph); err != nil {
		t.Fatal(err)
	}
	nodes := map[string]fullNode{}
	for _, node := range graph.Nodes {
		nodes[node.ID] = node
	}
	return nodes, content
}

func TestGraphIncludesOptionalClaimDateAndExtensions(t *testing.T) {
	// Arrange
	root, _ := newStore(t)
	writeRecord(t, root, "memory/2026-11/M-0001-external.md", `
id: M-0001
status: active
date: 2026-11-01
claim_kind: historical
source_external: true
source_url: https://example.com/thread/1
source_quote: The cutover moves to Monday.
promoted_via: auto
project: demo-project
captured_at: 2026-11-01T09:00:00Z
attempts: 3
`, "# External event")
	writeRecord(t, root, "decisions/D-0001-legacy.md", `
id: D-0001
status: active
`, "# A legacy record")

	// Act
	nodes, content := buildFullGraph(t, root)

	// Assert
	external := nodes["M-0001"]
	if external.ClaimKind != "historical" || external.Date != "2026-11-01" || external.PromotedVia != "auto" ||
		external.SourceURL != "https://example.com/thread/1" || external.SourceQuote != "The cutover moves to Monday." ||
		!external.SourceExternal {
		t.Errorf("defined fields missing from the node: %+v", external)
	}
	want := map[string]any{"project": "demo-project", "captured_at": "2026-11-01T09:00:00Z", "attempts": float64(3)}
	if len(external.Extensions) != len(want) {
		t.Errorf("extensions %v, want %v", external.Extensions, want)
	}
	for key, value := range want {
		if external.Extensions[key] != value {
			t.Errorf("extension %s = %#v, want %#v", key, external.Extensions[key], value)
		}
	}
	for _, defined := range []string{"source_url", "promoted_via", "claim_kind", "status"} {
		if _, mixed := external.Extensions[defined]; mixed {
			t.Errorf("defined field %s leaked into extensions", defined)
		}
	}
	legacy := nodes["D-0001"]
	if legacy.Extensions != nil || legacy.ClaimKind != "" || legacy.Date != "" {
		t.Errorf("a record without the fields gained them: %+v", legacy)
	}
	if strings.Contains(string(content), `"extensions": {}`) || strings.Contains(string(content), `"extensions": null`) {
		t.Error("an empty extensions object was written instead of being omitted")
	}
}

// An extension key is display-only (SPEC 5.6.1). One spelled like a lifecycle
// field must not move the node's status or citability.
func TestGraphExtensionsDoNotAffectCitability(t *testing.T) {
	// Arrange
	root, _ := newStore(t)
	header := `
id: G-0001
status: active
claim_kind: current-state
owned_by: payments
source_ref: payments@3f9a1c2e8b74
observed_at: 2026-08-01
`
	writeRecord(t, root, "gaps/G-0001-plain.md", header, "# A claim")
	plain, _ := buildFullGraph(t, root)
	writeRecord(t, root, "gaps/G-0001-plain.md", header+"state: resolved\ncitable: false\ncitable_until: 2099-01-01\n", "# A claim")

	// Act
	extended, _ := buildFullGraph(t, root)

	// Assert
	before, after := plain["G-0001"], extended["G-0001"]
	if before.Status != after.Status || before.CitableUntil != after.CitableUntil || after.CitableUntil == "" {
		t.Fatalf("an extension key moved the lifecycle: before %+v after %+v", before, after)
	}
	if after.Extensions["state"] != "resolved" || after.Extensions["citable_until"] != "2099-01-01" {
		t.Errorf("extensions not published as written: %v", after.Extensions)
	}
}

// graph.json is committed, so two builds of the same records must be the same
// bytes, whatever order a mapping was written in.
func TestGraphNestedExtensionsAreDeterministic(t *testing.T) {
	// Arrange
	root, _ := newStore(t)
	writeRecord(t, root, "decisions/D-0001-a.md", `
id: D-0001
status: active
review:
  zeta: [1, two, {b: 2, a: 1}]
  alpha: {nested: true, empty: null}
`, "# Ordered one way")
	first, firstBytes := buildFullGraph(t, root)
	writeRecord(t, root, "decisions/D-0001-a.md", `
id: D-0001
status: active
review:
  alpha: {empty: null, nested: true}
  zeta: [1, two, {a: 1, b: 2}]
`, "# Ordered one way")

	// Act
	second, secondBytes := buildFullGraph(t, root)
	_, rebuilt := buildFullGraph(t, root)

	// Assert
	strip := func(content []byte) string {
		var lines []string
		for _, line := range strings.Split(string(content), "\n") {
			if !strings.Contains(line, "content_hash") {
				lines = append(lines, line)
			}
		}
		return strings.Join(lines, "\n")
	}
	if strip(firstBytes) != strip(secondBytes) {
		t.Errorf("key order changed the graph:\n%s\n---\n%s", firstBytes, secondBytes)
	}
	if string(secondBytes) != string(rebuilt) {
		t.Error("rebuilding the same records changed the bytes")
	}
	review, _ := first["D-0001"].Extensions["review"].(map[string]any)
	if review == nil || second["D-0001"].Extensions["review"] == nil {
		t.Fatalf("nested extension missing: %v", first["D-0001"].Extensions)
	}
	alpha, _ := review["alpha"].(map[string]any)
	if alpha["nested"] != true || alpha["empty"] != nil {
		t.Errorf("nested values changed: %v", review)
	}
}

// A value JSON cannot hold is reported against the record rather than dropped
// in silence or allowed to take the record out of every other rule.
func TestGraphReportsAnExtensionJSONCannotHold(t *testing.T) {
	root, _ := newStore(t)
	writeRecord(t, root, "decisions/D-0001-a.md", `
id: D-0001
status: active
weights: {1: heavy}
ratio: .nan
note: kept
`, "# Odd keys")
	store, err := brain.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	record := store.ByID()["D-0001"]
	if record.Status != brain.StatusActive || record.Extensions["note"] != "kept" {
		t.Fatalf("the record did not load with its readable extensions: %+v", record)
	}
	var reported []string
	for _, finding := range brain.Check(store, brain.CheckPolicy{}, reference) {
		if finding.Rule == "brain/record-malformed" {
			reported = append(reported, finding.Message)
		}
	}
	joined := strings.Join(reported, "\n")
	if len(reported) != 2 || !strings.Contains(joined, "weights") || !strings.Contains(joined, "ratio") {
		t.Fatalf("want one finding each for weights and ratio, got %q", reported)
	}
}
