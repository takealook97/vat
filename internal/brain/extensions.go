package brain

import (
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

// readExtensions returns the header keys Metadata does not model, converted to
// values JSON can hold, and one problem per key that could not be converted.
//
// Values are taken from the YAML node rather than from a generic decode, so a
// timestamp is published as the text the author wrote instead of as whatever
// a time library prints for it, and the same header always yields the same
// bytes.
func readExtensions(header string) (map[string]any, []string) {
	defined := frontmatter.Keys(Metadata{})
	var document yaml.Node
	if err := yaml.Unmarshal([]byte(header), &document); err != nil || len(document.Content) == 0 {
		return nil, nil
	}
	mapping := document.Content[0]
	if mapping.Kind != yaml.MappingNode {
		return nil, nil
	}
	extensions := map[string]any{}
	var problems []string
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		key, value := mapping.Content[i], mapping.Content[i+1]
		if key.Kind != yaml.ScalarNode || key.ShortTag() != yamlStringTag {
			problems = append(problems, fmt.Sprintf("header key %q is not a string, so graph.json cannot carry it", key.Value))
			continue
		}
		if defined[key.Value] {
			continue
		}
		converted, err := jsonValue(value)
		if err != nil {
			problems = append(problems, fmt.Sprintf("extension key %s cannot be carried into graph.json: %v", key.Value, err))
			continue
		}
		extensions[key.Value] = converted
	}
	sort.Strings(problems)
	if len(extensions) == 0 {
		return nil, problems
	}
	return extensions, problems
}

// jsonValue converts a YAML node into a string-keyed mapping, a list, or a
// plain value, refusing anything JSON cannot represent.
func jsonValue(node *yaml.Node) (any, error) {
	switch node.Kind {
	case yaml.AliasNode:
		return jsonValue(node.Alias)
	case yaml.SequenceNode:
		list := make([]any, 0, len(node.Content))
		for _, item := range node.Content {
			converted, err := jsonValue(item)
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
			if key.Kind != yaml.ScalarNode || key.ShortTag() != yamlStringTag {
				return nil, fmt.Errorf("mapping key %q is not a string", key.Value)
			}
			converted, err := jsonValue(node.Content[i+1])
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

func jsonScalar(node *yaml.Node) (any, error) {
	switch node.ShortTag() {
	case yamlStringTag, "!!timestamp", "!!binary":
		return node.Value, nil
	case "!!null":
		return nil, nil
	case "!!bool":
		var value bool
		err := node.Decode(&value)
		return value, err
	case "!!int":
		var value int64
		if err := node.Decode(&value); err != nil {
			return nil, fmt.Errorf("integer %s does not fit in 64 bits", node.Value)
		}
		return value, nil
	case "!!float":
		var value float64
		if err := node.Decode(&value); err != nil {
			return nil, err
		}
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return nil, fmt.Errorf("%s is not a number JSON can hold", node.Value)
		}
		return value, nil
	}
	return nil, fmt.Errorf("value tagged %s has no JSON form", node.Tag)
}
