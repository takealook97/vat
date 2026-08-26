package brain

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Hit is one search result.
type Hit struct {
	Path    string   `json:"path"`
	ID      string   `json:"id,omitempty"`
	Status  Status   `json:"status,omitempty"`
	Title   string   `json:"title,omitempty"`
	Score   int      `json:"score"`
	Excerpt []string `json:"excerpt,omitempty"`
}

// QueryOptions bound a search.
type QueryOptions struct {
	// IncludeHistory widens the search to archived and historical material.
	// The default surface is deliberately narrow: an answer assembled from
	// superseded reasoning is worse than no answer.
	IncludeHistory bool
	// IncludeTerminal includes superseded, revoked, and resolved records.
	IncludeTerminal bool
	Limit           int
	// ContextLines is how many matching lines to quote per hit.
	ContextLines int
}

// Query searches the bounded surface for every term, ranking records that match
// more terms higher and preferring answerable records.
func Query(store *Store, terms []string, opts QueryOptions) []Hit {
	if len(terms) == 0 {
		return nil
	}
	needles := make([]string, 0, len(terms))
	for _, term := range terms {
		if trimmed := strings.ToLower(strings.TrimSpace(term)); trimmed != "" {
			needles = append(needles, trimmed)
		}
	}
	if len(needles) == 0 {
		return nil
	}
	limit := opts.Limit
	if limit <= 0 {
		limit = 20
	}
	contextLines := opts.ContextLines
	if contextLines <= 0 {
		contextLines = 2
	}

	type candidate struct {
		hit      Hit
		haystack string
		body     string
		length   int
	}
	candidates := []candidate{}
	total := 0
	add := func(hit Hit, haystack, body string) {
		length := len(strings.Fields(haystack))
		total += length
		candidates = append(candidates, candidate{hit: hit, haystack: haystack, body: body, length: length})
	}

	for _, record := range store.Records {
		if record.Status.Terminal() && !opts.IncludeTerminal {
			continue
		}
		add(Hit{Path: record.Path, ID: record.ID, Status: record.Status, Title: record.Title},
			strings.ToLower(record.Title+"\n"+record.Body+"\n"+record.ID), record.Body)
	}
	for _, path := range surfaceFiles(store.Root, opts.IncludeHistory) {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		add(Hit{Path: filepath.ToSlash(mustRel(store.Root, path))},
			strings.ToLower(string(data)), string(data))
	}

	average := 1.0
	if len(candidates) > 0 && total > 0 {
		average = float64(total) / float64(len(candidates))
	}

	hits := []Hit{}
	for _, entry := range candidates {
		score, excerpt := scoreText(entry.haystack, entry.body, needles, contextLines,
			float64(entry.length)/average)
		if score == 0 {
			continue
		}
		if entry.hit.Status.Answerable() {
			// Prefer a reviewed record over an unreviewed one at the same
			// textual relevance.
			score += 5
		}
		entry.hit.Score = score
		entry.hit.Excerpt = excerpt
		hits = append(hits, entry.hit)
	}

	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].Score != hits[j].Score {
			return hits[i].Score > hits[j].Score
		}
		return hits[i].Path < hits[j].Path
	})
	if len(hits) > limit {
		hits = hits[:limit]
	}
	return hits
}

// Term-frequency saturation and length normalisation, the two constants of the
// BM25 family. k1 decides how quickly repeating a word stops helping; b decides
// how much a long document is discounted for being long.
const (
	termSaturation   = 1.2
	lengthNormalised = 0.75
	// coverageWeight makes answering every term worth more than any amount of
	// repetition, which is the whole point: someone searching three words wants
	// the record about all three.
	coverageWeight = 30
	densityWeight  = 10
)

// scoreText ranks one document against the query terms, discounting length.
//
// Counting raw occurrences instead is arithmetic, not relevance: a long record
// repeating one query word out-scores a short record that answers all three.
// The long record is usually the sprawling one nobody has split up yet, so the
// naive ranking prefers precisely the least useful document in the repository.
//
// relativeLength is the document's length over the average across the surface.
func scoreText(lowered, original string, needles []string, contextLines int, relativeLength float64) (int, []string) {
	density := 0.0
	matched := 0
	for _, needle := range needles {
		count := float64(strings.Count(lowered, needle))
		if count == 0 {
			continue
		}
		matched++
		density += count * (termSaturation + 1) /
			(count + termSaturation*(1-lengthNormalised+lengthNormalised*relativeLength))
	}
	if matched == 0 {
		return 0, nil
	}
	score := matched*coverageWeight + int(density*densityWeight)
	if score < 1 {
		score = 1
	}
	return score, excerptLines(original, needles, contextLines)
}

func excerptLines(body string, needles []string, limit int) []string {
	var excerpt []string
	for _, line := range strings.Split(body, "\n") {
		lowered := strings.ToLower(line)
		for _, needle := range needles {
			if strings.Contains(lowered, needle) {
				trimmed := strings.TrimSpace(line)
				if trimmed != "" {
					excerpt = append(excerpt, trimmed)
				}
				break
			}
		}
		if len(excerpt) >= limit {
			break
		}
	}
	return excerpt
}

// defaultSurface is what a plain query reads: the generated index and the
// hand-written projections at the root.
// The generated index is deliberately absent: it only restates record titles,
// so including it would rank a pointer above the record it points at.
func defaultSurface() []string {
	return []string{"GOAL.md", "STATUS.md", "ROADMAP.md",
		"DECISIONS.md", "GAP_ANALYSIS.md", "MEMORY.md", "AGENT_OPERATING_MODEL.md"}
}

func surfaceFiles(root string, includeHistory bool) []string {
	var paths []string
	for _, name := range defaultSurface() {
		path := filepath.Join(root, name)
		if _, err := os.Stat(path); err == nil {
			paths = append(paths, path)
		}
	}
	if !includeHistory {
		return paths
	}
	// archive/ is deliberately absent: its records are loaded as records, and
	// walking it here would rank each archived record twice.
	for _, dir := range []string{"history", "analysis", "docs"} {
		full := filepath.Join(root, dir)
		_ = filepath.Walk(full, func(path string, info os.FileInfo, err error) error {
			if err != nil || info == nil || info.IsDir() {
				return nil //nolint:nilerr // a missing optional directory is not an error
			}
			if strings.HasSuffix(path, ".md") {
				paths = append(paths, path)
			}
			return nil
		})
	}
	return paths
}

func mustRel(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return path
	}
	return rel
}
