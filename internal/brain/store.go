package brain

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

// Generated file names. These are projections rebuilt from the atomic records
// and must never be hand-edited; the check rules treat a hand edit as drift.
const (
	CurrentFile = "CURRENT.md"
	GraphFile   = "graph.json"
	MarkerFile  = ".brain"
)

// Store is a loaded brain repository.
type Store struct {
	Root    string
	Records []Record
}

// IsBrain reports whether a directory looks like a brain repository: it has
// the marker file, or at least one of the atomic record directories.
func IsBrain(root string) bool {
	if fsx.Exists(filepath.Join(root, MarkerFile)) {
		return true
	}
	for _, kind := range Kinds() {
		if fsx.IsDir(filepath.Join(root, kind.Dir())) {
			return true
		}
	}
	return false
}

// Load reads every atomic record under root.
func Load(root string) (*Store, error) {
	store := &Store{Root: filepath.Clean(root)}
	for _, kind := range Kinds() {
		records, err := loadKind(store.Root, kind)
		if err != nil {
			return nil, err
		}
		store.Records = append(store.Records, records...)
	}
	return store, nil
}

func loadKind(root string, kind Kind) ([]Record, error) {
	dir := filepath.Join(root, kind.Dir())
	if !fsx.IsDir(dir) {
		return nil, nil
	}
	var records []Record
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			return nil
		}
		if strings.HasPrefix(entry.Name(), "_") || entry.Name() == "README.md" {
			return nil
		}
		record, err := loadRecord(root, kind, path)
		if err != nil {
			return err
		}
		records = append(records, record)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("scan %s: %w", dir, err)
	}
	return records, nil
}

func loadRecord(root string, kind Kind, path string) (Record, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Record{}, fmt.Errorf("read %s: %w", path, err)
	}
	doc := frontmatter.Split(string(data))
	var metadata Metadata
	if err := doc.Decode(&metadata); err != nil {
		return Record{}, fmt.Errorf("%s: %w", relTo(root, path), err)
	}
	relative := relTo(root, path)
	if metadata.ID == "" {
		metadata.ID = inferID(filepath.Base(path))
	}
	if metadata.Status == "" {
		// A record with no declared status is not assumed true. It enters the
		// lifecycle as provisional so the check rules surface it.
		metadata.Status = StatusProvisional
	}
	title := frontmatter.Title(doc.Body)
	if title == "" {
		title = metadata.ID
	}
	return Record{
		Metadata: metadata,
		Kind:     kind,
		Path:     relative,
		Title:    cleanTitle(title, metadata.ID),
		Body:     doc.Body,
	}, nil
}

func relTo(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return path
	}
	return filepath.ToSlash(rel)
}

func inferID(fileName string) string {
	stem := strings.TrimSuffix(fileName, ".md")
	if head, _, ok := strings.Cut(stem, "-"); ok {
		// "D-0042-something" keeps the two leading segments; "0042-something"
		// keeps only the number.
		if len(head) == 1 {
			rest := strings.TrimPrefix(stem, head+"-")
			if number, _, ok := strings.Cut(rest, "-"); ok {
				return head + "-" + number
			}
			return stem
		}
		return head
	}
	return stem
}

func cleanTitle(title, id string) string {
	trimmed := strings.TrimSpace(title)
	trimmed = strings.TrimPrefix(trimmed, id)
	trimmed = strings.TrimLeft(trimmed, " —-:·")
	if trimmed == "" {
		return title
	}
	return trimmed
}

// ByID indexes the store's records.
func (s *Store) ByID() map[string]Record {
	index := make(map[string]Record, len(s.Records))
	for _, record := range s.Records {
		index[record.ID] = record
	}
	return index
}

// OfKind returns every record of one kind, sorted by identifier.
func (s *Store) OfKind(kind Kind) []Record {
	var matching []Record
	for _, record := range s.Records {
		if record.Kind == kind {
			matching = append(matching, record)
		}
	}
	return SortRecords(matching)
}

// Answerable returns the records that may be cited as current truth.
func (s *Store) Answerable() []Record {
	var matching []Record
	for _, record := range s.Records {
		if record.Status.Answerable() {
			matching = append(matching, record)
		}
	}
	return SortRecords(matching)
}

// WithStatus returns every record in a given status.
func (s *Store) WithStatus(status Status) []Record {
	var matching []Record
	for _, record := range s.Records {
		if record.Status == status {
			matching = append(matching, record)
		}
	}
	return SortRecords(matching)
}

// CurrentStateClaims returns records that assert something about the present
// and are therefore subject to expiry.
func (s *Store) CurrentStateClaims() []Record {
	var matching []Record
	for _, record := range s.Records {
		if record.IsCurrentStateClaim() && !record.Status.Terminal() {
			matching = append(matching, record)
		}
	}
	return SortRecords(matching)
}

// ReferenceCounts returns how many other records point at each identifier. It
// is the weight used to prioritise the review queue: a claim nothing cites can
// wait, a claim everything cites cannot.
func (s *Store) ReferenceCounts() map[string]int {
	counts := map[string]int{}
	for _, record := range s.Records {
		for _, ref := range record.Refs {
			counts[ref]++
		}
		for _, ref := range record.Supersedes {
			counts[ref]++
		}
		if record.SupersededBy != "" {
			counts[record.SupersededBy]++
		}
	}
	return counts
}

// NextID returns the next free identifier for a kind, formatted to match the
// widest existing identifier so sorting stays lexicographic.
func (s *Store) NextID(kind Kind) string {
	highest := 0
	width := 4
	for _, record := range s.OfKind(kind) {
		if value := numericID(record.ID); value > highest {
			highest = value
		}
		if digits := len(strings.TrimLeft(trailingDigits(record.ID), "")); digits > 0 && digits > width {
			width = digits
		}
	}
	return fmt.Sprintf("%s-%0*d", kind.Prefix(), width, highest+1)
}

func trailingDigits(id string) string {
	digits := ""
	for i := len(id) - 1; i >= 0; i-- {
		if id[i] >= '0' && id[i] <= '9' {
			digits = string(id[i]) + digits
			continue
		}
		break
	}
	return digits
}

// RecentMemories returns the newest dated memory records.
func (s *Store) RecentMemories(limit int) []Record {
	memories := s.OfKind(KindMemory)
	sort.SliceStable(memories, func(i, j int) bool {
		return memories[i].Path > memories[j].Path
	})
	if len(memories) > limit {
		return memories[:limit]
	}
	return memories
}
