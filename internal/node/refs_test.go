package node_test

import (
	"strings"
	"testing"

	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/node"
)

// fakeRefLookup is the test-only RefLookup. Title lookups consult the
// titles map; node-ID lookups consult the ids map.
type fakeRefLookup struct {
	titles map[string]map[string][]string // type → title → []nodeID
	ids    map[string]string              // nodeID → type (presence indicates existence)
}

func (lookup *fakeRefLookup) FindByID(nodeID string) (foundType string, found bool) {
	foundType, found = lookup.ids[nodeID]
	return
}

func (lookup *fakeRefLookup) FindByTitle(targetType, title string) ([]string, error) {
	if targetType == "*" {
		var all []string
		for _, byTitle := range lookup.titles {
			all = append(all, byTitle[title]...)
		}
		return all, nil
	}
	return lookup.titles[targetType][title], nil
}

func newParsedNode(nodeID, nodeType string, props map[string]any) *node.Node {
	return &node.Node{
		ID:         nodeID,
		Type:       nodeType,
		Path:       nodeID + ".md",
		Properties: props,
		Edges:      map[string][]string{},
	}
}

func TestResolveRefs_BareTitleResolves(test *testing.T) {
	parsed := newParsedNode("tickets/auth", "ticket", map[string]any{"assignee": "alice"})

	decls := map[string]manifest.NodeType{
		"ticket": {Properties: []manifest.PropertyDecl{{Name: "assignee", Type: "ref", To: "person"}}},
	}

	lookup := &fakeRefLookup{
		titles: map[string]map[string][]string{"person": {"alice": {"people/alice"}}},
		ids:    map[string]string{"people/alice": "person"},
	}

	result := node.ResolveRefs(parsed, decls, lookup)

	if len(result.HardErrors) > 0 {
		test.Fatalf("HardErrors = %v", result.HardErrors)
	}

	if len(result.Edges) != 1 {
		test.Fatalf("Edges = %v, want 1", result.Edges)
	}

	edge := result.Edges[0]

	if edge.EdgeType != "assignee" || edge.TargetID != "people/alice" {
		test.Errorf("Edge = %+v", edge)
	}
}

func TestResolveRefs_WikilinkResolvesToNodeID(test *testing.T) {
	parsed := newParsedNode("tickets/auth", "ticket", map[string]any{"assignee": "[[people/alice]]"})

	decls := map[string]manifest.NodeType{
		"ticket": {Properties: []manifest.PropertyDecl{{Name: "assignee", Type: "ref", To: "person"}}},
	}

	lookup := &fakeRefLookup{
		titles: map[string]map[string][]string{},
		ids:    map[string]string{"people/alice": "person"},
	}

	result := node.ResolveRefs(parsed, decls, lookup)

	if len(result.HardErrors) > 0 {
		test.Fatalf("HardErrors = %v", result.HardErrors)
	}

	if len(result.Edges) != 1 || result.Edges[0].TargetID != "people/alice" {
		test.Fatalf("Edges = %+v", result.Edges)
	}
}

// #690: an aliased wikilink `[[id|display]]` in a ref-property value resolves
// to id, dropping the display suffix — the same rule the body extractor uses.
func TestResolveRefs_AliasedWikilinkResolvesToNodeID(test *testing.T) {
	parsed := newParsedNode("tickets/auth", "ticket", map[string]any{"assignee": "[[people/alice|Alice A.]]"})

	decls := map[string]manifest.NodeType{
		"ticket": {Properties: []manifest.PropertyDecl{{Name: "assignee", Type: "ref", To: "person"}}},
	}

	lookup := &fakeRefLookup{
		titles: map[string]map[string][]string{},
		ids:    map[string]string{"people/alice": "person"},
	}

	result := node.ResolveRefs(parsed, decls, lookup)

	if len(result.HardErrors) > 0 {
		test.Fatalf("HardErrors = %v", result.HardErrors)
	}

	if len(result.Edges) != 1 || result.Edges[0].TargetID != "people/alice" {
		test.Fatalf("Edges = %+v", result.Edges)
	}
}

func TestResolveRefs_DanglingTitleHardErrors(test *testing.T) {
	parsed := newParsedNode("tickets/auth", "ticket", map[string]any{"assignee": "missing"})

	decls := map[string]manifest.NodeType{
		"ticket": {Properties: []manifest.PropertyDecl{{Name: "assignee", Type: "ref", To: "person"}}},
	}

	lookup := &fakeRefLookup{titles: map[string]map[string][]string{}, ids: map[string]string{}}

	result := node.ResolveRefs(parsed, decls, lookup)

	if len(result.HardErrors) != 1 {
		test.Fatalf("HardErrors = %+v, want 1", result.HardErrors)
	}

	if result.HardErrors[0].Kind != node.RefErrDangling || result.HardErrors[0].Property != "assignee" {
		test.Errorf("HardErrors[0] = %+v", result.HardErrors[0])
	}

	if len(result.Edges) != 0 {
		test.Errorf("Edges = %+v, want none", result.Edges)
	}
}

