package node

import (
	"strings"
	"testing"

	"github.com/germanamz/tusk/internal/manifest"
)

// Regression: renderMarkdown (yaml.v3) single-quotes an edge target that leads
// with a YAML indicator character (e.g. '@scope/old'); rename's value rewriter
// must recognize that single-quoted form or it silently leaves the edge pointing
// at the old id (a dangling edge) after a rename.
func TestRewriteFrontmatterEdgeValues_SingleQuotedIndicatorTarget(test *testing.T) {
	edgeTypes := manifest.EdgeTypes{"parent": manifest.EdgeType{}}

	rendered, renderErr := renderMarkdown(map[string]any{"type": "ticket", "parent": "@scope/old"}, []byte("body\n"))

	if renderErr != nil {
		test.Fatalf("renderMarkdown: %v", renderErr)
	}

	if !strings.Contains(string(rendered), `parent: '@scope/old'`) {
		test.Fatalf("expected single-quoted target in render:\n%s", rendered)
	}

	out := rewriteFrontmatterEdgeValues(rendered, "@scope/old", "@scope/new", edgeTypes)

	reparsed, parseErr := ParseFile("x/n.md", out)

	if parseErr != nil {
		test.Fatalf("reparse: %v\n%s", parseErr, out)
	}

	if got := reparsed.Properties["parent"]; got != "@scope/new" {
		test.Errorf("edge target not renamed: parent = %#v\noutput:\n%s", got, out)
	}
}
