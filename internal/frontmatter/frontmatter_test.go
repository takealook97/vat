package frontmatter_test

import (
	"strings"
	"testing"

	"github.com/takealook97/vat/internal/frontmatter"
)

func TestSplitSeparatesTheHeaderFromTheBody(t *testing.T) {
	// Arrange
	source := "---\nid: D-0001\nstatus: active\n---\n\n# Title\n\nBody text.\n"

	// Act
	doc := frontmatter.Split(source)

	// Assert
	if !doc.Present {
		t.Fatal("a header was not detected")
	}
	if !strings.Contains(doc.Raw, "id: D-0001") {
		t.Errorf("header = %q", doc.Raw)
	}
	if !strings.HasPrefix(doc.Body, "# Title") {
		t.Errorf("body = %q", doc.Body)
	}
}

func TestSplitTreatsAFileWithNoHeaderAsAllBody(t *testing.T) {
	// Act
	doc := frontmatter.Split("# Just a document\n")

	// Assert
	if doc.Present {
		t.Error("a header was invented")
	}
	if doc.Body != "# Just a document\n" {
		t.Errorf("body = %q", doc.Body)
	}
}

func TestSplitToleratesWindowsLineEndings(t *testing.T) {
	// Act
	doc := frontmatter.Split("---\r\nid: D-0001\r\n---\r\n\r\n# Title\r\n")

	// Assert
	if !doc.Present {
		t.Fatal("a header with CRLF endings was not detected")
	}
	if !strings.Contains(doc.Raw, "id: D-0001") {
		t.Errorf("header = %q", doc.Raw)
	}
}

func TestDecodeToleratesUnknownKeys(t *testing.T) {
	// Arrange: a record may carry metadata a newer version understands, and
	// refusing to read it would make the file unusable.
	doc := frontmatter.Split("---\nid: D-0001\nfuture_field: something\n---\n\nBody\n")
	var target struct {
		ID string `yaml:"id"`
	}

	// Act
	err := doc.Decode(&target)

	// Assert
	if err != nil {
		t.Fatalf("Decode returned an error: %v", err)
	}
	if target.ID != "D-0001" {
		t.Errorf("id = %q", target.ID)
	}
}

func TestStringsAcceptsBothASequenceAndASingleScalar(t *testing.T) {
	// Arrange
	sequence, err := frontmatter.Split("---\nrefs: [A, B]\n---\n").Fields()
	if err != nil {
		t.Fatalf("Fields returned an error: %v", err)
	}
	scalar, err := frontmatter.Split("---\nrefs: A\n---\n").Fields()
	if err != nil {
		t.Fatalf("Fields returned an error: %v", err)
	}

	// Act & Assert
	if got := frontmatter.Strings(sequence, "refs"); len(got) != 2 {
		t.Errorf("sequence = %v, want two entries", got)
	}
	if got := frontmatter.Strings(scalar, "refs"); len(got) != 1 || got[0] != "A" {
		t.Errorf("scalar = %v, want [A]", got)
	}
}

func TestRenderRoundTripsThroughSplit(t *testing.T) {
	// Arrange
	metadata := struct {
		ID     string `yaml:"id"`
		Status string `yaml:"status"`
	}{ID: "D-0001", Status: "active"}

	// Act
	rendered, err := frontmatter.Render(metadata, "# Title\n\nBody.\n")
	if err != nil {
		t.Fatalf("Render returned an error: %v", err)
	}
	doc := frontmatter.Split(string(rendered))

	// Assert
	if !doc.Present {
		t.Fatal("the rendered document has no detectable header")
	}
	if frontmatter.Title(doc.Body) != "Title" {
		t.Errorf("title = %q, want Title", frontmatter.Title(doc.Body))
	}
}

// The accessors below are how every lint rule and every record reader gets at a
// header field without knowing the schema. They tolerate what a hand-edited
// Markdown file actually contains, and that tolerance is the part worth
// asserting: each branch decides whether a malformed record is reported or
// silently read as empty.

func TestStringReadsAScalarAndReportsAbsenceAsEmpty(t *testing.T) {
	// Arrange
	fields := map[string]any{
		"id":      "  D-0001  ",
		"blank":   nil,
		"count":   3,
		"enabled": true,
	}

	cases := []struct {
		key  string
		want string
	}{
		{"id", "D-0001"},
		{"blank", ""},
		{"absent", ""},
		{"count", "3"},
		{"enabled", "true"},
	}

	for _, testCase := range cases {
		// Act & Assert
		if got := frontmatter.String(fields, testCase.key); got != testCase.want {
			t.Errorf("String(%q) = %q, want %q", testCase.key, got, testCase.want)
		}
	}
}

func TestStringsToleratesNilBlankAndNonStringEntries(t *testing.T) {
	// Arrange: a record written by hand carries "repos: payments" as often as
	// it carries a list, and rejecting one of the two would make the file
	// unreadable rather than reporting a fixable problem.
	fields := map[string]any{
		"list":       []any{"payments", "  identity  ", nil},
		"scalar":     "payments",
		"blank":      "   ",
		"empty":      nil,
		"numeric":    7,
		"emptyArray": []any{},
	}

	cases := []struct {
		key  string
		want []string
	}{
		{"list", []string{"payments", "identity"}},
		{"scalar", []string{"payments"}},
		{"blank", nil},
		{"empty", nil},
		{"absent", nil},
		{"numeric", []string{"7"}},
		{"emptyArray", []string{}},
	}

	for _, testCase := range cases {
		// Act
		got := frontmatter.Strings(fields, testCase.key)

		// Assert
		if len(got) != len(testCase.want) {
			t.Errorf("Strings(%q) = %v, want %v", testCase.key, got, testCase.want)
			continue
		}
		for i := range got {
			if got[i] != testCase.want[i] {
				t.Errorf("Strings(%q)[%d] = %q, want %q", testCase.key, i, got[i], testCase.want[i])
			}
		}
	}
}

