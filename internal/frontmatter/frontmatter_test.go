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
