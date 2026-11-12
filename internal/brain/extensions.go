package brain

import (
	"fmt"
	"sort"

	"github.com/takealook97/vat/internal/frontmatter"
)

// Header keys a record carries beyond the ones vat models.
//
// The specification says a reader preserves a key it does not know (SPEC
// §5.2), and a writer such as a publishing job uses exactly those keys to say
// where a record came from. vat gives them no meaning; it reads them so it can
// show them, and so a credential pasted into one is found like one pasted into
// the body.

// IsDefinedField reports whether key is one of the header fields vat models.
func IsDefinedField(key string) bool {
	return frontmatter.Keys(Metadata{})[key]
}

// extensionFields returns the header keys Metadata does not model, or nil when
// there are none.
func extensionFields(doc frontmatter.Document) (map[string]any, error) {
	fields, err := doc.Fields()
	if err != nil {
		return nil, err
	}
	defined := frontmatter.Keys(Metadata{})
	var extensions map[string]any
	for key, value := range fields {
		if defined[key] {
			continue
		}
		if extensions == nil {
			extensions = map[string]any{}
		}
		extensions[key] = value
	}
	return extensions, nil
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
