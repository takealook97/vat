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
	// count numbers the --set flags, so a refusal can say which one it means
	// without repeating the argument.
	count int
}

// String reports the keys collected so far, for the flag package.
func (f *extensionFlag) String() string { return strings.Join(f.keys, ",") }

// Set records one key=value pair. Problems are collected rather than returned,
// so every bad --set is named in one run.
//
// A refusal never repeats the argument. `--set` is where a script passes what
// it copied, and an argument missing its "=" is as likely to be a token as a
// key; printing it put the token into the terminal and every log of the run.
// The flag is named by its position, and a key only once the credential scan
// finds nothing in it.
func (f *extensionFlag) Set(raw string) error {
	f.count++
	position := fmt.Sprintf("--set #%d", f.count)
	key, value, found := strings.Cut(raw, "=")
	key = strings.TrimSpace(key)
	switch {
	case !found:
		f.errs = append(f.errs, position+" needs the form key=value")
	case key == "":
		f.errs = append(f.errs, position+" has no key")
	case brain.IsDefinedField(key):
		// Setting status or reviewed_by here would walk round promotion.
		f.errs = append(f.errs, definedFieldRefusal(key))
	default:
		if f.values == nil {
			f.values = map[string]string{}
		}
		if _, repeated := f.values[key]; repeated {
			f.errs = append(f.errs, repeatedKeyRefusal(position, key))
			return nil
		}
		f.keys = append(f.keys, key)
		f.values[key] = value
	}
	return nil
}

func repeatedKeyRefusal(position, key string) string {
	if brain.DisplayableKey(key) {
		return fmt.Sprintf("%s gives %s a second time", position, key)
	}
	return position + " repeats the key of an earlier --set"
}

// definedFieldFlags names the brain new flag that writes a defined field.
// source_url and source_quote were extension keys before they were fields, so
// scripts written then still pass them through --set; the refusal has to say
// where they went.
var definedFieldFlags = map[string]string{
	"source_url":      "--source-url",
	"source_quote":    "--source-quote",
	"source_external": "--source-external",
	"claim_kind":      "--claim",
	"owned_by":        "--owner",
	"axis":            "--axis",
	"refs":            "--refs",
	"id":              "--id",
}

func definedFieldRefusal(key string) string {
	if flag, ok := definedFieldFlags[key]; ok {
		return fmt.Sprintf("--set %s: %s is a field vat defines; pass it with %s", key, key, flag)
	}
	return fmt.Sprintf("--set %s: %s is a field vat defines; use its own flag or command", key, key)
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
