// Package frontmatter reads and writes the YAML header of a Markdown file.
// vat uses it for two kinds of record — agent roles and brain facts — which
// share the same shape: machine-readable metadata above a human-readable body.
package frontmatter

import (
	"bytes"
	"fmt"
	"reflect"
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

// Merge writes metadata back into the document's own header, keeping every key
// the caller's type does not model, in its original position, with its comments.
//
// Re-rendering the typed struct instead is lossy in a way nobody notices: a
// workspace that extended the schema, or a comment explaining why a value was
// chosen, disappears on the first lifecycle transition — a status change vat
// performed, not an edit a human made. A record vat rewrote must not come back
// smaller than the one it read.
//
// A field the type does model and left empty is removed, so clearing one is
// still possible. That distinction is why this needs the type's key set and not
// only its output; metadata that is not a struct declares no keys, and nothing
// is removed for it.
func (d Document) Merge(metadata any) ([]byte, error) {
	if !d.Present {
		return Render(metadata, d.Body)
	}
	var original yaml.Node
	if err := yaml.Unmarshal([]byte(d.Raw), &original); err != nil {
		return nil, fmt.Errorf("parse front matter: %w", err)
	}
	var updated yaml.Node
	if err := updated.Encode(metadata); err != nil {
		return nil, fmt.Errorf("encode front matter: %w", err)
	}
	merged := mergeMapping(mappingOf(&original), &updated, modelledKeys(metadata))

	var buf bytes.Buffer
	buf.WriteString(delimiter + "\n")
	encoded, err := yaml.Marshal(merged)
	if err != nil {
		return nil, fmt.Errorf("encode front matter: %w", err)
	}
	buf.Write(encoded)
	buf.WriteString(delimiter + "\n\n")
	buf.WriteString(strings.TrimLeft(d.Body, "\n"))
	if !strings.HasSuffix(d.Body, "\n") {
		buf.WriteString("\n")
	}
	return buf.Bytes(), nil
}

// mappingOf unwraps the document node yaml.Unmarshal produces, returning nil
// for a header that held no mapping at all.
func mappingOf(node *yaml.Node) *yaml.Node {
	if node.Kind == yaml.DocumentNode && len(node.Content) > 0 {
		node = node.Content[0]
	}
	if node.Kind != yaml.MappingNode {
		return nil
	}
	return node
}

func mergeMapping(original, updated *yaml.Node, modelled map[string]bool) *yaml.Node {
	if original == nil {
		return updated
	}
	merged := *original
	merged.Content = append([]*yaml.Node{}, original.Content...)

	position := map[string]int{}
	for i := 0; i+1 < len(merged.Content); i += 2 {
		position[merged.Content[i].Value] = i
	}

	written := map[string]bool{}
	for i := 0; i+1 < len(updated.Content); i += 2 {
		key, value := updated.Content[i], updated.Content[i+1]
		written[key.Value] = true
		at, exists := position[key.Value]
		if !exists {
			merged.Content = append(merged.Content, key, value)
			continue
		}
		// A comment hangs off the node it annotates, so a value replaced in
		// place has to carry the old one's annotations forward — otherwise the
		// merge deletes the very explanation it exists to keep.
		previous := merged.Content[at+1]
		value.HeadComment, value.LineComment, value.FootComment =
			previous.HeadComment, previous.LineComment, previous.FootComment
		merged.Content[at+1] = value
	}

	kept := merged.Content[:0]
	for i := 0; i+1 < len(merged.Content); i += 2 {
		key := merged.Content[i].Value
		if modelled[key] && !written[key] {
			continue
		}
		kept = append(kept, merged.Content[i], merged.Content[i+1])
	}
	merged.Content = kept
	return &merged
}

// modelledKeys returns the header keys a type declares, which is what lets a
// merge tell a field the caller cleared from a field it has never heard of.
func modelledKeys(metadata any) map[string]bool {
	keys := map[string]bool{}
	collectKeys(reflect.ValueOf(metadata), keys)
	return keys
}

func collectKeys(value reflect.Value, keys map[string]bool) {
	for value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return
		}
		value = value.Elem()
	}
	if value.Kind() != reflect.Struct {
		return
	}
	structType := value.Type()
	for i := 0; i < structType.NumField(); i++ {
		field := structType.Field(i)
		if !field.IsExported() {
			continue
		}
		name, options, _ := strings.Cut(field.Tag.Get("yaml"), ",")
		if name == "-" {
			continue
		}
		if strings.Contains(options, "inline") {
			collectKeys(value.Field(i), keys)
			continue
		}
		if name == "" {
			name = strings.ToLower(field.Name)
		}
		keys[name] = true
	}
}

// Render writes metadata and body into a new Markdown file. Use Merge to
// rewrite a file that already has a header.
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
