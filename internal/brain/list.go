package brain

import (
	"sort"
	"time"
)

// Listing records by what they are rather than by what they say.
//
// query needs a word and ranks by relevance; graph.json is a committed
// projection that can lag the files. A reader that wants "every active
// decision for this project, now" had neither, and parsed front matter itself.
// List reads the files as they are and filters on status, kind, and extension
// values, and it applies citability at read time, as Citable does.

// ListOptions narrows a listing. Every set filter must hold.
type ListOptions struct {
	Status Status
	Kind   Kind
	// Where matches extension keys to exact string values.
	Where map[string]string
}

// ListEntry is one record in a listing. Every field is always present, so a
// consumer never has to tell an absent field from an empty one.
type ListEntry struct {
	Path           string         `json:"path"`
	ID             string         `json:"id"`
	Kind           Kind           `json:"kind"`
	Status         Status         `json:"status"`
	ClaimKind      ClaimKind      `json:"claim_kind"`
	Title          string         `json:"title"`
	Date           string         `json:"date"`
	ObservedAt     string         `json:"observed_at"`
	Citable        bool           `json:"citable"`
	CitableUntil   string         `json:"citable_until"`
	Supersedes     []string       `json:"supersedes"`
	SupersededBy   string         `json:"superseded_by"`
	Extensions     map[string]any `json:"extensions"`
	SourceExternal bool           `json:"source_external"`
}

// ListSkipped is a file that could not be read as a record. The reason is
// fixed text: a file vat could not parse may hold anything, a credential
// included, and a listing is exactly what gets pasted into a ticket.
type ListSkipped struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

// ListResult is a listing: the records that matched, and what was skipped.
type ListResult struct {
	Records []ListEntry   `json:"records"`
	Skipped []ListSkipped `json:"skipped"`
}

// SkippedUnreadable is the reason given for every file List could not read.
const SkippedUnreadable = "not readable as a record; run `vat brain check` for the problem"

// List returns every record matching opts, archived ones included, ordered by
// identifier and then path.
func List(store *Store, opts ListOptions, policy CheckPolicy, now time.Time) ListResult {
	result := ListResult{Records: []ListEntry{}, Skipped: []ListSkipped{}}
	for _, record := range store.Records {
		if !listMatches(record, opts) {
			continue
		}
		result.Records = append(result.Records, listEntry(record, policy, now))
	}
	sort.SliceStable(result.Records, func(i, j int) bool {
		a, b := result.Records[i], result.Records[j]
		if a.ID != b.ID {
			return a.ID < b.ID
		}
		return a.Path < b.Path
	})
	for _, malformed := range store.Malformed {
		result.Skipped = append(result.Skipped, ListSkipped{Path: malformed.Path, Reason: SkippedUnreadable})
	}
	return result
}

func listMatches(record Record, opts ListOptions) bool {
	if opts.Status != "" && record.Status != opts.Status {
		return false
	}
	if opts.Kind != "" && record.Kind != opts.Kind {
		return false
	}
	for key, want := range opts.Where {
		got, ok := record.Extensions[key].(string)
		if !ok || got != want {
			return false
		}
	}
	return true
}

func listEntry(record Record, policy CheckPolicy, now time.Time) ListEntry {
	supersedes := record.Supersedes
	if supersedes == nil {
		supersedes = []string{}
	}
	extensions := record.Extensions
	if extensions == nil {
		extensions = map[string]any{}
	}
	return ListEntry{
		Path: record.Path, ID: record.ID, Kind: record.Kind, Status: record.Status,
		ClaimKind: record.ClaimKind, Title: record.Title, Date: record.Date,
		ObservedAt: record.ObservedAt,
		Citable:    Citable(record, policy, now), CitableUntil: citableUntil(record, policy),
		Supersedes: supersedes, SupersededBy: record.SupersededBy,
		Extensions: extensions, SourceExternal: record.SourceExternal,
	}
}
