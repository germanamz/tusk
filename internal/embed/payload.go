package embed

import (
	"fmt"
	"sort"
	"strings"

	"github.com/germanamz/tusk/internal/node"
)

// BuildPayload renders a node into the canonical embed input.
//
// Format (per spec §10.4):
//
//	[type] {type}
//	[title] {title}
//	{remaining frontmatter properties as key=value, sorted by key}
//	---
//	{body}
//
// Order is stable so the resulting content_hash is reproducible.
func BuildPayload(parsedNode *node.Node) []byte {
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
	builder.Write(parsedNode.Body)

	return []byte(builder.String())
}