func TestResolveRefs_AmbiguousTitleHardErrors(test *testing.T) {
	parsed := newParsedNode("tickets/auth", "ticket", map[string]any{"assignee": "alice"})

	decls := map[string]manifest.NodeType{
		"ticket": {Properties: []manifest.PropertyDecl{{Name: "assignee", Type: "ref", To: "person"}}},
	}

	lookup := &fakeRefLookup{
		titles: map[string]map[string][]string{"person": {"alice": {"people/alice-1", "people/alice-2"}}},
		ids:    map[string]string{},
	}

	result := node.ResolveRefs(parsed, decls, lookup)

	if len(result.HardErrors) != 1 || result.HardErrors[0].Kind != node.RefErrAmbiguous {
		test.Fatalf("HardErrors = %+v, want one ambiguous", result.HardErrors)
	}

	if !strings.Contains(result.HardErrors[0].Reason, "people/alice-1") {
		test.Errorf("Reason = %q, expected to mention candidate", result.HardErrors[0].Reason)
	}
}

func TestResolveRefs_TypeMismatchOnWikilink(test *testing.T) {
	parsed := newParsedNode("tickets/auth", "ticket", map[string]any{"assignee": "[[people/bob]]"})

	decls := map[string]manifest.NodeType{
		"ticket": {Properties: []manifest.PropertyDecl{{Name: "assignee", Type: "ref", To: "person"}}},
	}

	lookup := &fakeRefLookup{
		titles: map[string]map[string][]string{},
		ids:    map[string]string{"people/bob": "user"}, // exists, but type=user not person
	}

	result := node.ResolveRefs(parsed, decls, lookup)

	if len(result.HardErrors) != 1 || result.HardErrors[0].Kind != node.RefErrTypeMismatch {
		test.Fatalf("HardErrors = %+v, want one type-mismatch", result.HardErrors)
	}
}

func TestResolveRefs_ListOfRefPreservesOrder(test *testing.T) {
	parsed := newParsedNode("tickets/auth", "ticket", map[string]any{
		"watchers": []any{"alice", "[[people/bob]]"},
	})

	decls := map[string]manifest.NodeType{
		"ticket": {Properties: []manifest.PropertyDecl{
			{Name: "watchers", Type: "list-of", ItemType: "ref", To: "person"},
		}},
	}

	lookup := &fakeRefLookup{
		titles: map[string]map[string][]string{"person": {"alice": {"people/alice"}}},
		ids:    map[string]string{"people/bob": "person"},
	}

	result := node.ResolveRefs(parsed, decls, lookup)

	if len(result.HardErrors) > 0 {
		test.Fatalf("HardErrors = %+v", result.HardErrors)
	}

	if len(result.Edges) != 2 {
		test.Fatalf("Edges = %+v, want 2", result.Edges)
	}

	if result.Edges[0].TargetID != "people/alice" {
		test.Errorf("Edges[0] = %+v, want target people/alice", result.Edges[0])
	}

	if result.Edges[1].TargetID != "people/bob" {
		test.Errorf("Edges[1] = %+v, want target people/bob", result.Edges[1])
	}
}

func TestResolveRefs_EmptyValueSkipped(test *testing.T) {
	parsed := newParsedNode("tickets/auth", "ticket", map[string]any{"assignee": ""})

	decls := map[string]manifest.NodeType{
		"ticket": {Properties: []manifest.PropertyDecl{{Name: "assignee", Type: "ref", To: "person"}}},
	}

	lookup := &fakeRefLookup{}

	result := node.ResolveRefs(parsed, decls, lookup)

	if len(result.HardErrors) != 0 || len(result.Edges) != 0 {
		test.Errorf("expected empty result; got %+v", result)
	}
}

func TestResolveRefs_NilValueSkipped(test *testing.T) {
	parsed := newParsedNode("tickets/auth", "ticket", map[string]any{"assignee": nil})

	decls := map[string]manifest.NodeType{
		"ticket": {Properties: []manifest.PropertyDecl{{Name: "assignee", Type: "ref", To: "person"}}},
	}

	lookup := &fakeRefLookup{}

	result := node.ResolveRefs(parsed, decls, lookup)

	if len(result.HardErrors) != 0 || len(result.Edges) != 0 {
		test.Errorf("expected empty result; got %+v", result)
	}
}

func TestResolveRefs_WildcardToAcceptsAnyType(test *testing.T) {
	parsed := newParsedNode("tickets/auth", "ticket", map[string]any{"linked": "[[anything/x]]"})

	decls := map[string]manifest.NodeType{
		"ticket": {Properties: []manifest.PropertyDecl{{Name: "linked", Type: "ref", To: "*"}}},
	}

	lookup := &fakeRefLookup{ids: map[string]string{"anything/x": "whatever"}}

	result := node.ResolveRefs(parsed, decls, lookup)

	if len(result.HardErrors) != 0 || len(result.Edges) != 1 {
		test.Errorf("expected one edge; got %+v", result)
	}
}
