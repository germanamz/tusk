package node_test

import (
	"testing"

	"github.com/germanamz/tusk/internal/node"
)

func TestIsHTMLPath(test *testing.T) {
	cases := map[string]bool{
		"page.html":      true,
		"a/b/page.htm":   true,
		"notes/hello.md": false,
		"noext":          false,
	}

	for path, want := range cases {
		if got := node.IsHTMLPath(path); got != want {
			test.Errorf("IsHTMLPath(%q) = %v, want %v", path, got, want)
		}
	}
}

func TestParseContentFile_DispatchesByExtension(test *testing.T) {
	// Markdown routes to ParseFile: id strips the extension.
	md := []byte("---\ntype: note\ntitle: MD\n---\nbody\n")

	mdNode, mdErr := node.ParseContentFile("notes/hello.md", md)

	if mdErr != nil {
		test.Fatalf("markdown: %v", mdErr)
	}

	if mdNode.ID != "notes/hello" {
		test.Errorf("markdown ID = %q, want notes/hello", mdNode.ID)
	}

	if mdNode.Type != "note" {
		test.Errorf("markdown Type = %q, want note", mdNode.Type)
	}

	// HTML routes to ParseHTMLFile: id retains the extension and data-* signals
	// are collected under the reserved key.
	htmlSrc := []byte(`<html><head><meta name="tusk:type" content="page">` +
		`<title>HT</title></head><body><p data-k="v">hi</p></body></html>`)

	htNode, htErr := node.ParseContentFile("pages/sample.html", htmlSrc)

	if htErr != nil {
		test.Fatalf("html: %v", htErr)
	}

	if htNode.ID != "pages/sample.html" {
		test.Errorf("html ID = %q, want pages/sample.html (extension retained)", htNode.ID)
	}

	if htNode.Type != "page" {
		test.Errorf("html Type = %q, want page", htNode.Type)
	}

	if htNode.Title != "HT" {
		test.Errorf("html Title = %q, want HT", htNode.Title)
	}

	if _, ok := htNode.Properties[node.HTMLSignalsKey]; !ok {
		test.Errorf("html node missing %q signals, properties = %v", node.HTMLSignalsKey, htNode.Properties)
	}
}
