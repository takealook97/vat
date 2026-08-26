package ui_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/takealook97/vat/internal/ui"
)

// Which stream a line goes to is part of the interface. Reports go to the
// output stream — headings and hints included, because they are part of the
// report a person reads — and only problems go to the error stream. `--json`
// stays pipeable because the commands emit the payload and return before any
// commentary is written, not because commentary is redirected.

func printer() (*ui.Printer, *bytes.Buffer, *bytes.Buffer) {
	var out, errOut bytes.Buffer
	return ui.NewWith(&out, &errOut, false), &out, &errOut
}

func TestOneReportArrivesOnOneStream(t *testing.T) {
	// Arrange: a report split across stdout and stderr interleaves unpredictably
	// when either is buffered, so the reader sees the hint before the table it
	// refers to. Everything that is part of the report goes to one place.
	p, out, errOut := printer()

	// Act
	p.Println("REPOSITORY  BRANCH")
	p.Heading("Result")
	p.Hint("Run `vat sync` to fast-forward what can be advanced safely.")

	// Assert
	for _, expected := range []string{"REPOSITORY", "Result", "vat sync"} {
		if !strings.Contains(out.String(), expected) {
			t.Errorf("%q is part of the report but did not reach the output stream: %q",
				expected, out.String())
		}
	}
	if errOut.Len() != 0 {
		t.Errorf("a report with no problems in it wrote to the error stream: %q", errOut.String())
	}
}

func TestOutAndErrHandBackTheStreamsTheyWereBuiltWith(t *testing.T) {
	// Arrange: commands write JSON straight to Out(), so a printer that handed
	// back the wrong writer would put the payload on stderr.
	p, out, errOut := printer()

	// Act
	if _, err := p.Out().Write([]byte("payload")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := p.Err().Write([]byte("commentary")); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Assert
	if out.String() != "payload" {
		t.Errorf("Out() wrote %q", out.String())
	}
	if errOut.String() != "commentary" {
		t.Errorf("Err() wrote %q", errOut.String())
	}
}

func TestPrintlnAndPrintfBothReachTheSameStream(t *testing.T) {
	// Arrange: the two are used interchangeably across the commands, so them
	// disagreeing on destination would split one report across two streams.
	p, out, _ := printer()

	// Act
	p.Println("first")
	p.Printf("%s\n", "second")

	// Assert
	if !strings.Contains(out.String(), "first") || !strings.Contains(out.String(), "second") {
		t.Errorf("the two writers disagreed on destination: %q", out.String())
	}
}

func TestWithColorLeavesTheOriginalPrinterAlone(t *testing.T) {
	// Arrange: printers are derived per command, and a derivation that mutated
	// its source would leak escape codes into a piped run elsewhere.
	p, out, _ := printer()

	// Act
	derived := p.WithColor(true)
	p.Status(ui.LevelOK, "console", "clean")

	// Assert
	if derived == p {
		t.Error("WithColor returned the same printer rather than a derived one")
	}
	if out.Len() == 0 {
		t.Fatal("the original printer wrote nothing, so this proves nothing about its colour")
	}
	if strings.Contains(out.String(), "\x1b[") {
		t.Errorf("the original printer emitted colour after a coloured one was derived: %q",
			out.String())
	}
}

func TestColorIsEmittedOnlyWhenItWasAskedFor(t *testing.T) {
	// Arrange: colour that survives a pipe corrupts every downstream consumer.
	var withColor, withoutColor bytes.Buffer
	ui.NewWith(&withColor, &withColor, true).Status(ui.LevelFail, "payments", "missing")
	ui.NewWith(&withoutColor, &withoutColor, false).Status(ui.LevelFail, "payments", "missing")

	// Assert
	if !strings.Contains(withColor.String(), "\x1b[") {
		t.Errorf("a printer built with colour emitted none: %q", withColor.String())
	}
	if strings.Contains(withoutColor.String(), "\x1b[") {
		t.Errorf("a printer built without colour emitted escape codes: %q", withoutColor.String())
	}
	if !strings.Contains(withoutColor.String(), "payments") {
		t.Errorf("turning colour off also removed the content: %q", withoutColor.String())
	}
}

func TestNewBuildsAPrinterAttachedToRealStreams(t *testing.T) {
	// Arrange: this is the constructor the binary itself uses, and it decides
	// whether colour is appropriate by inspecting the real streams.

	// Act
	p := ui.New()

	// Assert
	if p == nil {
		t.Fatal("New returned no printer")
	}
	if p.Out() == nil || p.Err() == nil {
		t.Error("the default printer has no streams to write to")
	}
}

func TestEveryLevelRendersADistinctLabel(t *testing.T) {
	// Arrange: the labels are the whole status vocabulary. Two levels sharing a
	// label would make a skip indistinguishable from a pass in every report.
	seen := map[string]ui.Level{}

	// Act & Assert
	for _, level := range []ui.Level{
		ui.LevelOK, ui.LevelWarn, ui.LevelFail, ui.LevelInfo, ui.LevelSkip,
	} {
		var out bytes.Buffer
		ui.NewWith(&out, &out, false).Status(level, "payments", "detail")
		fields := strings.Fields(out.String())
		if len(fields) == 0 {
			t.Fatalf("level %v printed nothing", level)
		}
		if previous, clash := seen[fields[0]]; clash {
			t.Errorf("levels %v and %v both render as %q", previous, level, fields[0])
		}
		seen[fields[0]] = level
	}
}
