package brain

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"sort"
	"strings"

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
func Build(store *Store, policy CheckPolicy) (BuildResult, error) {
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
		CurrentFile: []byte(RenderCurrent(store, policy)),
		GraphFile:   nil,
	}
	graph, err := RenderGraph(store, policy)
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
func Drift(store *Store, policy CheckPolicy) ([]string, error) {
	graph, err := RenderGraph(store, policy)
	if err != nil {
		return nil, err
	}

	// A file vat never wrote is not out of date with respect to the records;
	// it is not a projection at all. Calling it drift would offer `vat brain
	// build` as the repair, and the repair would delete somebody's work.
	foreign, err := Unmanaged(store.Root)
	if err != nil {
		return nil, err
	}

	var drifted []string
	for _, name := range Generated() {
		if slices.Contains(foreign, name) {
			continue
		}
		path := filepath.Join(store.Root, name)
		current, exists, err := fsx.ReadFileIfExists(path)
		if err != nil {
			return nil, err
		}
		if !exists {
			drifted = append(drifted, name)
			continue
		}
		expected := graph
		if name == CurrentFile {
			expected = []byte(RenderCurrent(store, policy))
		}
		// The same question the harness asks of its own generated files: a line
		// ending is not drift, and reporting it as one gave a Windows checkout
		// a permanently red `vat brain check` on files nobody had touched.
		if fsx.NormaliseNewlines(string(current)) != fsx.NormaliseNewlines(string(expected)) {
			drifted = append(drifted, name)
		}
	}
	sort.Strings(drifted)
	return drifted, nil
}

