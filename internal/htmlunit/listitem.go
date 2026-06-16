package htmlunit

import (
	"strings"

	"golang.org/x/net/html"
)

// isListContainer reports whether node is a <ul> or <ol> element —
// the boundary at which an <li>'s own content ends and nested list
// items (walked separately as peer units) begin.
func isListContainer(node *html.Node) bool {
	return node.Type == html.ElementNode && (node.Data == "ul" || node.Data == "ol")
}

// listItemCheckbox scans item's own content in document order —
// skipping nested <ul>/<ol> subtrees — for the first <input> whose
// type attribute is "checkbox" (attribute keys are lowercased by
// x/net/html; the value is matched case-insensitively). The checked
// state is attribute presence: <input checked>, checked="", and
// checked="checked" all count. Mirrors subunit.extractCheckbox
// (parse.go:418): a nested item's checkbox never leaks to its parent.
func listItemCheckbox(item *html.Node) (bool, bool) {
	var found, checked bool

	var visit func(node *html.Node) bool
	visit = func(node *html.Node) bool {
		if isListContainer(node) {
			return true
		}
		if node.Type == html.ElementNode && node.Data == "input" {
			if strings.EqualFold(attrValue(node, "type"), "checkbox") {
				found = true
				checked = hasAttr(node, "checked")
				return false
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			if !visit(child) {
				return false
			}
		}
		return true
	}

	for child := item.FirstChild; child != nil; child = child.NextSibling {
		if !visit(child) {
			break
		}
	}

	return checked, found
}

// attrValue returns the value of the named attribute, or "" when
// absent. key must be lowercase; x/net/html normalizes all attribute
// keys to lowercase during parsing.
func attrValue(node *html.Node, key string) string {
	for _, attr := range node.Attr {
		if attr.Key == key {
			return attr.Val
		}
	}
	return ""
}

// hasAttr reports whether the named attribute is present, regardless
// of value.
func hasAttr(node *html.Node, key string) bool {
	for _, attr := range node.Attr {
		if attr.Key == key {
			return true
		}
	}
	return false
}

// listItemOwnText returns the collapsed inline text of the item
// excluding nested <ul>/<ol> subtrees — the HTML twin of
// subunit.extractListItemText (parse.go:437). Nested list items are
// walked separately as peer units; <input> elements contribute no
// text, so the checkbox marker is naturally absent.
func listItemOwnText(item *html.Node) string {
	var builder strings.Builder
	for child := item.FirstChild; child != nil; child = child.NextSibling {
		collectTextPruning(child, &builder, isListContainer)
	}
	return strings.Join(strings.Fields(builder.String()), " ")
}

// nestedLists returns the topmost <ul>/<ol> descendants of item in
// document order — anywhere in the subtree, wrappers like <div>
// included. Lists nested deeper are reached when the walk recurses
// into their own parent <li>, so descent stops at each match.
func nestedLists(item *html.Node) []*html.Node {
	var lists []*html.Node

	var visit func(node *html.Node)
	visit = func(node *html.Node) {
		if isListContainer(node) {
			lists = append(lists, node)
			return
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			visit(child)
		}
	}

	for child := item.FirstChild; child != nil; child = child.NextSibling {
		visit(child)
	}

	return lists
}
