package brain

import (
	"fmt"
	"math"
	"reflect"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Extension keys, carried into graph.json.
//
// SPEC 5.6.1 lets a record hold keys this package does not define, and until
// now they survived every rewrite but stopped at the loader: a reader of
// graph.json had to re-parse every record to see them, which is the parsing
// the graph exists to save. They are carried as display data only. Nothing
// here reads one to decide a status, a citability, or an order.

// metadataKeys is every header key Metadata models; anything else is an
// extension. Derived from the type so a field added to Metadata cannot also
// appear as an extension of itself.
var metadataKeys = func() map[string]bool {
	keys := map[string]bool{}
	typ := reflect.TypeOf(Metadata{})
	for i := range typ.NumField() {
		if name := strings.Split(typ.Field(i).Tag.Get("yaml"), ",")[0]; name != "" && name != "-" {
			keys[name] = true
		}
	}
	return keys
}()

// readExtensions returns the header keys Metadata does not model, converted to
// values JSON can hold, and one problem per key that could not be converted.
//
// Values are taken from the YAML node rather than from a generic decode, so a
// timestamp is published as the text the author wrote instead of as whatever
// a time library prints for it, and the same header always yields the same
// bytes.
func readExtensions(header string) (map[string]any, []string) {
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
		if key.Kind != yaml.ScalarNode || key.ShortTag() != "!!str" {
			problems = append(problems, fmt.Sprintf("header key %q is not a string, so graph.json cannot carry it", key.Value))
			continue
		}
		if metadataKeys[key.Value] {
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
			if key.Kind != yaml.ScalarNode || key.ShortTag() != "!!str" {
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
	case "!!str", "!!timestamp", "!!binary":
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
