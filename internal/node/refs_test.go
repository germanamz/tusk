package node_test

import (
	"strings"
	"testing"

	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/node"
	"github.com/germanamz/tusk/internal/wikilink"
)

// fakeRefLookup is the test-only RefLookup. Title lookups consult the
// titles map; node-ID lookups consult the ids map.
type fakeRefLookup struct {
	titles   map[string]map[string][]string // type → title → []nodeID
	ids      map[string]string              // nodeID → type (presence indicates existence)
	sections map[string][]wikilink.Section  // file id → its sections, for heading fragments
	blocks   map[string]map[string]string   // file id → block key → sub-unit, for block references
}

func (lookup *fakeRefLookup) FindByID(nodeID string) (foundType string, found bool) {
	foundType, found = lookup.ids[nodeID]
	return
}

// ResolveName matches a name-form wikilink against the ids map: the id itself,
// else every id whose last path segment is the name.
func (lookup *fakeRefLookup) ResolveName(name string) (string, []string, error) {
	names := wikilink.NewNames(wikilink.DefaultCase)

	for id := range lookup.ids {
		names.Add(id, id+".md")
	}

	return names.ResolveName(name)
}

// Exists reports a node in the ids map.
func (lookup *fakeRefLookup) Exists(id string) (bool, error) {
	_, found := lookup.ids[id]

	return found, nil
}

// Sections serves the sections map.
func (lookup *fakeRefLookup) Sections(fileID string) ([]wikilink.Section, error) {
	return lookup.sections[fileID], nil
}

// Blocks serves the blocks map.
func (lookup *fakeRefLookup) Blocks(fileID string) (map[string]string, error) {
	return lookup.blocks[fileID], nil
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

// #791: a wikilink ref value resolves like a body link. A file name finds the
// one file with that name, two files make it ref_ambiguous with both listed,
// and a relative path resolves from the node's folder.
func TestResolveRefs_WikilinkNamesResolveByFileName(test *testing.T) {
	decls := map[string]manifest.NodeType{
		"ticket": {Properties: []manifest.PropertyDecl{{Name: "owner", Type: "ref", To: "person"}}},
	}

	lookup := &fakeRefLookup{
		ids: map[string]string{"people/alice": "person", "a/bob": "person", "b/bob": "person"},
	}

	for _, tc := range []struct {
		value      string
		wantTarget string
		wantKind   node.RefErrKind
		candidates []string
	}{
		{value: "[[alice|Alice]]", wantTarget: "people/alice"},
		{value: "[[../people/alice]]", wantTarget: "people/alice"},
		{value: "[[bob]]", wantKind: node.RefErrAmbiguous, candidates: []string{"a/bob", "b/bob"}},
		{value: "[[carol]]", wantKind: node.RefErrDangling},
	} {
		parsed := newParsedNode("tickets/t1", "ticket", map[string]any{"owner": tc.value})
		result := node.ResolveRefs(parsed, decls, lookup)

		if tc.wantTarget != "" {
			if len(result.HardErrors) > 0 || len(result.Edges) != 1 || result.Edges[0].TargetID != tc.wantTarget {
				test.Errorf("%s: edges %+v, errors %+v; want %s", tc.value, result.Edges, result.HardErrors, tc.wantTarget)
			}

			continue
		}

		if len(result.HardErrors) != 1 || result.HardErrors[0].Kind != tc.wantKind ||
			strings.Join(result.HardErrors[0].Candidates, ",") != strings.Join(tc.candidates, ",") {
			test.Errorf("%s: errors %+v, want one %s with candidates %v", tc.value, result.HardErrors, tc.wantKind, tc.candidates)
		}
	}
}

// #808: a wikilink ref value with a heading fragment resolves to the section
// that heading names. A ref is typed, so a heading the note lacks is a
// dangling ref rather than the note itself.
func TestResolveRefs_HeadingFragmentsResolveToTheirSection(test *testing.T) {
	decls := map[string]manifest.NodeType{
		"ticket": {Properties: []manifest.PropertyDecl{{Name: "spec", Type: "ref", To: "section"}}},
	}

	lookup := &fakeRefLookup{
		ids: map[string]string{
			"docs/rfc": "note", "docs/rfc#S1": "section", "docs/rfc#S2": "section",
			"tickets/t1": "ticket", "tickets/t1#S1": "section",
		},
		sections: map[string][]wikilink.Section{
			"docs/rfc": {
				{ID: "docs/rfc#S1", Parent: "docs/rfc", Key: "overview"},
				{ID: "docs/rfc#S2", Parent: "docs/rfc", Key: "open questions"},
			},
			"tickets/t1": {{ID: "tickets/t1#S1", Parent: "tickets/t1", Key: "context"}},
		},
	}

	for _, tc := range []struct {
		value      string
		wantTarget string
	}{
		{value: "[[rfc#Open Questions]]", wantTarget: "docs/rfc#S2"},
		{value: "[[docs/rfc#S1|the overview]]", wantTarget: "docs/rfc#S1"},
		{value: "[[#Context]]", wantTarget: "tickets/t1#S1"},
		{value: "[[rfc#Renamed]]"},
	} {
		parsed := newParsedNode("tickets/t1", "ticket", map[string]any{"spec": tc.value})
		result := node.ResolveRefs(parsed, decls, lookup)

		if tc.wantTarget == "" {
			if len(result.HardErrors) != 1 || result.HardErrors[0].Kind != node.RefErrDangling ||
				!strings.Contains(result.HardErrors[0].Reason, `docs/rfc has no sub-unit or heading "#Renamed"`) {
				test.Errorf("%s: errors %+v, want a dangling ref naming the missing heading", tc.value, result.HardErrors)
			}

			continue
		}

		if len(result.HardErrors) > 0 || len(result.Edges) != 1 || result.Edges[0].TargetID != tc.wantTarget {
			test.Errorf("%s: edges %+v, errors %+v; want %s", tc.value, result.Edges, result.HardErrors, tc.wantTarget)
		}
	}
}

// #819: a wikilink ref value with a block reference resolves to the unit its
// ^id names; an id the note lacks is a dangling ref naming the block.
func TestResolveRefs_BlockReferencesResolveToTheirUnit(test *testing.T) {
	decls := map[string]manifest.NodeType{
		"ticket": {Properties: []manifest.PropertyDecl{{Name: "quote", Type: "ref", To: "paragraph"}}},
	}

	lookup := &fakeRefLookup{
		ids:    map[string]string{"docs/rfc": "note", "docs/rfc#P2": "paragraph", "tickets/t1": "ticket"},
		blocks: map[string]map[string]string{"docs/rfc": {"^quote-1": "docs/rfc#P2"}},
	}

	parsed := newParsedNode("tickets/t1", "ticket", map[string]any{"quote": "[[rfc#^Quote-1]]"})

	if result := node.ResolveRefs(parsed, decls, lookup); len(result.HardErrors) > 0 || len(result.Edges) != 1 || result.Edges[0].TargetID != "docs/rfc#P2" {
		test.Errorf("edges %+v, errors %+v; want docs/rfc#P2", result.Edges, result.HardErrors)
	}

	parsed = newParsedNode("tickets/t1", "ticket", map[string]any{"quote": "[[rfc#^gone]]"})
	result := node.ResolveRefs(parsed, decls, lookup)

	if len(result.HardErrors) != 1 || result.HardErrors[0].Kind != node.RefErrDangling ||
		!strings.Contains(result.HardErrors[0].Reason, `docs/rfc has no block "#^gone"`) {
		test.Errorf("errors %+v, want a dangling ref naming the missing block", result.HardErrors)
	}
}