func TestDecodeAndFieldsAreNoOpsWhenThereIsNoHeaderAtAll(t *testing.T) {
	// Arrange: a Markdown file with no front matter is a valid document, not a
	// parse failure — the brain reads plenty of them.
	document := frontmatter.Split("# A note\n\nbody\n")

	// Act
	var target struct {
		ID string `yaml:"id"`
	}
	decodeErr := document.Decode(&target)
	fields, fieldsErr := document.Fields()

	// Assert
	if decodeErr != nil {
		t.Errorf("Decode on a headerless document returned %v", decodeErr)
	}
	if fieldsErr != nil {
		t.Errorf("Fields on a headerless document returned %v", fieldsErr)
	}
	if len(fields) != 0 {
		t.Errorf("Fields = %v, want an empty map rather than nil or a value", fields)
	}
	if target.ID != "" {
		t.Errorf("Decode populated %q from a document with no header", target.ID)
	}
}

func TestAMalformedHeaderIsReportedRatherThanReadAsEmpty(t *testing.T) {
	// Arrange: silently treating an unparseable header as absent is how a
	// record loses its id and is reported as a different problem entirely.
	document := frontmatter.Split("---\nid: [unclosed\n---\n\nbody\n")

	// Act
	var target struct {
		ID string `yaml:"id"`
	}
	decodeErr := document.Decode(&target)
	_, fieldsErr := document.Fields()

	// Assert
	if decodeErr == nil {
		t.Error("Decode accepted a malformed header")
	}
	if fieldsErr == nil {
		t.Error("Fields accepted a malformed header")
	}
}

func TestTitleFallsBackToASecondLevelHeadingAndThenToNothing(t *testing.T) {
	// Arrange & Act & Assert
	cases := []struct {
		name string
		body string
		want string
	}{
		{"first level", "intro\n\n#  A decision \n\n## later\n", "A decision"},
		{"second level when there is no first", "intro\n\n## A gap\n", "A gap"},
		{"no heading at all", "just prose\nand more\n", ""},
		{"a hash that is not a heading", "#no-space\n", ""},
	}

	for _, testCase := range cases {
		if got := frontmatter.Title(testCase.body); got != testCase.want {
			t.Errorf("%s: Title = %q, want %q", testCase.name, got, testCase.want)
		}
	}
}

// A record vat rewrote must not come back smaller than the one it read. The
// typed struct is a partial view of the header — a workspace extends the schema
// and a comment explains a value — and re-marshalling that view drops
// everything outside it on the first lifecycle transition, with no error and no
// diff anyone reads.
func TestMergeKeepsHeaderKeysTheCallerDoesNotModel(t *testing.T) {
	// Arrange
	source := "---\n" +
		"id: G-0014\n" +
		"# graded by the payments team, not by vat\n" +
		"confidence: high\n" +
		"status: active\n" +
		"tags: [payments, retries]\n" +
		"---\n\n# G-0014 — Retries double-submit\n"
	doc := frontmatter.Split(source)
	type modelled struct {
		ID     string `yaml:"id"`
		Status string `yaml:"status"`
	}

	// Act
	rendered, err := doc.Merge(modelled{ID: "G-0014", Status: "stale"})
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	result := string(rendered)

	// Assert
	if !strings.Contains(result, "status: stale") {
		t.Errorf("the modelled field was not updated:\n%s", result)
	}
	for _, kept := range []string{"confidence: high", "tags: [payments, retries]",
		"# graded by the payments team, not by vat"} {
		if !strings.Contains(result, kept) {
			t.Errorf("merge dropped %q:\n%s", kept, result)
		}
	}
	if !strings.Contains(result, "# G-0014 — Retries double-submit") {
		t.Errorf("the body did not survive:\n%s", result)
	}
}

// The other half of the same guarantee: a field the caller does model and
// deliberately cleared has to leave, or a superseded_by that was unset would
// stay on disk and the check rules would keep reporting a link that no longer
// exists.
func TestMergeRemovesAModelledFieldTheCallerCleared(t *testing.T) {
	// Arrange
	doc := frontmatter.Split("---\nid: D-0001\nstatus: active\nsuperseded_by: D-0002\n---\n\n# D-0001\n")
	type modelled struct {
		ID           string `yaml:"id"`
		Status       string `yaml:"status"`
		SupersededBy string `yaml:"superseded_by,omitempty"`
	}

	// Act
	rendered, err := doc.Merge(modelled{ID: "D-0001", Status: "active"})
	if err != nil {
		t.Fatalf("merge: %v", err)
	}

	// Assert
	if strings.Contains(string(rendered), "superseded_by") {
		t.Errorf("a cleared field stayed on disk:\n%s", rendered)
	}
}

func TestMergeWritesAHeaderWhenTheDocumentHadNone(t *testing.T) {
	// Arrange
	doc := frontmatter.Split("# Only a body\n")

	// Act
	rendered, err := doc.Merge(struct {
		ID string `yaml:"id"`
	}{ID: "M-0001"})
	if err != nil {
		t.Fatalf("merge: %v", err)
	}

	// Assert
	if !strings.HasPrefix(string(rendered), "---\nid: M-0001\n---\n") {
		t.Errorf("no header was written:\n%s", rendered)
	}
}
