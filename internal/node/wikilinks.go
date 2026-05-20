package node

import (
	"bytes"
	"regexp"
	"strings"

	"github.com/germanamz/tusk/internal/manifest"
)

// wikilinkPattern matches `[[target]]` where target is one or more characters
// that do not include `[`, `]`, or pipe `|`.
var wikilinkPattern = regexp.MustCompile(`\[\[([^\[\]|]+)\]\]`)

// ExtractWikilinks returns the unique list of wikilink targets from body,
// in first-seen order, ignoring fenced code blocks (```…```).
func ExtractWikilinks(body []byte) []string {
	stripped := stripFencedCodeBlocks(body)
	matches := wikilinkPattern.FindAllSubmatch(stripped, -1)

	seen := map[string]struct{}{}
	var ordered []string

	for _, match := range matches {
		target := strings.TrimSpace(string(match[1]))

		if target == "" {
			continue
		}

		if _, already := seen[target]; already {
			continue
		}

		seen[target] = struct{}{}
		ordered = append(ordered, target)
	}

	return ordered
}

// MaterializeWikilinks appends body [[wikilink]] targets to every edge type
// flagged with `wikilinks = true`. Idempotent per edge via appendUnique, so
// repeated calls and duplicate links do not create duplicate edges.
func MaterializeWikilinks(parsed *Node, edgeTypes manifest.EdgeTypes) {
	for name, edgeType := range edgeTypes {
		if !edgeType.Wikilinks {
			continue
		}

		for _, target := range ExtractWikilinks(parsed.Body) {
			parsed.Edges[name] = appendUnique(parsed.Edges[name], target)
		}
	}
}

// stripFencedCodeBlocks returns body with content inside triple-backtick fences
// replaced by blank space so wikilinkPattern doesn't match into code samples.
func stripFencedCodeBlocks(body []byte) []byte {
	const fence = "```"

	stripped := make([]byte, 0, len(body))
	rest := body

	for {
		openIndex := bytes.Index(rest, []byte(fence))

		if openIndex < 0 {
			stripped = append(stripped, rest...)
			return stripped
		}

		stripped = append(stripped, rest[:openIndex]...)

		afterOpen := rest[openIndex+len(fence):]
		closeIndex := bytes.Index(afterOpen, []byte(fence))

		if closeIndex < 0 {
			// Unterminated fence — drop the rest.
			return stripped
		}

		rest = afterOpen[closeIndex+len(fence):]
	}
}
