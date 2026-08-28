package brain

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/takealook97/vat/internal/fsx"
)

// Generated returns the projection files rebuilt from atomic records.
func Generated() []string { return []string{CurrentFile, GraphFile} }

// BuildResult reports which generated files a build changed, and which it
// refused to touch because vat did not write them.
type BuildResult struct {
	Changed []string `json:"changed"`
	// Skipped names the projections left exactly as they were found. See
	// Unmanaged for why a build declines rather than overwrites.
	Skipped []string `json:"skipped,omitempty"`
}

// Build regenerates every projection from the atomic records.
//
// The split between atomic records and projections is what keeps a knowledge
// repository readable as it grows. Detail accumulates in one file per fact;
// the index stays a fixed-size entry point. Appending detail into a summary
// instead produces a file nobody reads and an agent quotes the stale top of.
func Build(store *Store, now time.Time) (BuildResult, error) {
	var result BuildResult

	// Asked before anything is rendered. A projection vat did not write is
	// somebody's file that happens to share a name, and a build that answers
	// only "does this match what I would render" cannot tell the two apart.
	foreign, err := Unmanaged(store.Root)
	if err != nil {
		return result, err
	}
	result.Skipped = foreign

	renders := map[string][]byte{
		CurrentFile: []byte(RenderCurrent(store, now)),
		GraphFile:   nil,
	}
	graph, err := RenderGraph(store)
	if err != nil {
		return result, err
	}
	renders[GraphFile] = graph

	names := make([]string, 0, len(renders))
	for name := range renders {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		if slices.Contains(foreign, name) {
			continue
		}
		path := filepath.Join(store.Root, name)
		current, _, err := fsx.ReadFileIfExists(path)
		if err != nil {
			return result, err
		}
		// Compared on content: under the default core.autocrlf on Windows a
		// committed projection comes back with CRLF, and an exact match had
		// every build rewrite both files and report them as regenerated.
		if fsx.NormaliseNewlines(string(current)) == fsx.NormaliseNewlines(string(renders[name])) {
			continue
		}
		if err := fsx.WriteFileAtomic(path, renders[name], fsx.DefaultFileMode); err != nil {
			return result, err
		}
		result.Changed = append(result.Changed, name)
	}
	return result, nil
}

// Drift returns the generated files whose on-disk content no longer matches
// what the atomic records would produce.
func Drift(store *Store, now time.Time) ([]string, error) {
	var drifted []string
	expected := map[string][]byte{CurrentFile: []byte(RenderCurrent(store, now))}
	graph, err := RenderGraph(store)
	if err != nil {
		return nil, err
	}
	expected[GraphFile] = graph

	// A file vat never wrote is not out of date with respect to the records;
	// it is not a projection at all. Calling it drift would offer `vat brain
	// build` as the repair, and the repair would delete somebody's work.
	foreign, err := Unmanaged(store.Root)
	if err != nil {
		return nil, err
	}

	for _, name := range Generated() {
		if slices.Contains(foreign, name) {
			continue
		}
		path := filepath.Join(store.Root, name)
		current, exists, err := fsx.ReadFileIfExists(path)
		if err != nil {
			return nil, err
		}
		// The same question the harness asks of its own generated files: a line
		// ending is not drift, and reporting it as one gave a Windows checkout
		// a permanently red `vat brain check` on files nobody had touched.
		if !exists || fsx.NormaliseNewlines(string(current)) != fsx.NormaliseNewlines(string(expected[name])) {
			drifted = append(drifted, name)
		}
	}
	sort.Strings(drifted)
	return drifted, nil
}

