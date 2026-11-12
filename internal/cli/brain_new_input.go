package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/takealook97/vat/internal/brain"
)

// Input `vat brain new` takes beyond its flags: a piped body and extension
// keys. A writer that records automatically used to scaffold a record and then
// rewrite the file, which is two writes, a window in which the record has no
// content, and a path round every check `brain new` makes.

// extensionFlag collects repeated --set key=value pairs, in the order given.
type extensionFlag struct {
	keys   []string
	values map[string]string
	errs   []string
}

// String reports the keys collected so far, for the flag package.
func (f *extensionFlag) String() string { return strings.Join(f.keys, ",") }

// Set records one key=value pair. Problems are collected rather than returned,
// so every bad --set is named in one run.
func (f *extensionFlag) Set(raw string) error {
	key, value, found := strings.Cut(raw, "=")
	key = strings.TrimSpace(key)
	switch {
	case !found:
		f.errs = append(f.errs, fmt.Sprintf("--set %q needs the form key=value", raw))
	case key == "":
		f.errs = append(f.errs, fmt.Sprintf("--set %q has no key", raw))
	case brain.IsDefinedField(key):
		// Setting status or reviewed_by here would walk round promotion.
		f.errs = append(f.errs, fmt.Sprintf("--set %s: %s is a field vat defines; use its own flag or command", key, key))
	default:
		if f.values == nil {
			f.values = map[string]string{}
		}
		if _, repeated := f.values[key]; repeated {
			f.errs = append(f.errs, fmt.Sprintf("--set %s is given twice", key))
			return nil
		}
		f.keys = append(f.keys, key)
		f.values[key] = value
	}
	return nil
}

func (f *extensionFlag) validate() error {
	if len(f.errs) > 0 {
		return usageErrorf("%s", strings.Join(f.errs, "\n"))
	}
	return nil
}

// fields returns the extension keys as strings, which is what --set promises:
// a value that looks like a date or a number stays the text that was typed.
func (f *extensionFlag) fields() map[string]any {
	if len(f.keys) == 0 {
		return nil
	}
	fields := make(map[string]any, len(f.keys))
	for _, key := range f.keys {
		fields[key] = f.values[key]
	}
	return fields
}

// readBody reads the piped body and refuses one vat would have to rewrite.
func readBody(env *Env) (string, error) {
	data, err := io.ReadAll(env.stdin())
	if err != nil {
		return "", fmt.Errorf("read the body from standard input: %w", err)
	}
	body := strings.ReplaceAll(string(data), "\r\n", "\n")
	if strings.TrimSpace(body) == "" {
		return "", usageErrorf("--body-stdin read nothing; pipe the body in, or leave the flag off for the default")
	}
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(strings.TrimLeft(line, " "), "# ") {
			return "", usageErrorf("the body may not carry an H1: vat writes \"# <ID> — <title>\" from --title")
		}
	}
	return body, nil
}
