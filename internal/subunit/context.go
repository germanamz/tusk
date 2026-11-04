package subunit

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/wikilink"
)

// LeafContext is what a sub-unit leaf embeds in front of its passage (#862):
// a label saying where the passage sits, and a line per named file property. A
// passage that answers a question about a page often never names the page, so
// a bare passage's vector misses a query that does.
//
// The embed drain renders it into the text it embeds. The sub-unit sync folds
// it into the leaf's content hash, so changing anything it shows (a title or
// heading rename, a property edit) re-embeds the leaf. Both build it with
// NewLeafContext from the same stored values: the file row's title, path, type
// and properties, and the titles of the leaf's enclosing section rows.
type LeafContext struct {
	// Label is the file's title, or its file name when it has none. Under
	// "path" the enclosing headings follow it, joined by " > ". Empty under
	// "none".
	Label string
	// Lines holds one "name: value" line per sub-unit-properties name the file
	// has a value for, in the setting's order.
	Lines []string
}

// ContextFile is what a leaf's context reads from its file row.
type ContextFile struct {
	Title          string
	Path           string
	Type           string
	PropertiesJSON string
}

// labelSeparator joins the parts of a "path" label.
const labelSeparator = " > "

// NewLeafContext builds a leaf's context under mode (a manifest.SubUnitContext*
// value; "" behaves as none) and the property names, from its file and the
// titles of its enclosing sections, outermost first. headings is read only
// under "path", where a heading that repeats the label part before it (an H1
// that restates the title) is left out.
func NewLeafContext(mode string, properties []string, file ContextFile, headings []string) LeafContext {
	leaf := LeafContext{Lines: propertyLines(properties, file)}

	if mode != manifest.SubUnitContextTitle && mode != manifest.SubUnitContextPath {
		return leaf
	}

	parts := []string{fileLabel(file)}

	if mode == manifest.SubUnitContextPath {
		for _, heading := range headings {
			heading = strings.TrimSpace(heading)

			if heading == "" || strings.EqualFold(heading, parts[len(parts)-1]) {
				continue
			}

			parts = append(parts, heading)
		}
	}

	leaf.Label = strings.Join(parts, labelSeparator)

	return leaf
}

// Empty reports whether the context puts nothing in front of the passage.
func (leaf LeafContext) Empty() bool {
	return leaf.Label == "" && len(leaf.Lines) == 0
}

// Header returns the lines the leaf embeds before its passage, each ending in
// a newline: the label first, then the property lines. labelInPrefix leaves
// the label out, for a document-prefix whose {title} already carries it. Nil
// when there is nothing to put in front.
func (leaf LeafContext) Header(labelInPrefix bool) []byte {
	var builder strings.Builder

	if leaf.Label != "" && !labelInPrefix {
		builder.WriteString(leaf.Label)
		builder.WriteByte('\n')
	}

	for _, line := range leaf.Lines {
		builder.WriteString(line)
		builder.WriteByte('\n')
	}

	if builder.Len() == 0 {
		return nil
	}

	return []byte(builder.String())
}

// ContentHash returns the content hash of a leaf whose passage is payload under
// this context: sha256 of the header (label included) followed by payload. An
// empty context hashes payload alone, the hash a leaf had before contexts
// existed (ContentHashFor). It is a fingerprint of what the leaf embeds, not of
// the exact bytes: the document prefix is left out, and so is where {title}
// moves the label.
func (leaf LeafContext) ContentHash(payload string) string {
	hasher := sha256.New()
	_, _ = hasher.Write(leaf.Header(false))
	_, _ = hasher.Write([]byte(payload))

	return hex.EncodeToString(hasher.Sum(nil))
}

// fileLabel is the name a file goes by in a leaf's label: its title, or its
// file name when untitled, the same fallback the title match uses.
func fileLabel(file ContextFile) string {
	if title := strings.TrimSpace(file.Title); title != "" {
		return title
	}

	return wikilink.FileName(file.Path)
}

// propertyLines renders "name: value" for each named property the file has a
// non-empty value for. "type" is the file row's type, which a file adopted
// under the workspace default type has without a frontmatter key. Properties
// that don't decode yield no lines.
func propertyLines(names []string, file ContextFile) []string {
	if len(names) == 0 {
		return nil
	}

	values := map[string]any{}

	if file.PropertiesJSON != "" {
		_ = json.Unmarshal([]byte(file.PropertiesJSON), &values)
	}

	var lines []string

	for _, name := range names {
		value := values[name]

		if name == "type" {
			value = file.Type
		}

		if rendered := renderValue(value); rendered != "" {
			lines = append(lines, name+": "+rendered)
		}
	}

	return lines
}

// renderValue renders a decoded property value on one line: strings with their
// whitespace collapsed, numbers and booleans as written, and lists as their
// items joined by ", ". Objects, nulls and empty values render as "".
func renderValue(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.Join(strings.Fields(typed), " ")
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(typed)
	case []any:
		items := make([]string, 0, len(typed))

		for _, item := range typed {
			if _, nested := item.([]any); nested {
				continue
			}

			if rendered := renderValue(item); rendered != "" {
				items = append(items, rendered)
			}
		}

		return strings.Join(items, ", ")
	default:
		return ""
	}
}
