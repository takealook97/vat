// Package ui centralises vat's terminal output so every command reports state
// the same way: a fixed column layout, a small status vocabulary, and colour
// that disappears when the output is not a terminal.
package ui

import (
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"
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
		_, _ = fmt.Fprintf(p.out, "%s %s\n", label, subject)
		return
	}
	_, _ = fmt.Fprintf(p.out, "%s %s  %s\n", label, pad(subject, 24), detail)
}

// Table renders rows under headers with columns padded to their widest cell.
func (p *Printer) Table(headers []string, rows [][]string) {
	if len(headers) == 0 {
		return
	}
	widths := make([]int, len(headers))
	for i, header := range headers {
		widths[i] = utf8.RuneCountInString(header)
	}
	for _, row := range rows {
		for i, cell := range row {
			if i < len(widths) && utf8.RuneCountInString(cell) > widths[i] {
				widths[i] = utf8.RuneCountInString(cell)
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
	gap := width - utf8.RuneCountInString(text)
	if gap <= 0 {
		return text
	}
	return text + strings.Repeat(" ", gap)
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
