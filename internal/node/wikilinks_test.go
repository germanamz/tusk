package node_test

import (
	"reflect"
	"sort"
	"testing"

	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/node"
)

func TestExtractWikilinks_FindsBracketedTargets(test *testing.T) {
	body := []byte(`# Body

See [[notes/auth-rfc]] for context.

Also relates to [[tickets/refactor-storage]].

A second mention of [[notes/auth-rfc]] is deduped.
`)

	links := node.ExtractWikilinks(body)

	sort.Strings(links)

	want := []string{"notes/auth-rfc", "tickets/refactor-storage"}

	if !reflect.DeepEqual(links, want) {
		test.Errorf("got %v, want %v", links, want)
	}
}

func TestExtractWikilinks_IgnoresEscapedAndCodeFence(test *testing.T) {
	body := []byte("Real link [[real]].\n\n```\nfenced [[notreal]]\n```\n")

	links := node.ExtractWikilinks(body)

	if len(links) != 1 || links[0] != "real" {
		test.Errorf("got %v, want [real]", links)
	}
}

func TestExtractWikilinks_ReturnsEmptyWhenNoLinks(test *testing.T) {
	links := node.ExtractWikilinks([]byte("plain body, no brackets at all\n"))

	if len(links) != 0 {
		test.Errorf("got %v, want empty", links)
	}
}

func TestMaterializeWikilinks_FlagsArbitraryEdgeName(test *testing.T) {
	parsed := &node.Node{
		Body:  []byte("See [[notes/target]] and [[notes/other]].\n"),
		Edges: map[string][]string{},
	}

	edgeTypes := manifest.EdgeTypes{
		"wbs-references": manifest.EdgeType{Wikilinks: true},
	}

	node.MaterializeWikilinks(parsed, edgeTypes)

	want := []string{"notes/target", "notes/other"}

	if !reflect.DeepEqual(parsed.Edges["wbs-references"], want) {
		test.Errorf("wbs-references = %v, want %v", parsed.Edges["wbs-references"], want)
	}
}

func TestMaterializeWikilinks_SkipsUnflaggedReferences(test *testing.T) {
	parsed := &node.Node{
		Body:  []byte("See [[notes/target]].\n"),
		Edges: map[string][]string{},
	}

	edgeTypes := manifest.EdgeTypes{
		"references": manifest.EdgeType{Wikilinks: false},
	}

	node.MaterializeWikilinks(parsed, edgeTypes)

	if len(parsed.Edges["references"]) != 0 {
		test.Errorf("references = %v, want empty (no flag)", parsed.Edges["references"])
	}
}

func TestMaterializeWikilinks_MultipleFlaggedEdges(test *testing.T) {
	parsed := &node.Node{
		Body:  []byte("See [[notes/target]].\n"),
		Edges: map[string][]string{},
	}

	edgeTypes := manifest.EdgeTypes{
		"references":     manifest.EdgeType{Wikilinks: true},
		"wbs-references": manifest.EdgeType{Wikilinks: true},
	}

	node.MaterializeWikilinks(parsed, edgeTypes)

	for _, name := range []string{"references", "wbs-references"} {
		if len(parsed.Edges[name]) != 1 || parsed.Edges[name][0] != "notes/target" {
			test.Errorf("%s = %v, want [notes/target]", name, parsed.Edges[name])
		}
	}
}
