package node

import (
	"bytes"
	"regexp"
	"strings"

	"github.com/germanamz/tusk/internal/manifest"
)

// wikilinkPattern matches `[[target]]` and the Obsidian aliased form
// `[[target|display]]`. The captured group is the whole inner text (target plus
// any `|display` suffix); splitWikilinkAlias separates the two. `[` and `]` still
// terminate the class so a link can never span brackets or run past its `]]`.
var wikilinkPattern = regexp.MustCompile(`\[\[([^\[\]]+)\]\]`)

// splitWikilinkAlias splits a wikilink's inner text (the run between `[[` and
// `]]`) into its link target and Obsidian-style `|alias` display suffix. The
// target — everything before the first `|` — is trimmed; the alias is returned
// verbatim with its leading `|` (empty when the link carries no alias), so a
// rewrite can substitute a new target and keep the display text byte-for-byte.
// `[[id|Label]]` links to `id`; the label is presentation only and is never
// resolved. A sub-unit fragment (`[[id#S1|Label]]`) stays with the target.
func splitWikilinkAlias(inner string) (target, alias string) {
	if pipe := strings.IndexByte(inner, '|'); pipe >= 0 {
		return strings.TrimSpace(inner[:pipe]), inner[pipe:]
	}

	return strings.TrimSpace(inner), ""
}

// ExtractWikilinks returns the unique list of wikilink targets from body,
// in first-seen order, ignoring fenced code blocks (```…```).
func ExtractWikilinks(body []byte) []string {
	stripped := stripFencedCodeBlocks(body)
	matches := wikilinkPattern.FindAllSubmatch(stripped, -1)

	seen := map[string]struct{}{}
	var ordered []string

	for _, match := range matches {
		target, _ := splitWikilinkAlias(string(match[1]))

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
	// Extract once: the body's wikilinks do not change per edge type, but the
	// extraction strips fenced code and runs a regex over the whole body. The
	// HTML twin (ResolveHTMLLinks) already hoists its equivalent.
	targets := ExtractWikilinks(parsed.Body)

	for name, edgeType := range edgeTypes {
		if !edgeType.Wikilinks {
			continue
		}

		for _, target := range targets {
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
