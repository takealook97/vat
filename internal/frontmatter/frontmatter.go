// Package frontmatter reads and writes the YAML header of a Markdown file.
// vat uses it for two kinds of record — agent roles and brain facts — which
// share the same shape: machine-readable metadata above a human-readable body.
package frontmatter

import (
	"bytes"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

const delimiter = "---"

// Document is a Markdown file split into its metadata and its prose.
type Document struct {
	// Raw is the undecoded YAML header, preserved so a rewrite can keep
	// key order and comments the caller did not touch.
	Raw string
	// Body is everything after the header.
	Body string
	// Present reports whether the file actually had a header.
	Present bool
}

// Split separates a Markdown file into header and body. A file without a
// header yields Present=false and the whole file as Body.
func Split(content string) Document {
	normalised := strings.ReplaceAll(content, "\r\n", "\n")
	if !strings.HasPrefix(normalised, delimiter+"\n") {
		return Document{Body: normalised}
	}
	rest := normalised[len(delimiter)+1:]
	end := strings.Index(rest, "\n"+delimiter)
	if end < 0 {
		return Document{Body: normalised}
	}
	header := rest[:end]
	body := rest[end+len(delimiter)+1:]
	// Drop the blank line conventionally left between the header and the body,
	// so callers see the document starting at its first real line.
	body = strings.TrimLeft(body, "\n")
	return Document{Raw: header, Body: body, Present: true}
}

// Decode parses a document's header into target, which must be a pointer.
// Unknown keys are tolerated: a record may carry metadata a newer vat
// understands, and refusing to read it would make the file unusable.
func (d Document) Decode(target any) error {
	if !d.Present {
		return nil
	}
	if err := yaml.Unmarshal([]byte(d.Raw), target); err != nil {
		return fmt.Errorf("parse front matter: %w", err)
	}
	return nil
}

// Fields decodes the header into a generic map, preserving scalar values as
// strings so a lint rule can check for presence without knowing the schema.
func (d Document) Fields() (map[string]any, error) {
	if !d.Present {
		return map[string]any{}, nil
	}
	fields := map[string]any{}
	if err := yaml.Unmarshal([]byte(d.Raw), &fields); err != nil {
		return nil, fmt.Errorf("parse front matter: %w", err)
	}
	return fields, nil
}

// String reads a header field as a scalar string, returning "" when absent.
func String(fields map[string]any, key string) string {
	value, ok := fields[key]
	if !ok || value == nil {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	default:
		return strings.TrimSpace(fmt.Sprintf("%v", typed))
	}
}

// Strings reads a header field as a list of strings, accepting either a YAML
// sequence or a single scalar.
func Strings(fields map[string]any, key string) []string {
	value, ok := fields[key]
	if !ok || value == nil {
		return nil
	}
	switch typed := value.(type) {
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if item == nil {
				continue
			}
			out = append(out, strings.TrimSpace(fmt.Sprintf("%v", item)))
		}
		return out
	case string:
		if strings.TrimSpace(typed) == "" {
			return nil
		}
		return []string{strings.TrimSpace(typed)}
	default:
		return []string{strings.TrimSpace(fmt.Sprintf("%v", typed))}
	}
}

// Render writes metadata and body back into a Markdown file.
func Render(metadata any, body string) ([]byte, error) {
	var buf bytes.Buffer
	encoded, err := yaml.Marshal(metadata)
	if err != nil {
		return nil, fmt.Errorf("encode front matter: %w", err)
	}
	buf.WriteString(delimiter + "\n")
	buf.Write(encoded)
	buf.WriteString(delimiter + "\n\n")
	buf.WriteString(strings.TrimLeft(body, "\n"))
	if !strings.HasSuffix(body, "\n") {
		buf.WriteString("\n")
	}
	return buf.Bytes(), nil
}

// Title returns the first Markdown heading in a body, or "" when there is none.
func Title(body string) string {
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "# ") {
			return strings.TrimSpace(strings.TrimPrefix(trimmed, "# "))
		}
		if strings.HasPrefix(trimmed, "## ") {
			return strings.TrimSpace(strings.TrimPrefix(trimmed, "## "))
		}
	}
	return ""
}
