package embed

import (
	"fmt"
	"sort"
	"strings"

	"github.com/germanamz/tusk/internal/node"
)

// BuildHeader renders a node's frontmatter (type, title, sorted remaining
// properties) followed by a `---\n` separator. The header is prepended to
// every chunk's body before embedding, so each chunk carries doc-level
// context.
func BuildHeader(parsedNode *node.Node) []byte {
	var builder strings.Builder

	fmt.Fprintf(&builder, "[type] %s\n", parsedNode.Type)

	if parsedNode.Title != "" {
		fmt.Fprintf(&builder, "[title] %s\n", parsedNode.Title)
	}

	keys := make([]string, 0, len(parsedNode.Properties))

	for key := range parsedNode.Properties {
		if key == "type" || key == "title" {
			continue
		}

		keys = append(keys, key)
	}

	sort.Strings(keys)

	for _, key := range keys {
		fmt.Fprintf(&builder, "%s=%v\n", key, parsedNode.Properties[key])
	}

	builder.WriteString("---\n")

	return []byte(builder.String())
}

// BuildBody returns the node's raw body bytes. Chunkers split this slice.
func BuildBody(parsedNode *node.Node) []byte {
	return parsedNode.Body
}

// BuildPayload renders a node into the canonical unchunked embed input by
// concatenating BuildHeader and BuildBody. The drain path builds header and
// body separately (it chunks the body), so this convenience composition is
// exercised only by tests; it documents the canonical header+body wire shape.
func BuildPayload(parsedNode *node.Node) []byte {
	header := BuildHeader(parsedNode)
	body := BuildBody(parsedNode)

	out := make([]byte, 0, len(header)+len(body))
	out = append(out, header...)
	out = append(out, body...)

	return out
}
