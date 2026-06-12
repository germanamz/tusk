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