// RenderCurrent produces the bounded entry point: enough to find the right
// record, and deliberately not enough to answer from on its own.
func RenderCurrent(store *Store, now time.Time) string {
	var b strings.Builder
	b.WriteString("# Current index\n\n")
	b.WriteString(CurrentNotice + "\n\n")
	b.WriteString("Start every question here. Find the identifiers that matter, then open only\n")
	b.WriteString("those records. Reading the whole repository makes answers worse, not better:\n")
	b.WriteString("superseded reasoning and current fact become indistinguishable.\n\n")
	fmt.Fprintf(&b, "Rebuilt %s.\n\n", now.Format("2006-01-02"))

	b.WriteString(renderCounts(store))
	b.WriteString("\n")
	b.WriteString(renderCanonicalViews(store.Root))
	goals, _ := renderSection(store, KindGoal, "Goals", "GOAL.md", func(r Record) bool {
		return !r.Status.Terminal()
	})
	b.WriteString(goals)
	gaps, _ := renderSection(store, KindGap, "Open gaps", "GAP_ANALYSIS.md", func(r Record) bool {
		return !r.Status.Terminal()
	})
	b.WriteString(gaps)
	decisions, shownDecisions := renderSection(store, KindDecision, "Active decisions", "DECISIONS.md",
		func(r Record) bool {
			return r.Status == StatusActive || r.Status == StatusProvisional
		})
	b.WriteString(decisions)
	b.WriteString(renderNewestDecisions(store, shownDecisions))

	b.WriteString(renderAttention(store, now))
	b.WriteString(renderRecentMemory(store))

	b.WriteString("\n## Reading contract\n\n")
	b.WriteString("1. Locate identifiers here.\n")
	b.WriteString("2. Open only the atomic records named.\n")
	b.WriteString("3. Re-verify any claim about the present against the repository that owns\n")
	b.WriteString("   it. A record states when it was last observed, not that it is still true.\n")
	b.WriteString("4. Open `history/`, `archive/`, and long-form analysis only when asked for\n")
	b.WriteString("   past reasoning.\n")
	return b.String()
}

type canonicalView struct {
	label string
	file  string
}

// renderCanonicalViews keeps maintained synthesis documents reachable from the
// generated entry point. Atomic records answer which fact to open; these views
// answer the wider questions vat's own scaffold creates them for, such as what
// is running and what should happen next.
func canonicalViewsPresent(root string) []canonicalView {
	status := "STATUS.md"
	if !fsx.Exists(filepath.Join(root, status)) && fsx.Exists(filepath.Join(root, "PORTFOLIO_STATUS.md")) {
		status = "PORTFOLIO_STATUS.md"
	}
	candidates := []canonicalView{
		{label: "Current state", file: status},
		{label: "Goals and acceptance criteria", file: "GOAL.md"},
		{label: "Distance from the goals", file: "GAP_ANALYSIS.md"},
		{label: "Execution order", file: "ROADMAP.md"},
		{label: "Decisions", file: "DECISIONS.md"},
		{label: "Reviewed observations", file: "MEMORY.md"},
		{label: "Agent operating model", file: "AGENT_OPERATING_MODEL.md"},
	}
	views := make([]canonicalView, 0, len(candidates))
	for _, candidate := range candidates {
		if fsx.Exists(filepath.Join(root, candidate.file)) {
			views = append(views, candidate)
		}
	}
	return views
}

func renderCanonicalViews(root string) string {
	views := canonicalViewsPresent(root)
	if len(views) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("## Canonical views\n\n")
	b.WriteString("| Question | Maintained view |\n")
	b.WriteString("| --- | --- |\n")
	for _, view := range views {
		fmt.Fprintf(&b, "| %s | [%s](%s) |\n", view.label, view.file, view.file)
	}
	b.WriteString("\n")
	return b.String()
}

func renderCounts(store *Store) string {
	byStatus := map[Status]int{}
	for _, record := range store.Records {
		byStatus[record.Status]++
	}
	var b strings.Builder
	b.WriteString("## Inventory\n\n")
	b.WriteString("| Status | Records | Meaning |\n")
	b.WriteString("| --- | --- | --- |\n")
	for _, status := range Statuses() {
		count := byStatus[status]
		if count == 0 {
			continue
		}
		fmt.Fprintf(&b, "| `%s` | %d | %s |\n", status, count, statusMeaning(status))
	}
	if len(store.Records) == 0 {
		b.WriteString("| — | 0 | No records yet. Create one with `vat brain new`. |\n")
	}
	return b.String()
}

func statusMeaning(status Status) string {
	switch status {
	case StatusProvisional:
		return "Recorded, not yet reviewed. Not citable as fact."
	case StatusActive:
		return "Reviewed and citable."
	case StatusStale:
		return "Was true when observed; nobody has re-checked it since."
	case StatusQuarantined:
		return "Suspect. Withheld from answers until resolved."
	case StatusSuperseded:
		return "Replaced by a later decision; kept for its reasoning."
	case StatusRevoked:
		return "Withdrawn. Kept as a tombstone."
	case StatusResolved:
		return "Closed."
	default:
		return ""
	}
}

