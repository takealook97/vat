package brain

import (
	"errors"
	"fmt"
	"math"
	"sort"

	"gopkg.in/yaml.v3"

	"github.com/takealook97/vat/internal/frontmatter"
)

// yamlStringTag is the tag of a plain YAML string, which is the only key an
// extension mapping may use and the tag --set values are written with.
const yamlStringTag = "!!str"

// Extension keys, carried into graph.json.
//
// SPEC 5.6.1 lets a record hold keys this package does not define, and until
// now they survived every rewrite but stopped at the loader: a reader of
// graph.json had to re-parse every record to see them, which is the parsing
// the graph exists to save. They are carried as display data only. Nothing
// here reads one to decide a status, a citability, or an order.

// Header keys a record carries beyond the ones vat models are also text that
// is read and indexed as widely as the body, so the secret check reads them
// too (SPEC 5.2).

// IsDefinedField reports whether key is one of the header fields vat models.
func IsDefinedField(key string) bool {
	return frontmatter.Keys(Metadata{})[key]
}

// sortedExtensionKeys returns keys in a stable order, so findings and output
// read the same way twice.
func sortedExtensionKeys(extensions map[string]any) []string {
	keys := make([]string, 0, len(extensions))
	for key := range extensions {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// extensionText is the text of an extension value as the secret check reads
// it: a scalar as written, a list or mapping flattened.
func extensionText(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	case []any:
		text := ""
		for _, item := range typed {
			text += extensionText(item) + "\n"
		}
		return text
	case map[string]any:
		text := ""
		for _, key := range sortedExtensionKeys(typed) {
			text += key + ": " + extensionText(typed[key]) + "\n"
		}
		return text
	default:
		return fmt.Sprint(typed)
	}
}

// readHeader decodes a record's front matter into Metadata and its extension
// keys, parsing the header once.
//
// A key that is not a plain string — `? [a, b]`, a mapping used as a key —
// cannot name a field, and handing it to the typed decoder failed the whole
// decode, so one odd key made the record vanish from every rule and from the
// graph. It is reported as a malformed extension and kept away from the typed
// decoder; the defined fields, which are all plain strings, are untouched.
func readHeader(doc frontmatter.Document) (Metadata, map[string]any, []string, error) {
	var metadata Metadata
	if !doc.Present {
		return metadata, nil, nil, nil
	}
	var document yaml.Node
	if err := yaml.Unmarshal([]byte(doc.Raw), &document); err != nil {
		return metadata, nil, nil, headerProblem(err)
	}
	if len(document.Content) == 0 {
		return metadata, nil, nil, nil
	}
	root := document.Content[0]
	if root.Kind != yaml.MappingNode {
		if err := root.Decode(&metadata); err != nil {
			return metadata, nil, nil, headerProblem(err)
		}
		return metadata, nil, nil, nil
	}
	typed := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Line: root.Line, Column: root.Column}
	for i := 0; i+1 < len(root.Content); i += 2 {
		key := root.Content[i]
		if plainStringKey(key) || key.ShortTag() == mergeTag {
			typed.Content = append(typed.Content, key, root.Content[i+1])
		}
	}
	if err := typed.Decode(&metadata); err != nil {
		return metadata, nil, nil, headerProblem(err)
	}
	extensions, problems := readExtensions(root)
	return metadata, extensions, problems, nil
}

// mergeTag is the tag YAML gives the merge key `<<`.
const mergeTag = "!!merge"

// plainStringKey reports whether a mapping key is a string scalar, the only
// kind a field or an extension may be named by.
func plainStringKey(key *yaml.Node) bool {
	return key.Kind == yaml.ScalarNode && key.ShortTag() == yamlStringTag
}

