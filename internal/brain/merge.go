package brain

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// YAML merge keys in a record header.
//
// `<<: *shared` copies the keys of another mapping into this one. yaml.v3
// resolves it in every generic decode, so frontmatter.Document.Fields, lint,
// and every other reader saw the merged keys — while the extension loader,
// walking nodes, saw a key that was not a string, reported the record
// malformed, and left the merged keys out of graph.json. Resolution here
// follows yaml.v3: a key written in the mapping wins over a merged one, and
// among merged sources the earlier wins.

// headerPair is one key of a mapping and its value.
type headerPair struct{ key, value *yaml.Node }

// mappingPairs returns a mapping's pairs with merge keys resolved, and one
// problem per merge that cannot be resolved. active holds the mappings being
// resolved on the current path, so a mapping that merges itself is reported
// rather than followed forever.
//
// A key written in the mapping is kept as many times as it is written, so the
// typed decoder still refuses a duplicate. A second merge key is refused here,
// before merge keys are taken out of the mapping: the typed decoder never sees
// one, and accepting both let either supply the record's status where a
// generic decode rejects the header outright.
func mappingPairs(mapping *yaml.Node, active map[*yaml.Node]bool) ([]headerPair, []string, error) {
	var pairs []headerPair
	var merges []*yaml.Node
	present := map[string]bool{}
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		key, value := mapping.Content[i], mapping.Content[i+1]
		if key.Kind == yaml.ScalarNode && key.ShortTag() == mergeTag {
			if len(merges) > 0 {
				return nil, nil, mergeFault("gives a key more than once, or a value that does not fit its field", key)
			}
			merges = append(merges, value)
			continue
		}
		if plainStringKey(key) {
			present[key.Value] = true
		}
		pairs = append(pairs, headerPair{key, value})
	}
	var problems []string
	for _, merge := range merges {
		sources, ok := mergeSources(merge)
		if !ok {
			problems = append(problems, fmt.Sprintf("line %d: a merge key that names no mapping cannot be carried into graph.json", headerLine(merge)))
			continue
		}
		for _, source := range sources {
			if active[source] {
				problems = append(problems, fmt.Sprintf("line %d: a merge that includes itself cannot be carried into graph.json", headerLine(merge)))
				continue
			}
			active[source] = true
			merged, nested, err := mappingPairs(source, active)
			delete(active, source)
			if err != nil {
				return nil, nil, err
			}
			problems = append(problems, nested...)
			for _, pair := range merged {
				if !plainStringKey(pair.key) {
					pairs = append(pairs, pair)
					continue
				}
				if present[pair.key.Value] {
					continue
				}
				present[pair.key.Value] = true
				pairs = append(pairs, pair)
			}
		}
	}
	return pairs, problems, nil
}

// mergeFault refuses a header whose merge keys cannot be resolved the way a
// generic decode resolves them. Like every header problem it names a line and
// never the text, which is author input and may be the credential itself.
func mergeFault(what string, node *yaml.Node) error {
	return fmt.Errorf("parse front matter: the header %s (line %d)", what, headerLine(node))
}

// mergeSources returns the mappings a merge value names: one mapping, an alias
// of one, or a sequence of either.
func mergeSources(value *yaml.Node) ([]*yaml.Node, bool) {
	if mapping, ok := mergeMapping(value); ok {
		return []*yaml.Node{mapping}, true
	}
	if value.Kind != yaml.SequenceNode {
		return nil, false
	}
	sources := make([]*yaml.Node, 0, len(value.Content))
	for _, item := range value.Content {
		mapping, ok := mergeMapping(item)
		if !ok {
			return nil, false
		}
		sources = append(sources, mapping)
	}
	return sources, true
}

func mergeMapping(node *yaml.Node) (*yaml.Node, bool) {
	if node.Kind == yaml.AliasNode {
		node = node.Alias
	}
	if node == nil || node.Kind != yaml.MappingNode {
		return nil, false
	}
	return node, true
}
