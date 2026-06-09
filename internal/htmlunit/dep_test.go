package htmlunit

import (
	"strings"
	"testing"

	"golang.org/x/net/html"
)

// TestXNetHTMLLinked is a smoke test that the golang.org/x/net/html
// dependency is wired into the module and parses a trivial document.
func TestXNetHTMLLinked(test *testing.T) {
	doc, err := html.Parse(strings.NewReader("<p>hi</p>"))
	if err != nil {
		test.Fatalf("html.Parse: %v", err)
	}
	if doc == nil || doc.Type != html.DocumentNode {
		test.Fatalf("want a document node, got %+v", doc)
	}
}
