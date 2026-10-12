// Package wikilink finds [[wikilinks]] in markdown text. It is a leaf package so
// both internal/node and internal/subunit can share one definition of where a
// link target starts and ends.
package wikilink

import (
	"bytes"
	"regexp"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/text"
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

// Extract returns the unique list of wikilink targets from a markdown body, in
// first-seen order. Code never links: a `[[target]]` inside an inline code span
// or a fenced or indented code block is an example of the syntax, not a
// reference, so MaskCode blanks it before the scan. That is how the web reader
// renders a page too, so an edge exists exactly where the reader shows a link.
func Extract(body []byte) []string {
	return ExtractText(MaskCode(body))
}

// ExtractText returns the unique list of wikilink targets in text, in
// first-seen order, scanning every byte. It is for text with no markdown code
// left in it: a slice of MaskCode's output, or the plain text of an HTML page.
// Targets are alias-stripped and trimmed, and keep any `#section` fragment.
func ExtractText(text []byte) []string {
	matches := pattern.FindAllSubmatch(text, -1)

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

// markdownParser matches the sub-unit and path-ref parsers' extensions, so a
// code span inside a table cell or a task-list item parses the same way here as
// it does there.
var markdownParser = goldmark.New(
	goldmark.WithExtensions(
		extension.Table,
		extension.TaskList,
	),
)

// MaskCode returns a copy of the markdown body with the content of every code
// span and code block (fenced, tilde-fenced or indented) replaced by spaces.
// Line breaks stay and the length is unchanged, so an offset found in the mask
// indexes the same byte of body: the move rewriter finds links in the mask and
// edits the original, and the sub-unit parser cuts each unit's links out of it
// by the unit's byte span.
//
// What counts as code is goldmark's CommonMark reading, the parser the sub-unit
// and path-ref passes use: a run of N backticks opens a span that closes at the
// next run of exactly N, and an unmatched run is literal text. Only content is
// blanked; the backtick and fence markers stay, and cannot form a link.
func MaskCode(body []byte) []byte {
	return MaskCodeIn(markdownParser.Parser().Parse(text.NewReader(body)), body)
}

// MaskCodeIn is MaskCode for a body the caller has already parsed: doc must be
// the tree goldmark built from source. The sub-unit parser uses it so its units
// and their links come from one parse.
func MaskCodeIn(doc ast.Node, source []byte) []byte {
	masked := bytes.Clone(source)

	_ = ast.Walk(doc, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}

		switch typed := node.(type) {
		case *ast.CodeSpan:
			for child := typed.FirstChild(); child != nil; child = child.NextSibling() {
				if segment, isText := child.(*ast.Text); isText {
					blank(masked, segment.Segment)
				}
			}

			return ast.WalkSkipChildren, nil
		case *ast.FencedCodeBlock, *ast.CodeBlock:
			lines := typed.Lines()

			for index := range lines.Len() {
				blank(masked, lines.At(index))
			}

			return ast.WalkSkipChildren, nil
		}

		return ast.WalkContinue, nil
	})

	return masked
}

// blank overwrites segment's bytes in masked with spaces, keeping line breaks.
func blank(masked []byte, segment text.Segment) {
	for offset := segment.Start; offset < segment.Stop; offset++ {
		if masked[offset] != '\n' && masked[offset] != '\r' {
			masked[offset] = ' '
		}
	}
}