// sectionLimit is how many records one section of the index may list.
//
// The index is documented as a fixed-size entry point and was not one: it grew
// a row per record forever, until reading it cost more than reading the records
// it points at. That is the summary file this whole layer exists to replace,
// arriving late — once the repository is finally big enough to be worth having.
const sectionLimit = 15

// renderSection returns the rendered table and the records it listed, so a
// caller can say what the ranking left out without ranking again.
func renderSection(store *Store, kind Kind, heading, projection string, include func(Record) bool) (string, []Record) {
	records := make([]Record, 0)
	for _, record := range store.OfKind(kind) {
		if include(record) && !record.Archived {
			records = append(records, record)
		}
	}
	if len(records) == 0 {
		return "", nil
	}
	shown, remaining := mostDependedOn(store, records, sectionLimit)

	var b strings.Builder
	b.WriteString("\n## " + heading + "\n\n")
	if remaining > 0 {
		// The ranking was invisible, and a reader who could not find a decision
		// they had just made concluded the index was stale rather than ranked.
		b.WriteString("Ranked by how many records cite them.\n\n")
	}
	b.WriteString("| ID | Status | Title | Record |\n")
	b.WriteString("| --- | --- | --- | --- |\n")
	for _, record := range shown {
		fmt.Fprintf(&b, "| `%s` | %s | %s | [%s](%s) |\n",
			record.ID, record.Status, escapePipes(record.Title),
			filepath.Base(record.Path), record.Path)
	}
	if remaining > 0 {
		fmt.Fprintf(&b, "\n%d more in [%s](%s).\n", remaining, projection, projection)
	}
	return b.String(), shown
}

// mostDependedOn keeps the records the rest of the repository leans on hardest
// and reports how many were left out.
//
// Truncating by identifier instead would always keep the oldest records, which
// is the worst possible cut: the entry point would fill with the first things
// ever written and hide everything current. Citation count is the same measure
// the review queue already uses to decide what costs most to ignore. The kept
// records are then re-sorted by identifier, so the table itself does not
// reshuffle every time a reference is added.
func mostDependedOn(store *Store, records []Record, limit int) ([]Record, int) {
	if len(records) <= limit {
		return records, 0
	}
	references := store.ReferenceCounts()
	ranked := append([]Record{}, records...)
	sort.SliceStable(ranked, func(i, j int) bool {
		left, right := references[ranked[i].ID], references[ranked[j].ID]
		if left != right {
			return left > right
		}
		return ranked[i].ID > ranked[j].ID
	})
	return SortRecords(ranked[:limit]), len(records) - limit
}

func renderAttention(store *Store, now time.Time) string {
	type item struct {
		record Record
		age    int
	}
	var items []item
	for _, record := range store.WorkingSet() {
		switch record.Status {
		case StatusStale, StatusQuarantined, StatusProvisional:
			age, _ := record.AgeDays(now)
			items = append(items, item{record: record, age: age})
		}
	}
	if len(items) == 0 {
		return ""
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].age > items[j].age })
	remaining := 0
	if len(items) > sectionLimit {
		remaining = len(items) - sectionLimit
		items = items[:sectionLimit]
	}

	var b strings.Builder
	b.WriteString("\n## Needs attention\n\n")
	b.WriteString("These are not answers. Re-verify or retire them.\n\n")
	b.WriteString("| ID | Status | Age (days) | Record |\n")
	b.WriteString("| --- | --- | --- | --- |\n")
	for _, entry := range items {
		age := "unknown"
		if _, ok := entry.record.ObservedDate(); ok {
			age = fmt.Sprintf("%d", entry.age)
		}
		fmt.Fprintf(&b, "| `%s` | %s | %s | [%s](%s) |\n",
			entry.record.ID, entry.record.Status, age,
			filepath.Base(entry.record.Path), entry.record.Path)
	}
	if remaining > 0 {
		fmt.Fprintf(&b, "\n%d more waiting on review. The full queue: `vat brain review`.\n", remaining)
	}
	return b.String()
}

// recencyLimit is how many newly recorded decisions the index names beside the
// ranked table.
const recencyLimit = 5