// RenderCurrent produces the bounded entry point: enough to find the right
// record, and deliberately not enough to answer from on its own.
func RenderCurrent(store *Store, policy CheckPolicy) string {
	var b strings.Builder
	b.WriteString("# Current index\n\n")
	b.WriteString(CurrentNotice + "\n\n")
	b.WriteString("Start every question here. Find the identifiers that matter, then open only\n")
	b.WriteString("those records. Reading the whole repository makes answers worse, not better:\n")
	b.WriteString("superseded reasoning and current fact become indistinguishable.\n\n")
	fmt.Fprintf(&b, "Observation window: %d days.\n\n", policy.StaleAfterDays)
	b.WriteString("For current-state claims, compare the citable-until date with today before citing.\n")
	b.WriteString("Stored status alone does not establish current citability.\n\n")

	b.WriteString(renderCounts(store))
	b.WriteString("\n")
	b.WriteString(renderCanonicalViews(store.Root))
	goals, _ := renderSection(store, policy, KindGoal, "Goals", "GOAL.md", func(r Record) bool {
		return !r.Status.Terminal() && (r.Status != StatusActive || projectionEligible(r))
	})
	b.WriteString(goals)
	gaps, _ := renderSection(store, policy, KindGap, "Open gaps", "GAP_ANALYSIS.md", func(r Record) bool {
		return !r.Status.Terminal() && (r.Status != StatusActive || projectionEligible(r))
	})
	b.WriteString(gaps)
	decisions, shownDecisions := renderSection(store, policy, KindDecision, "Active decisions", "DECISIONS.md",
		func(r Record) bool {
			return projectionEligible(r) || r.Status == StatusProvisional
		})
	b.WriteString(decisions)
	b.WriteString(renderNewestDecisions(store, shownDecisions, policy))

	b.WriteString(renderAttention(store, policy))
	b.WriteString(renderRecentMemory(store, policy))

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
		return "Reviewed; current-state claims also require an observation within the window."
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
func renderSection(store *Store, policy CheckPolicy, kind Kind, heading, projection string, include func(Record) bool) (string, []Record) {
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
	b.WriteString("| ID | Status | Citable until | Title | Record |\n")
	b.WriteString("| --- | --- | --- | --- | --- |\n")
	for _, record := range shown {
		fmt.Fprintf(&b, "| `%s` | %s | %s | %s | [%s](%s) |\n",
			record.ID, record.Status, citableUntil(record, policy), escapePipes(record.Title),
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

func renderAttention(store *Store, policy CheckPolicy) string {
	var records []Record
	for _, record := range store.WorkingSet() {
		if record.Status == StatusStale || record.Status == StatusQuarantined || record.Status == StatusProvisional || (record.Status == StatusActive && !projectionEligible(record)) {
			records = append(records, record)
		}
	}
	if len(records) == 0 {
		return ""
	}
	// Oldest observations come first without introducing a build clock.
	sort.SliceStable(records, func(i, j int) bool {
		left, _ := records[i].ObservedDate()
		right, _ := records[j].ObservedDate()
		return left.Before(right)
	})
	remaining := 0
	if len(records) > sectionLimit {
		remaining = len(records) - sectionLimit
		records = records[:sectionLimit]
	}
	var b strings.Builder
	b.WriteString("\n## Needs attention\n\n")
	b.WriteString("These are not answers. Re-verify or retire them.\n\n")
	b.WriteString("| ID | Status | Observed | Citable until | Record | Reason |\n")
	b.WriteString("| --- | --- | --- | --- | --- | --- |\n")
	for _, record := range records {
		observed := "unknown"
		if date, ok := record.ObservedDate(); ok {
			observed = date.Format("2006-01-02")
		}
		reason := ""
		if record.Status == StatusActive && !projectionEligible(record) {
			reason = "observation date missing or unreadable"
		}
		fmt.Fprintf(&b, "| `%s` | %s | %s | %s | [%s](%s) | %s |\n", record.ID, record.Status, observed, citableUntil(record, policy), filepath.Base(record.Path), record.Path, reason)
	}
	if remaining > 0 {
		fmt.Fprintf(&b, "\n%d more waiting on review. The full queue: `vat brain review`.\n", remaining)
	}
	return b.String()
}

// projectionEligible checks only record fields; live readers apply the age test.
func projectionEligible(record Record) bool {
	if record.Status != StatusActive {
		return false
	}
	if !record.IsCurrentStateClaim() {
		return true
	}
	_, ok := record.ObservedDate()
	return ok
}

func citableUntil(record Record, policy CheckPolicy) string {
	if !record.IsCurrentStateClaim() {
		return ""
	}
	observed, ok := record.ObservedDate()
	if !ok {
		return ""
	}
	return observed.AddDate(0, 0, policy.StaleAfterDays).Format("2006-01-02")
}

func expirySuffix(record Record, policy CheckPolicy) string {
	if until := citableUntil(record, policy); until != "" {
		return " (citable until " + until + ")"
	}
	return ""
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
func renderNewestDecisions(store *Store, shown []Record, policy CheckPolicy) string {
	listed := map[string]bool{}
	for _, record := range shown {
		listed[record.ID] = true
	}
	var candidates []Record
	for _, record := range store.OfKind(KindDecision) {
		if record.Archived || listed[record.ID] {
			continue
		}
		if projectionEligible(record) || record.Status == StatusProvisional {
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
		fmt.Fprintf(&b, "- `%s` [%s](%s)%s\n", record.ID, escapePipes(record.Title), record.Path, expirySuffix(record, policy))
	}
	return b.String()
}

func renderRecentMemory(store *Store, policy CheckPolicy) string {
	eligible := &Store{Root: store.Root}
	for _, record := range store.Records {
		if record.Status != StatusActive || projectionEligible(record) {
			eligible.Records = append(eligible.Records, record)
		}
	}
	memories := eligible.RecentMemories(7)
	if len(memories) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n## Recent observations\n\n")
	for _, record := range memories {
		fmt.Fprintf(&b, "- [%s](%s)%s\n", escapePipes(record.Title), record.Path, expirySuffix(record, policy))
	}
	return b.String()
}

func escapePipes(text string) string {
	return strings.ReplaceAll(strings.TrimSpace(text), "|", `\|`)
}

// GraphSchemaVersion changes when consumers must interpret node fields differently.
const GraphSchemaVersion = 2

// GraphNode is one record in the exported knowledge graph.
type GraphNode struct {
	CitableUntil string   `json:"citable_until,omitempty"`
	ID           string   `json:"id"`
	Kind         Kind     `json:"kind"`
	Status       Status   `json:"status"`
	Title        string   `json:"title"`
	Path         string   `json:"path"`
	OwnedBy      string   `json:"owned_by,omitempty"`
	SourceRef    string   `json:"source_ref,omitempty"`
	Observed     string   `json:"observed_at,omitempty"`
	Refs         []string `json:"refs,omitempty"`
	// ContentHash is Record.ContentHash, published.
	//
	// Never omitted when empty: an absent field would be indistinguishable from
	// a hash nobody computed, and an index cannot tell "unchanged" from "not
	// known" without re-reading the record — which is the work the field exists
	// to avoid.
	ContentHash string `json:"content_hash"`
}

// GraphEdge is a directed relation between two records.
type GraphEdge struct {
	From string `json:"from"`
	To   string `json:"to"`
	Type string `json:"type"`
}

// Graph is the exported projection of the record relations.
type Graph struct {
	Generated      string `json:"generated_by"`
	StaleAfterDays int    `json:"stale_after_days"`
	// SchemaVersion distinguishes consumers that understand read-time citability.
	SchemaVersion int         `json:"schema_version"`
	Nodes         []GraphNode `json:"nodes"`
	Edges         []GraphEdge `json:"edges"`
}

// RenderGraph serialises the record relations. The graph is a projection for
// navigation, never a source of truth: if it disagrees with the Markdown, the
// Markdown wins and the graph is rebuilt.
func RenderGraph(store *Store, policy CheckPolicy) ([]byte, error) {
	graph := Graph{
		Generated: "vat brain build", SchemaVersion: GraphSchemaVersion,
		StaleAfterDays: policy.StaleAfterDays,
	}
	for _, record := range SortRecords(store.Records) {
		graph.Nodes = append(graph.Nodes, GraphNode{
			ID: record.ID, Kind: record.Kind, Status: record.Status, CitableUntil: citableUntil(record, policy),
			Title: record.Title, Path: record.Path, OwnedBy: record.OwnedBy,
			SourceRef: record.SourceRef, Observed: record.ObservedAt, Refs: record.Refs,
			ContentHash: record.ContentHash,
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
