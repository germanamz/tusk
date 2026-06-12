package htmlunit

import (
	"strings"

	"golang.org/x/net/html"
)

// titleMaxLen caps the single-line title excerpt, matching
// subunit.titleMaxLen so HTML and markdown titles render identically.
const titleMaxLen = 120

// elementText returns the collapsed inline text of an element subtree:
// all descendant text nodes concatenated in document order with
// whitespace runs collapsed to single spaces and the result trimmed.
// script/style subtrees contribute nothing. Used for heading text and
// single-block leaf text.
func elementText(node *html.Node) string {
	var builder strings.Builder
	collectText(node, &builder)
	return strings.Join(strings.Fields(builder.String()), " ")
}

// collectText appends every descendant text node's data to builder,
// skipping script/style subtrees.
func collectText(node *html.Node, builder *strings.Builder) {
	collectTextPruning(node, builder, nil)
}

// collectTextPruning is collectText with an optional prune predicate:
// subtrees for which prune returns true contribute no text. Used by
// listItemOwnText to exclude nested <ul>/<ol> from a list item's own
// text.
func collectTextPruning(node *html.Node, builder *strings.Builder, prune func(*html.Node) bool) {
	if prune != nil && prune(node) {
		return
	}
	if node.Type == html.ElementNode {
		switch node.Data {
		case "script", "style":
			return
		}
	}
	if node.Type == html.TextNode {
		builder.WriteString(node.Data)
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		collectTextPruning(child, builder, prune)
	}
}

// makeTitle returns a single-line excerpt of body for the nodes.title
// column. Mirrors subunit.makeTitle (parse.go:462).
func makeTitle(body string) string {
	trimmed := strings.TrimSpace(body)
	if trimmed == "" {
		return ""
	}
	if idx := strings.IndexByte(trimmed, '\n'); idx >= 0 {
		trimmed = trimmed[:idx]
	}
	trimmed = strings.Join(strings.Fields(trimmed), " ")
	runes := []rune(trimmed)
	if len(runes) <= titleMaxLen {
		return trimmed
	}
	return string(runes[:titleMaxLen-1]) + "…"
}
