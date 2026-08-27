package ui_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/takealook97/vat/internal/ui"
)

func newPrinter() (*ui.Printer, *bytes.Buffer, *bytes.Buffer) {
	var out, errOut bytes.Buffer
	return ui.NewWith(&out, &errOut, false), &out, &errOut
}

func TestStatusPrintsTheLevelSubjectAndDetail(t *testing.T) {
	// Arrange
	printer, out, _ := newPrinter()

	// Act
	printer.Status(ui.LevelFail, "payments", "origin does not match")

	// Assert
	line := out.String()
	for _, want := range []string{"FAIL", "payments", "origin does not match"} {
		if !strings.Contains(line, want) {
			t.Errorf("output %q is missing %q", line, want)
		}
	}
}

func TestQuietSuppressesRoutineLinesButNeverProblems(t *testing.T) {
	// Arrange: --quiet exists so a scheduled run reports only what needs acting
	// on. Hiding a failure would defeat it entirely.
	printer, out, _ := newPrinter()
	quiet := printer.WithQuiet(true)

	// Act
	quiet.Status(ui.LevelOK, "payments", "clean")
	quiet.Status(ui.LevelInfo, "console", "noted")
	quiet.Status(ui.LevelWarn, "docs", "on another branch")
	quiet.Status(ui.LevelFail, "brain", "not cloned")

	// Assert
	text := out.String()
	if strings.Contains(text, "payments") || strings.Contains(text, "console") {
		t.Errorf("quiet mode printed routine output: %q", text)
	}
	for _, want := range []string{"docs", "brain"} {
		if !strings.Contains(text, want) {
			t.Errorf("quiet mode suppressed a problem: %q is missing from %q", want, text)
		}
	}
}

func TestWithQuietDoesNotAlterTheOriginalPrinter(t *testing.T) {
	// Arrange
	printer, out, _ := newPrinter()

	// Act
	_ = printer.WithQuiet(true)
	printer.Status(ui.LevelOK, "payments", "clean")

	// Assert
	if !strings.Contains(out.String(), "payments") {
		t.Error("WithQuiet mutated the printer it was called on")
	}
}

func TestTableAlignsColumnsToTheirWidestCell(t *testing.T) {
	// Arrange
	printer, out, _ := newPrinter()

	// Act
	printer.Table(
		[]string{"REPOSITORY", "STATE"},
		[][]string{{"payments", "clean"}, {"a-much-longer-name", "dirty"}},
	)

	// Assert
	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("line count = %d, want a header and two rows", len(lines))
	}
	first := strings.Index(lines[1], "clean")
	second := strings.Index(lines[2], "dirty")
	if first != second {
		t.Errorf("the second column is not aligned: %d vs %d\n%s", first, second, out.String())
	}
}

func TestTableToleratesARowShorterThanItsHeader(t *testing.T) {
	// Arrange: a missing trailing cell should print as blank, not panic.
	printer, out, _ := newPrinter()

	// Act
	printer.Table([]string{"A", "B", "C"}, [][]string{{"one"}})

	// Assert
	if !strings.Contains(out.String(), "one") {
		t.Errorf("short row was not rendered: %q", out.String())
	}
}

func TestTableHandlesWideCharactersWithoutMiscounting(t *testing.T) {
	// Arrange: padding counts runes, not bytes, so a multi-byte name must not
	// throw the columns off.
	printer, out, _ := newPrinter()

	// Act
	printer.Table([]string{"NAME", "STATE"}, [][]string{{"결제", "clean"}, {"console", "dirty"}})

	// Assert
	if !strings.Contains(out.String(), "결제") {
		t.Errorf("multi-byte content was lost: %q", out.String())
	}
}

func TestErrorfAndWarnfWriteToTheErrorStream(t *testing.T) {
	// Arrange: findings go to stdout so they can be piped; diagnostics about
	// the invocation go to stderr so they do not corrupt that pipe.
	printer, out, errOut := newPrinter()

	// Act
	printer.Errorf("could not open %s", "vat.yaml")
	printer.Warnf("origin is unset")

	// Assert
	if out.Len() != 0 {
		t.Errorf("diagnostics leaked into stdout: %q", out.String())
	}
	text := errOut.String()
	for _, want := range []string{"error:", "vat.yaml", "warning:", "origin is unset"} {
		if !strings.Contains(text, want) {
			t.Errorf("stderr %q is missing %q", text, want)
		}
	}
}

func TestLevelLabelsAreStable(t *testing.T) {
	// These appear in output that people grep, so they are part of the contract.
	cases := map[ui.Level]string{
		ui.LevelOK:   "OK",
		ui.LevelInfo: "INFO",
		ui.LevelWarn: "WARN",
		ui.LevelFail: "FAIL",
		ui.LevelSkip: "SKIP",
	}
	for level, want := range cases {
		if got := level.Label(); got != want {
			t.Errorf("Label() = %q, want %q", got, want)
		}
	}
}

func TestColourIsOmittedWhenDisabled(t *testing.T) {
	// Arrange
	printer, out, _ := newPrinter()

	// Act
	printer.Status(ui.LevelFail, "payments", "broken")

	// Assert
	if strings.Contains(out.String(), "\033[") {
		t.Errorf("escape codes were emitted with colour disabled: %q", out.String())
	}
}

// Columns were padded by counting runes. A wide character occupies two terminal
// cells, so a Korean group name or a Japanese description shifted every column
// after it — in the output this tool prints most: `vat repo list`, `vat status`,
// `vat harness skills`.
func TestATableAlignsColumnsByWhatTheTerminalShows(t *testing.T) {
	// Arrange
	printer, out, _ := newPrinter()

	// Act
	printer.Table(
		[]string{"NAME", "GROUP", "BRANCH"},
		[][]string{
			{"orders", "ops", "main"},
			{"payments", "결제팀", "main"},
			{"console", "プラットフォーム", "main"},
		},
	)

	// Assert: every row's last column starts in the same terminal cell.
	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	if len(lines) != 4 {
		t.Fatalf("expected a header and three rows, got %d lines:\n%s", len(lines), out.String())
	}
	want := -1
	for _, line := range lines {
		at := displayIndexOf(line, "main")
		if at < 0 {
			continue
		}
		if want < 0 {
			want = at
			continue
		}
		if at != want {
			t.Errorf("the branch column starts at cell %d on one row and %d on another:\n%s",
				want, at, out.String())
		}
	}
	if want < 0 {
		t.Fatalf("no row carried the branch column:\n%s", out.String())
	}
}

// displayIndexOf returns the terminal cell a substring begins at, counting a
// wide character as the two cells it occupies.
func displayIndexOf(line, want string) int {
	at := strings.Index(line, want)
	if at < 0 {
		return -1
	}
	cells := 0
	for _, r := range line[:at] {
		cells++
		if r >= 0x1100 && (r <= 0x115F || (r >= 0x2E80 && r <= 0xA4CF) ||
			(r >= 0xAC00 && r <= 0xD7A3) || (r >= 0xF900 && r <= 0xFAFF) ||
			(r >= 0xFF00 && r <= 0xFF60) || (r >= 0xFFE0 && r <= 0xFFE6)) {
			cells++
		}
	}
	return cells
}
