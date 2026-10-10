package node_test

import (
	"reflect"
	"testing"

	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/node"
)

func TestMaterializeWikilinks_AliasedLink(test *testing.T) {
	parsed := &node.Node{
		Body:  []byte("See [[notes/target|the target]].\n"),
		Edges: map[string][]string{},
	}

	edgeTypes := manifest.EdgeTypes{
		"references": manifest.EdgeType{Wikilinks: true},
	}

	node.MaterializeWikilinks(parsed, edgeTypes)

	if len(parsed.Edges["references"]) != 1 || parsed.Edges["references"][0] != "notes/target" {
		test.Errorf("references = %v, want [notes/target]", parsed.Edges["references"])
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