// renderNewestDecisions names recent decisions the ranked table left out.
//
// The table keeps what the repository leans on hardest, which is the right cut
// for a bounded index and exactly the wrong one for "what was decided lately":
// a decision taken yesterday is cited by nothing yet, so ranking can only hide
// it. Reaching for the newest decision and not finding it is how a generated
// index gets read as stale and then stops being read.
func renderNewestDecisions(store *Store, shown []Record) string {
	listed := map[string]bool{}
	for _, record := range shown {
		listed[record.ID] = true
	}
	var candidates []Record
	for _, record := range store.OfKind(KindDecision) {
		if record.Archived || listed[record.ID] {
			continue
		}
		if record.Status == StatusActive || record.Status == StatusProvisional {
			candidates = append(candidates, record)
		}
	}
	if len(candidates) == 0 {
		return ""
	}
	// OfKind sorts by identifier, and identifiers are issued in order, so the
	// tail is the newest. Dates are not used: they are optional, and a record
	// with none would sort as the oldest thing in the repository.
	sort.SliceStable(candidates, func(i, j int) bool { return candidates[i].ID > candidates[j].ID })
	if len(candidates) > recencyLimit {
		candidates = candidates[:recencyLimit]
	}

	var b strings.Builder
	b.WriteString("\n## Newest decisions\n\n")
	b.WriteString("Recorded most recently, and not yet cited enough to rank above.\n\n")
	for _, record := range SortRecords(candidates) {
		fmt.Fprintf(&b, "- `%s` [%s](%s)\n", record.ID, escapePipes(record.Title), record.Path)
	}
	return b.String()
}

func renderRecentMemory(store *Store) string {
	memories := store.RecentMemories(7)
	if len(memories) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n## Recent observations\n\n")
	for _, record := range memories {
		fmt.Fprintf(&b, "- [%s](%s)\n", escapePipes(record.Title), record.Path)
	}
	return b.String()
}

func escapePipes(text string) string {
	return strings.ReplaceAll(strings.TrimSpace(text), "|", `\|`)
}

// GraphNode is one record in the exported knowledge graph.
type GraphNode struct {
	ID        string   `json:"id"`
	Kind      Kind     `json:"kind"`
	Status    Status   `json:"status"`
	Title     string   `json:"title"`
	Path      string   `json:"path"`
	OwnedBy   string   `json:"owned_by,omitempty"`
	SourceRef string   `json:"source_ref,omitempty"`
	Observed  string   `json:"observed_at,omitempty"`
	Refs      []string `json:"refs,omitempty"`
}

// GraphEdge is a directed relation between two records.
type GraphEdge struct {
	From string `json:"from"`
	To   string `json:"to"`
	Type string `json:"type"`
}

// Graph is the exported projection of the record relations.
type Graph struct {
	Generated string      `json:"generated_by"`
	Nodes     []GraphNode `json:"nodes"`
	Edges     []GraphEdge `json:"edges"`
}

// RenderGraph serialises the record relations. The graph is a projection for
// navigation, never a source of truth: if it disagrees with the Markdown, the
// Markdown wins and the graph is rebuilt.
func RenderGraph(store *Store) ([]byte, error) {
	graph := Graph{Generated: "vat brain build"}
	for _, record := range SortRecords(store.Records) {
		graph.Nodes = append(graph.Nodes, GraphNode{
			ID: record.ID, Kind: record.Kind, Status: record.Status,
			Title: record.Title, Path: record.Path, OwnedBy: record.OwnedBy,
			SourceRef: record.SourceRef, Observed: record.ObservedAt, Refs: record.Refs,
		})
		for _, ref := range record.Refs {
			graph.Edges = append(graph.Edges, GraphEdge{From: record.ID, To: ref, Type: "refs"})
		}
		for _, ref := range record.Supersedes {
			graph.Edges = append(graph.Edges, GraphEdge{From: record.ID, To: ref, Type: "supersedes"})
		}
		if record.SupersededBy != "" {
			graph.Edges = append(graph.Edges,
				GraphEdge{From: record.ID, To: record.SupersededBy, Type: "superseded_by"})
		}
	}
	if graph.Nodes == nil {
		graph.Nodes = []GraphNode{}
	}
	if graph.Edges == nil {
		graph.Edges = []GraphEdge{}
	}
	encoded, err := json.MarshalIndent(graph, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode %s: %w", GraphFile, err)
	}
	return append(encoded, '\n'), nil
}
