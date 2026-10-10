// Package wikilink finds [[wikilinks]] in markdown text. It is a leaf package so
// both internal/node and internal/subunit can share one definition of where a
// link target starts and ends.
package wikilink

import (
	"bytes"
	"regexp"
	"strings"
)

// pattern matches `[[target]]` and the Obsidian aliased form
// `[[target|display]]`. The captured group is the whole inner text (target plus
// any `|display` suffix); SplitAlias separates the two. `[` and `]` still
// terminate the class so a link can never span brackets or run past its `]]`.
var pattern = regexp.MustCompile(`\[\[([^\[\]]+)\]\]`)

// SplitAlias splits a wikilink's inner text (the run between `[[` and `]]`)
// into its link target and Obsidian-style `|alias` display suffix. The target —
// everything before the first `|` — is trimmed; the alias is returned verbatim
// with its leading `|` (empty when the link carries no alias), so a rewrite can
// substitute a new target and keep the display text byte-for-byte.
// `[[id|Label]]` links to `id`; the label is presentation only and is never
// resolved. A sub-unit fragment (`[[id#S1|Label]]`) stays with the target.
func SplitAlias(inner string) (target, alias string) {
	if pipe := strings.IndexByte(inner, '|'); pipe >= 0 {
		return strings.TrimSpace(inner[:pipe]), inner[pipe:]
	}

	return strings.TrimSpace(inner), ""
}

// Extract returns the unique list of wikilink targets from body, in first-seen
// order, ignoring fenced code blocks (```…```).
func Extract(body []byte) []string {
	stripped := stripFencedCodeBlocks(body)
	matches := pattern.FindAllSubmatch(stripped, -1)

	seen := map[string]struct{}{}
	var ordered []string

	for _, match := range matches {
		target, _ := SplitAlias(string(match[1]))

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

// stripFencedCodeBlocks returns body with content inside triple-backtick fences
// replaced by blank space so pattern doesn't match into code samples.
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