// readExtensions returns the header keys Metadata does not model, converted to
// values JSON can hold, and one problem per key that could not be converted.
//
// Values are taken from the YAML node rather than from a generic decode, so a
// timestamp is published as the text the author wrote instead of as whatever
// a time library prints for it, and the same header always yields the same
// bytes.
func readExtensions(mapping *yaml.Node) (map[string]any, []string) {
	defined := frontmatter.Keys(Metadata{})
	extensions := map[string]any{}
	var problems []string
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		key, value := mapping.Content[i], mapping.Content[i+1]
		if !plainStringKey(key) {
			problems = append(problems, fmt.Sprintf(
				"line %d: a header key that is not a plain string cannot be carried into graph.json", headerLine(key)))
			continue
		}
		if defined[key.Value] {
			continue
		}
		converted, err := jsonValue(value, map[*yaml.Node]bool{})
		if err != nil {
			problems = append(problems, fmt.Sprintf("line %d: %s cannot be carried into graph.json: %v",
				headerLine(key), keyLabel(key.Value), err))
			continue
		}
		extensions[key.Value] = converted
	}
	if len(extensions) == 0 {
		return nil, problems
	}
	return extensions, problems
}

// headerLine is the line of the record file a header node sits on: the
// header starts on the line after the opening delimiter.
func headerLine(node *yaml.Node) int { return node.Line + 1 }

// keyLabel names an extension key in a diagnostic. A key is author text like
// any value, and a finding that quotes a credential has published it a second
// time, so a key is named only once the credential scan finds nothing in it.
func keyLabel(key string) string {
	if DisplayableKey(key) {
		return "extension key " + key
	}
	return "a header key"
}

// jsonValue converts a YAML node into a string-keyed mapping, a list, or a
// plain value, refusing anything JSON cannot represent.
//
// active holds the anchors being expanded on the current path. An alias that
// names one of them refers to itself, and following it would recurse until the
// stack ran out: one record with `extra: &loop [*loop]` took down every
// command that loads the brain.
func jsonValue(node *yaml.Node, active map[*yaml.Node]bool) (any, error) {
	switch node.Kind {
	case yaml.AliasNode:
		if active[node.Alias] {
			return nil, fmt.Errorf("it refers to itself through an alias")
		}
		active[node.Alias] = true
		defer delete(active, node.Alias)
		return jsonValue(node.Alias, active)
	case yaml.SequenceNode:
		list := make([]any, 0, len(node.Content))
		for _, item := range node.Content {
			converted, err := jsonValue(item, active)
			if err != nil {
				return nil, err
			}
			list = append(list, converted)
		}
		return list, nil
	case yaml.MappingNode:
		mapping := make(map[string]any, len(node.Content)/2)
		for i := 0; i+1 < len(node.Content); i += 2 {
			key := node.Content[i]
			if !plainStringKey(key) {
				return nil, fmt.Errorf("a nested key is not a plain string")
			}
			converted, err := jsonValue(node.Content[i+1], active)
			if err != nil {
				return nil, err
			}
			mapping[key.Value] = converted
		}
		return mapping, nil
	case yaml.ScalarNode:
		return jsonScalar(node)
	}
	return nil, fmt.Errorf("unsupported YAML node")
}

// jsonScalar converts one scalar. Its errors are fixed sentences: the value,
// and a custom tag, are author text and may be the credential itself.
func jsonScalar(node *yaml.Node) (any, error) {
	switch node.ShortTag() {
	case yamlStringTag, "!!timestamp", "!!binary":
		return node.Value, nil
	case "!!null":
		return nil, nil
	case "!!bool":
		var value bool
		if err := node.Decode(&value); err != nil {
			return nil, errUnconvertible
		}
		return value, nil
	case "!!int":
		var value int64
		if err := node.Decode(&value); err != nil {
			return nil, fmt.Errorf("the value is not an integer that fits in 64 bits")
		}
		return value, nil
	case "!!float":
		var value float64
		if err := node.Decode(&value); err != nil {
			return nil, errUnconvertible
		}
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return nil, fmt.Errorf("the value is not a number JSON can hold")
		}
		return value, nil
	}
	return nil, fmt.Errorf("a value with a custom tag has no JSON form")
}

var errUnconvertible = errors.New("the value cannot be read as the type its tag names")
