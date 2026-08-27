// Package ui centralises vat's terminal output so every command reports state
// the same way: a fixed column layout, a small status vocabulary, and colour
// that disappears when the output is not a terminal.
package ui

import (
	"fmt"
	"io"
	"os"
	"strings"
	"unicode"
)

// Level classifies a single reported observation.
type Level int

const (
	// LevelOK marks an observation that satisfies the rule it was checked against.
	LevelOK Level = iota
	// LevelInfo marks a neutral observation that needs no action.
	LevelInfo
	// LevelWarn marks an observation a human should look at but which does not
	// block the command.
	LevelWarn
	// LevelFail marks an observation that makes the command exit non-zero.
	LevelFail
	// LevelSkip marks work deliberately not performed.
	LevelSkip
)

// Label returns the fixed-width word vat prints for a level.
func (l Level) Label() string {
	switch l {
	case LevelOK:
		return "OK"
	case LevelInfo:
		return "INFO"
	case LevelWarn:
		return "WARN"
	case LevelFail:
		return "FAIL"
	case LevelSkip:
		return "SKIP"
	default:
		return "?"
	}
}

const (
	colorReset  = "\033[0m"
	colorRed    = "\033[31m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorBlue   = "\033[34m"
	colorGray   = "\033[90m"
	colorBold   = "\033[1m"
)

func (l Level) color() string {
	switch l {
	case LevelOK:
		return colorGreen
	case LevelWarn:
		return colorYellow
	case LevelFail:
		return colorRed
	case LevelInfo:
		return colorBlue
	case LevelSkip:
		return colorGray
	default:
		return ""
	}
}

// Printer writes vat output to a stream, applying colour only when enabled.
type Printer struct {
	out   io.Writer
	err   io.Writer
	color bool
	quiet bool
}

// New returns a Printer writing to stdout and stderr with colour auto-detected.
func New() *Printer {
	return &Printer{out: os.Stdout, err: os.Stderr, color: colorEnabled()}
}

// NewWith returns a Printer bound to explicit streams; used by tests.
func NewWith(out, errOut io.Writer, color bool) *Printer {
	return &Printer{out: out, err: errOut, color: color}
}

// WithQuiet returns a copy of the printer that suppresses non-essential lines.
func (p *Printer) WithQuiet(quiet bool) *Printer {
	clone := *p
	clone.quiet = quiet
	return &clone
}

// WithColor returns a copy of the printer with colour forced on or off.
func (p *Printer) WithColor(color bool) *Printer {
	clone := *p
	clone.color = color
	return &clone
}

// Out exposes the standard output stream for callers that render their own
// payloads, such as JSON encoders.
func (p *Printer) Out() io.Writer { return p.out }

// Err exposes the error stream.
func (p *Printer) Err() io.Writer { return p.err }

func (p *Printer) paint(code, text string) string {
	if !p.color || code == "" {
		return text
	}
	return code + text + colorReset
}

// Printf writes an unadorned formatted line to stdout.
func (p *Printer) Printf(format string, args ...any) {
	// Output is a terminal or a captured buffer; a write failure there is not
	// something a command can act on, and propagating it would push error
	// handling into every print site for no benefit.
	_, _ = fmt.Fprintf(p.out, format, args...)
}

// Println writes an unadorned line to stdout.
func (p *Printer) Println(args ...any) {
	_, _ = fmt.Fprintln(p.out, args...)
}

// Errorf writes a formatted line to stderr, prefixed so it is visible when
// interleaved with normal output.
func (p *Printer) Errorf(format string, args ...any) {
	_, _ = fmt.Fprintf(p.err, "%s %s\n", p.paint(colorRed, "error:"), fmt.Sprintf(format, args...))
}

// Warnf writes a warning line to stderr.
func (p *Printer) Warnf(format string, args ...any) {
	_, _ = fmt.Fprintf(p.err, "%s %s\n", p.paint(colorYellow, "warning:"), fmt.Sprintf(format, args...))
}

// Heading writes a section heading, skipped in quiet mode.
func (p *Printer) Heading(text string) {
	if p.quiet {
		return
	}
	_, _ = fmt.Fprintf(p.out, "\n%s\n", p.paint(colorBold, text))
}

// Hint writes a de-emphasised follow-up suggestion, skipped in quiet mode.
func (p *Printer) Hint(format string, args ...any) {
	if p.quiet {
		return
	}
	_, _ = fmt.Fprintf(p.out, "%s\n", p.paint(colorGray, fmt.Sprintf(format, args...)))
}

// Status writes one observation as "LEVEL  subject  detail".
func (p *Printer) Status(level Level, subject, detail string) {
	if p.quiet && (level == LevelOK || level == LevelInfo) {
		return
	}
	label := p.paint(level.color(), pad(level.Label(), 5))
	if detail == "" {
		_, _ = fmt.Fprintf(p.out, "%s %s\n", label, oneLine(subject))
		return
	}
	_, _ = fmt.Fprintf(p.out, "%s %s  %s\n", label, pad(oneLine(subject), 24), oneLine(detail))
}

// Table renders rows under headers with columns padded to their widest cell.
func (p *Printer) Table(headers []string, rows [][]string) {
	if len(headers) == 0 {
		return
	}
	// Collapsed before anything is measured. A cell holding a newline put the
	// rest of its row on the next line and lost every column after it, and the
	// values here are descriptions, objectives, and titles — free text somebody
	// typed, which reaches every table this tool prints.
	flattened := make([][]string, 0, len(rows))
	for _, row := range rows {
		cells := make([]string, len(row))
		for i, cell := range row {
			cells[i] = oneLine(cell)
		}
		flattened = append(flattened, cells)
	}
	rows = flattened

	widths := make([]int, len(headers))
	for i, header := range headers {
		widths[i] = displayWidth(header)
	}
	for _, row := range rows {
		for i, cell := range row {
			if i < len(widths) && displayWidth(cell) > widths[i] {
				widths[i] = displayWidth(cell)
			}
		}
	}
	var head strings.Builder
	for i, header := range headers {
		head.WriteString(pad(header, widths[i]))
		if i < len(headers)-1 {
			head.WriteString("  ")
		}
	}
	_, _ = fmt.Fprintln(p.out, p.paint(colorBold, strings.TrimRight(head.String(), " ")))
	for _, row := range rows {
		var line strings.Builder
		for i := range headers {
			cell := ""
			if i < len(row) {
				cell = row[i]
			}
			line.WriteString(pad(cell, widths[i]))
			if i < len(headers)-1 {
				line.WriteString("  ")
			}
		}
		_, _ = fmt.Fprintln(p.out, strings.TrimRight(line.String(), " "))
	}
}

func pad(text string, width int) string {
	gap := width - displayWidth(text)
	if gap <= 0 {
		return text
	}
	return text + strings.Repeat(" ", gap)
}

// displayWidth is how many terminal cells a string occupies.
//
// Padding by rune count shifted every column after a Korean group name or a
// Japanese description, in the output this tool prints most. A wide character
// takes two cells and a combining mark takes none, and counting each as one put
// the branch column eight cells out.
//
// The ranges are the East Asian Wide and Fullwidth blocks. This is deliberately
// a table here rather than a dependency: the count is one, and that is a
// security property. It is approximate at the edges — an emoji built from a
// zero-width joiner is measured as its parts — and approximate is what a
// terminal does with those anyway.
func displayWidth(text string) int {
	cells := 0
	for _, r := range text {
		switch {
		case r == 0:
		case unicode.In(r, unicode.Mn, unicode.Me, unicode.Cf):
			// Combining marks and format characters occupy no cell of their own.
		case isWide(r):
			cells += 2
		default:
			cells++
		}
	}
	return cells
}

// wideRanges are the code points a terminal renders two cells across: the East
// Asian Wide and Fullwidth categories, plus the emoji blocks that follow the
// same convention.
var wideRanges = [...][2]rune{
	{0x1100, 0x115F},   // Hangul Jamo initial consonants
	{0x2E80, 0x303E},   // CJK radicals, Kangxi, CJK symbols and punctuation
	{0x3041, 0x33FF},   // Hiragana, Katakana, Bopomofo, Hangul compatibility, CJK compatibility
	{0x3400, 0x4DBF},   // CJK unified ideographs extension A
	{0x4E00, 0x9FFF},   // CJK unified ideographs
	{0xA000, 0xA4CF},   // Yi
	{0xAC00, 0xD7A3},   // Hangul syllables
	{0xF900, 0xFAFF},   // CJK compatibility ideographs
	{0xFE10, 0xFE19},   // Vertical forms
	{0xFE30, 0xFE6F},   // CJK compatibility forms, small form variants
	{0xFF00, 0xFF60},   // Fullwidth forms
	{0xFFE0, 0xFFE6},   // Fullwidth signs
	{0x1F300, 0x1F64F}, // Miscellaneous symbols and pictographs, emoticons
	{0x1F900, 0x1F9FF}, // Supplemental symbols and pictographs
	{0x20000, 0x3FFFD}, // CJK unified ideographs extensions B and beyond
}

func isWide(r rune) bool {
	for _, span := range wideRanges {
		if r >= span[0] && r <= span[1] {
			return true
		}
	}
	return false
}

func colorEnabled() bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	if os.Getenv("TERM") == "dumb" {
		return false
	}
	info, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

// oneLine collapses a value onto a single terminal line.
//
// Every table and status line here is laid out in columns, and a value carrying
// a newline puts the rest of its row on the next line and loses every column
// after it. The values are free text somebody typed — a description, an
// objective, a record title — so this is reachable from most of what vat prints.
func oneLine(value string) string {
	if !strings.ContainsAny(value, "\r\n") {
		return value
	}
	return strings.Join(strings.Fields(value), " ")
}
