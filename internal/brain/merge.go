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

// maxMergeWork bounds the mappings one header may resolve and the pairs it may
// copy out of merged mappings. A header a person writes uses a handful; the
// bound exists for one built to grow, where each level merges the last twice.
const maxMergeWork = 10000

// mergeResolver resolves the merge keys of one header.
//
// A mapping is resolved once and reused. Following every reference afresh
// expanded `aN: {<<: [*aN-1, *aN-1]}` once per path — 2^N times for a result
// of a single key — and thirty such lines in one record stalled every command
// that loads the brain. Keys that are not plain strings are not deduplicated,
// so the memo alone cannot keep their copies from doubling; the work budget
// does, and a header that exceeds it is refused rather than expanded.
type mergeResolver struct {
	resolved map[*yaml.Node][]headerPair
	work     int
}

func newMergeResolver() *mergeResolver {
	return &mergeResolver{resolved: map[*yaml.Node][]headerPair{}}
}

// spend charges n units of work and refuses the header once the budget is
// gone. at is the node the refusal names.
func (r *mergeResolver) spend(n int, at *yaml.Node) error {
	r.work += n
	if r.work > maxMergeWork {
		return mergeFault("expands its merge keys and aliases further than a record header can", at)
	}
	return nil
}

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
//
// The returned slice may be shared with later calls and must not be modified.
func (r *mergeResolver) mappingPairs(mapping *yaml.Node, active map[*yaml.Node]bool) ([]headerPair, []string, error) {
	if pairs, ok := r.resolved[mapping]; ok {
		return pairs, nil, nil
	}
	if err := r.spend(1, mapping); err != nil {
		return nil, nil, err
	}
	var pairs []headerPair
	var merge *yaml.Node
	present := map[string]bool{}
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		key, value := mapping.Content[i], mapping.Content[i+1]
		if key.Kind == yaml.ScalarNode && key.ShortTag() == mergeTag {
			if merge != nil {
				return nil, nil, mergeFault("gives a key more than once, or a value that does not fit its field", key)
			}
			merge = value
			continue
		}
		if plainStringKey(key) {
			present[key.Value] = true
		}
		pairs = append(pairs, headerPair{key, value})
	}
	problems, err := r.mergeInto(&pairs, present, merge, active)
	if err != nil {
		return nil, nil, err
	}
	// A mapping with a problem is not reused: whether a merge includes itself
	// depends on the path that reached it, and the record is malformed anyway.
	if len(problems) == 0 {
		r.resolved[mapping] = pairs
	}
	return pairs, problems, nil
}

// mergeInto appends the pairs merge names to pairs, skipping a string key
// already present, and returns one problem per source it cannot resolve.
func (r *mergeResolver) mergeInto(pairs *[]headerPair, present map[string]bool, merge *yaml.Node, active map[*yaml.Node]bool) ([]string, error) {
	if merge == nil {
		return nil, nil
	}
	sources, ok := mergeSources(merge)
	if !ok {
		return []string{fmt.Sprintf("line %d: a merge key that names no mapping cannot be carried into graph.json", headerLine(merge))}, nil
	}
	var problems []string
	for _, source := range sources {
		if active[source] {
			problems = append(problems, fmt.Sprintf("line %d: a merge that includes itself cannot be carried into graph.json", headerLine(merge)))
			continue
		}
		active[source] = true
		merged, nested, err := r.mappingPairs(source, active)
		delete(active, source)
		if err != nil {
			return nil, err
		}
		if err := r.spend(len(merged), merge); err != nil {
			return nil, err
		}
		problems = append(problems, nested...)
		for _, pair := range merged {
			if plainStringKey(pair.key) {
				if present[pair.key.Value] {
					continue
				}
				present[pair.key.Value] = true
			}
			*pairs = append(*pairs, pair)
		}
	}
	return problems, nil
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
