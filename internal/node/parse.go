package node

import (
	"bytes"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// ErrMissingFrontmatter indicates the file does not begin with a YAML frontmatter block.
var ErrMissingFrontmatter = errors.New("node: missing frontmatter")

// ErrMissingType indicates the frontmatter has no `type:` field.
var ErrMissingType = errors.New("node: missing required `type` field")

var frontmatterDelimiter = []byte("---")

// utf8BOM is the three-byte UTF-8 byte-order mark. Some editors write it at the
// head of a file; left in place it defeats the frontmatter delimiter check
// (dropping the whole typed file from the index) and, when it lands at the head
// of the body, demotes the first heading to a paragraph (#682 item 2).
var utf8BOM = []byte("\xef\xbb\xbf")

// ParseFile parses content as a Tusk node file. relPath is the workspace-relative
// path (with extension); the canonical id is relPath stripped of its extension.
func ParseFile(relPath string, content []byte) (*Node, error) {
	frontmatterBytes, body, splitErr := splitFrontmatter(content)

	if splitErr != nil {
		return nil, splitErr
	}

	properties := map[string]any{}

	if decodeErr := yaml.Unmarshal(frontmatterBytes, &properties); decodeErr != nil {
		return nil, fmt.Errorf("node: decode frontmatter %s: %w", relPath, decodeErr)
	}

	typeValue, hasType := properties["type"].(string)

	if !hasType || typeValue == "" {
		return nil, ErrMissingType
	}

	title, _ := properties["title"].(string)

	properties = normalizeYAMLNumbers(properties)

	return &Node{
		ID:         strings.TrimSuffix(relPath, filepath.Ext(relPath)),
		Path:       relPath,
		Type:       typeValue,
		Title:      title,
		Properties: properties,
		Body:       body,
	}, nil
}

// splitFrontmatter returns the YAML body (without delimiters) and the remaining
// markdown body. The file must begin with `---\n`.
func splitFrontmatter(content []byte) ([]byte, []byte, error) {
	// Strip a leading UTF-8 BOM before the delimiter check so a BOM-prefixed
	// file is still recognized as a Tusk node instead of being silently dropped.
	trimmed := bytes.TrimLeft(bytes.TrimPrefix(content, utf8BOM), " \t\r\n")

	if !bytes.HasPrefix(trimmed, frontmatterDelimiter) {
		return nil, nil, ErrMissingFrontmatter
	}

	afterOpen := trimmed[len(frontmatterDelimiter):]

	// Skip the newline after the opening delimiter.
	afterOpen = bytes.TrimLeft(afterOpen, "\r\n")

	closingIndex := bytes.Index(afterOpen, append([]byte("\n"), frontmatterDelimiter...))

	if closingIndex < 0 {
		return nil, nil, ErrMissingFrontmatter
	}

	frontmatter := afterOpen[:closingIndex]
	rest := afterOpen[closingIndex+len("\n")+len(frontmatterDelimiter):]

	// Strip leading blank lines and a body-start BOM so a BOM written between
	// the frontmatter and the first heading does not demote that heading to a
	// paragraph in the sub-unit outline.
	body := bytes.TrimPrefix(bytes.TrimLeft(rest, "\r\n"), utf8BOM)

	return frontmatter, body, nil
}

// normalizeYAMLNumbers walks a parsed YAML map and converts number-shaped values
// from the YAML library's default int / float64 to plain int where the value
// fits losslessly. This keeps test assertions stable across YAML library
// behavior changes.
func normalizeYAMLNumbers(values map[string]any) map[string]any {
	for key, value := range values {
		switch typed := value.(type) {
		case int64:
			values[key] = int(typed)
		case float64:
			if typed == float64(int(typed)) {
				values[key] = int(typed)
			}
		}
	}

	return values
}
