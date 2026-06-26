package node_test

import (
	"reflect"
	"testing"

	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/node"
)

func TestResolveHTMLLinks_ResolvesRelativeSiblings(test *testing.T) {
	got := node.ResolveHTMLLinks("mml/index.html", []string{
		"topic-map.html",
		"layer-0-proofs.html",
		"sub/nested.html",
	})

	want := []string{"mml/topic-map.html", "mml/layer-0-proofs.html", "mml/sub/nested.html"}

	if !reflect.DeepEqual(got, want) {
		test.Errorf("got %v, want %v", got, want)
	}
}

func TestResolveHTMLLinks_ResolvesParentAndRootRelative(test *testing.T) {
	got := node.ResolveHTMLLinks("mml/index.html", []string{
		"../sibling.html",
		"/from-root.html",
	})

	want := []string{"sibling.html", "from-root.html"}

	if !reflect.DeepEqual(got, want) {
		test.Errorf("got %v, want %v", got, want)
	}
}

func TestResolveHTMLLinks_DropsQueryAndFragment(test *testing.T) {
	got := node.ResolveHTMLLinks("mml/index.html", []string{
		"layer-0-proofs.html#section",
		"topic-map.html?v=2",
	})

	want := []string{"mml/layer-0-proofs.html", "mml/topic-map.html"}

	if !reflect.DeepEqual(got, want) {
		test.Errorf("got %v, want %v", got, want)
	}
}

func TestResolveHTMLLinks_IgnoresExternalAnchorAndEscaping(test *testing.T) {
	got := node.ResolveHTMLLinks("mml/index.html", []string{
		"https://example.com/page.html",
		"mailto:a@b.com",
		"//cdn.example.com/x.html",
		"#in-page",
		"",
		"../../escapes.html",
	})

	if len(got) != 0 {
		test.Errorf("got %v, want empty (all ignored)", got)
	}
}

func TestResolveHTMLLinks_DedupesInFirstSeenOrder(test *testing.T) {
	got := node.ResolveHTMLLinks("mml/index.html", []string{
		"topic-map.html",
		"layer-0.html",
		"topic-map.html#again",
	})

	want := []string{"mml/topic-map.html", "mml/layer-0.html"}

	if !reflect.DeepEqual(got, want) {
		test.Errorf("got %v, want %v", got, want)
	}
}

func TestResolveHTMLLinks_RootLevelSource(test *testing.T) {
	got := node.ResolveHTMLLinks("index.html", []string{"about.html"})

	want := []string{"about.html"}

	if !reflect.DeepEqual(got, want) {
		test.Errorf("got %v, want %v", got, want)
	}
}

func TestMaterializeHTMLLinks_FlagsWikilinkEdgeTypes(test *testing.T) {
	parsed := &node.Node{
		Path:      "mml/index.html",
		HTMLLinks: []string{"topic-map.html", "layer-0.html"},
		Edges:     map[string][]string{},
	}

	edgeTypes := manifest.EdgeTypes{
		"references": manifest.EdgeType{Wikilinks: true},
	}

	node.MaterializeHTMLLinks(parsed, edgeTypes)

	want := []string{"mml/topic-map.html", "mml/layer-0.html"}

	if !reflect.DeepEqual(parsed.Edges["references"], want) {
		test.Errorf("references = %v, want %v", parsed.Edges["references"], want)
	}
}

func TestMaterializeHTMLLinks_SkipsUnflaggedEdgeTypes(test *testing.T) {
	parsed := &node.Node{
		Path:      "mml/index.html",
		HTMLLinks: []string{"topic-map.html"},
		Edges:     map[string][]string{},
	}

	edgeTypes := manifest.EdgeTypes{
		"references": manifest.EdgeType{Wikilinks: false},
	}

	node.MaterializeHTMLLinks(parsed, edgeTypes)

	if len(parsed.Edges["references"]) != 0 {
		test.Errorf("references = %v, want empty (no flag)", parsed.Edges["references"])
	}
}

func TestMaterializeHTMLLinks_NoLinksIsNoOp(test *testing.T) {
	parsed := &node.Node{
		Path:  "notes/source",
		Edges: map[string][]string{},
	}

	edgeTypes := manifest.EdgeTypes{
		"references": manifest.EdgeType{Wikilinks: true},
	}

	node.MaterializeHTMLLinks(parsed, edgeTypes)

	if len(parsed.Edges["references"]) != 0 {
		test.Errorf("references = %v, want empty (no HTML links)", parsed.Edges["references"])
	}
}

func TestMaterializeHTMLLinks_DedupesAgainstExistingEdges(test *testing.T) {
	parsed := &node.Node{
		Path:      "mml/index.html",
		HTMLLinks: []string{"topic-map.html", "topic-map.html"},
		Edges: map[string][]string{
			"references": {"mml/topic-map.html"},
		},
	}

	edgeTypes := manifest.EdgeTypes{
		"references": manifest.EdgeType{Wikilinks: true},
	}

	node.MaterializeHTMLLinks(parsed, edgeTypes)

	want := []string{"mml/topic-map.html"}

	if !reflect.DeepEqual(parsed.Edges["references"], want) {
		test.Errorf("references = %v, want %v", parsed.Edges["references"], want)
	}
}
