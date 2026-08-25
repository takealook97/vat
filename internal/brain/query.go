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

	hits := []Hit{}
	for _, record := range store.Records {
		if record.Status.Terminal() && !opts.IncludeTerminal {
			continue
		}
		haystack := strings.ToLower(record.Title + "\n" + record.Body + "\n" + record.ID)
		score, excerpt := scoreText(haystack, record.Body, needles, contextLines)
		if score == 0 {
			continue
		}
		if record.Status.Answerable() {
			// Prefer a reviewed record over an unreviewed one at the same
			// textual relevance.
			score += 2
		}
		hits = append(hits, Hit{
			Path: record.Path, ID: record.ID, Status: record.Status,
			Title: record.Title, Score: score, Excerpt: excerpt,
		})
	}

	for _, path := range surfaceFiles(store.Root, opts.IncludeHistory) {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		body := string(data)
		score, excerpt := scoreText(strings.ToLower(body), body, needles, contextLines)
		if score == 0 {
			continue
		}
		hits = append(hits, Hit{
			Path: filepath.ToSlash(mustRel(store.Root, path)), Score: score, Excerpt: excerpt,
		})
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

func scoreText(lowered, original string, needles []string, contextLines int) (int, []string) {
	score := 0
	matched := 0
	for _, needle := range needles {
		count := strings.Count(lowered, needle)
		if count > 0 {
			matched++
			score += count
		}
	}
	if matched == 0 {
		return 0, nil
	}
	// A record containing every term is a better answer than one containing a
	// single term many times.
	score += matched * 5
	if matched < len(needles) {
		score -= (len(needles) - matched) * 2
	}
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
	for _, dir := range []string{"history", "archive", "analysis", "docs"} {
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
