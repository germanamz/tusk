package webui

import (
	"database/sql"
	"errors"
	"sort"
	"testing"

	"github.com/germanamz/tusk/internal/index"
)

// fakeNodeLister mirrors *index.NodeRepo.ListByIDs: matching rows ordered by id
// ASC, silently omitting ids with no row (which is what makes a dangling edge
// target detectable only by its absence).
type fakeNodeLister struct {
	byID    map[string]index.NodeRow
	listErr error

	// calls counts ListByIDs invocations and requestedIDs records the ids of the
	// last call, so a test can pin the single-batched-lookup contract.
	calls        int
	requestedIDs []string
}

func (fake *fakeNodeLister) ListByIDs(ids []string) ([]index.NodeRow, error) {
	fake.calls++
	fake.requestedIDs = append([]string(nil), ids...)

	if fake.listErr != nil {
		return nil, fake.listErr
	}

	out := make([]index.NodeRow, 0, len(ids))

	for _, id := range ids {
		if row, ok := fake.byID[id]; ok {
			out = append(out, row)
		}
	}

	sort.SliceStable(out, func(left, right int) bool {
		return out[left].ID < out[right].ID
	})

	return out, nil
}

// fakeEdgeLister mirrors *index.EdgeRepo: ListBySource orders by (type,
// target_id), ListByTarget by (type, source_id) — neither is the global
// ListAll order, which is what Neighbors re-sorts to.
type fakeEdgeLister struct {
	all       []index.EdgeRow
	sourceErr error
	targetErr error
}

func (fake *fakeEdgeLister) ListBySource(sourceID string) ([]index.EdgeRow, error) {
	if fake.sourceErr != nil {
		return nil, fake.sourceErr
	}

	out := make([]index.EdgeRow, 0)

	for _, row := range fake.all {
		if row.SourceID == sourceID {
			out = append(out, row)
		}
	}

	sort.SliceStable(out, func(left, right int) bool {
		if out[left].Type != out[right].Type {
			return out[left].Type < out[right].Type
		}

		return out[left].TargetID < out[right].TargetID
	})

	return out, nil
}

func (fake *fakeEdgeLister) ListByTarget(targetID string) ([]index.EdgeRow, error) {
	if fake.targetErr != nil {
		return nil, fake.targetErr
	}

	out := make([]index.EdgeRow, 0)

	for _, row := range fake.all {
		if row.TargetID == targetID {
			out = append(out, row)
		}
	}

	sort.SliceStable(out, func(left, right int) bool {
		if out[left].Type != out[right].Type {
			return out[left].Type < out[right].Type
		}

		return out[left].SourceID < out[right].SourceID
	})

	return out, nil
}

// fileNode builds a file-level NodeRow (ParentID NULL).
func fileNode(id, nodeType, title string) index.NodeRow {
	return index.NodeRow{ID: id, Type: nodeType, Title: title, Path: id + ".md"}
}

// subUnitNode builds a sub-unit NodeRow under parentID (ParentID set).
func subUnitNode(id, parentID, nodeType, title string) index.NodeRow {
	return index.NodeRow{
		ID:       id,
		Type:     nodeType,
		Title:    title,
		Path:     parentID + ".md",
		ParentID: sql.NullString{String: parentID, Valid: true},
	}
}

// edgeRow builds an EdgeRow with the given kind.
func edgeRow(edgeType, source, target, kind string) index.EdgeRow {
	return index.EdgeRow{Type: edgeType, SourceID: source, TargetID: target, Kind: kind}
}

// TestNeighbors_OrdersByListAllGlobalOrder pins the re-sort to ListAll's global
// (SourceID, Type, TargetID). The fixture is chosen so the per-list orders the
// repos return (out-edges first, then in-edges) do NOT match the expected
// order: the result interleaves in- and out-edges by source id.
func TestNeighbors_OrdersByListAllGlobalOrder(test *testing.T) {
	nodes := &fakeNodeLister{byID: map[string]index.NodeRow{
		"notes/aaa": fileNode("notes/aaa", "note", "AAA"),
		"notes/bbb": fileNode("notes/bbb", "note", "BBB"),
		"notes/mid": fileNode("notes/mid", "note", "MID"),
		"notes/zzz": fileNode("notes/zzz", "note", "ZZZ"),
	}}
	edges := &fakeEdgeLister{all: []index.EdgeRow{
		edgeRow("references", "notes/zzz", "notes/mid", "direct"),
		edgeRow("references", "notes/mid", "notes/bbb", "direct"),
		edgeRow("references", "notes/aaa", "notes/mid", "direct"),
		edgeRow("mentions", "notes/aaa", "notes/mid", "derived"),
		edgeRow("references", "notes/mid", "notes/aaa", "direct"),
	}}

	got, err := Neighbors(nodes, edges, "notes/mid")

	if err != nil {
		test.Fatalf("Neighbors: %v", err)
	}

	type step struct {
		farID     string
		edgeType  string
		direction string
	}

	want := []step{
		{farID: "notes/aaa", edgeType: "mentions", direction: "in"},    // source notes/aaa, type mentions
		{farID: "notes/aaa", edgeType: "references", direction: "in"},  // source notes/aaa, type references
		{farID: "notes/aaa", edgeType: "references", direction: "out"}, // source notes/mid, target notes/aaa
		{farID: "notes/bbb", edgeType: "references", direction: "out"}, // source notes/mid, target notes/bbb
		{farID: "notes/zzz", edgeType: "references", direction: "in"},  // source notes/zzz
	}

	if len(got) != len(want) {
		test.Fatalf("len(neighbors) = %d, want %d: %+v", len(got), len(want), got)
	}

	for idx, expected := range want {
		actual := step{farID: got[idx].Node.ID, edgeType: got[idx].Edge.Type, direction: got[idx].Direction}

		if actual != expected {
			test.Fatalf("neighbor[%d] = %+v, want %+v", idx, actual, expected)
		}
	}
}

// TestNeighbors_SelfLoopEmittedOnceAsOut pins the de-duplication: a self-loop
// comes back from both ListBySource and ListByTarget, but ListAll yields it
// once and classifies it "out".
func TestNeighbors_SelfLoopEmittedOnceAsOut(test *testing.T) {
	nodes := &fakeNodeLister{byID: map[string]index.NodeRow{
		"notes/solo": fileNode("notes/solo", "note", "Solo"),
	}}
	edges := &fakeEdgeLister{all: []index.EdgeRow{
		edgeRow("related", "notes/solo", "notes/solo", "direct"),
	}}

	got, err := Neighbors(nodes, edges, "notes/solo")

	if err != nil {
		test.Fatalf("Neighbors: %v", err)
	}

	if len(got) != 1 {
		test.Fatalf("len(neighbors) = %d, want 1 (self-loop must not double-emit): %+v", len(got), got)
	}

	if got[0].Direction != "out" {
		test.Fatalf("self-loop direction = %q, want out", got[0].Direction)
	}

	if got[0].Node.ID != "notes/solo" {
		test.Fatalf("self-loop far id = %q, want notes/solo", got[0].Node.ID)
	}
}

// TestNeighbors_SkipsSubUnitFarEnds pins the file-level rule: ListByIDs
// resolves sub-unit rows too, so they must be excluded by ParentID, not by a
// not-found.
func TestNeighbors_SkipsSubUnitFarEnds(test *testing.T) {
	nodes := &fakeNodeLister{byID: map[string]index.NodeRow{
		"notes/aaa":    fileNode("notes/aaa", "note", "AAA"),
		"notes/bbb":    fileNode("notes/bbb", "note", "BBB"),
		"notes/aaa#s1": subUnitNode("notes/aaa#s1", "notes/aaa", "section", "Section 1"),
	}}
	edges := &fakeEdgeLister{all: []index.EdgeRow{
		edgeRow("contains", "notes/aaa", "notes/aaa#s1", "structural"),
		edgeRow("references", "notes/aaa", "notes/bbb", "direct"),
	}}

	got, err := Neighbors(nodes, edges, "notes/aaa")

	if err != nil {
		test.Fatalf("Neighbors: %v", err)
	}

	if len(got) != 1 || got[0].Node.ID != "notes/bbb" {
		test.Fatalf("neighbors = %+v, want only notes/bbb (sub-unit far end excluded)", got)
	}
}

// TestNeighbors_KeepsStructuralEdgesToFileNodes guards the layering rule: the
// sub-unit skip is keyed on the far node's ParentID, NOT on the edge's Kind.
// Filtering structural edges is a view's business, not this function's — the
// graph view keeps them.
func TestNeighbors_KeepsStructuralEdgesToFileNodes(test *testing.T) {
	nodes := &fakeNodeLister{byID: map[string]index.NodeRow{
		"notes/aaa": fileNode("notes/aaa", "note", "AAA"),
		"notes/bbb": fileNode("notes/bbb", "note", "BBB"),
	}}
	edges := &fakeEdgeLister{all: []index.EdgeRow{
		edgeRow("contains", "notes/aaa", "notes/bbb", "structural"),
	}}

	got, err := Neighbors(nodes, edges, "notes/aaa")

	if err != nil {
		test.Fatalf("Neighbors: %v", err)
	}

	if len(got) != 1 || got[0].Edge.Kind != "structural" {
		test.Fatalf("neighbors = %+v, want the structural edge to a file node retained", got)
	}
}

// TestNeighbors_SkipsDanglingFarEnds pins the drop of an edge whose far end has
// no node row. ListByIDs omits missing ids silently rather than erroring.
func TestNeighbors_SkipsDanglingFarEnds(test *testing.T) {
	nodes := &fakeNodeLister{byID: map[string]index.NodeRow{
		"notes/aaa": fileNode("notes/aaa", "note", "AAA"),
		"notes/bbb": fileNode("notes/bbb", "note", "BBB"),
	}}
	edges := &fakeEdgeLister{all: []index.EdgeRow{
		edgeRow("references", "notes/aaa", "notes/ghost", "direct"),
		edgeRow("references", "notes/aaa", "notes/bbb", "direct"),
		edgeRow("mentions", "notes/phantom", "notes/aaa", "derived"),
	}}

	got, err := Neighbors(nodes, edges, "notes/aaa")

	if err != nil {
		test.Fatalf("Neighbors: %v", err)
	}

	if len(got) != 1 || got[0].Node.ID != "notes/bbb" {
		test.Fatalf("neighbors = %+v, want only notes/bbb (dangling far ends skipped)", got)
	}
}

// TestNeighbors_IsolatedNodeReturnsEmptyNonNil pins the empty-but-non-nil
// return. A nil slice marshals to JSON null where the views' wire contract is
// []; no view test covers an isolated node, so this is the only guard.
func TestNeighbors_IsolatedNodeReturnsEmptyNonNil(test *testing.T) {
	nodes := &fakeNodeLister{byID: map[string]index.NodeRow{
		"notes/lonely": fileNode("notes/lonely", "note", "Lonely"),
	}}
	edges := &fakeEdgeLister{all: []index.EdgeRow{}}

	got, err := Neighbors(nodes, edges, "notes/lonely")

	if err != nil {
		test.Fatalf("Neighbors: %v", err)
	}

	if got == nil {
		test.Fatal("neighbors = nil, want an empty non-nil slice (nil marshals to null, not [])")
	}

	if len(got) != 0 {
		test.Fatalf("neighbors = %+v, want empty", got)
	}
}

// TestNeighbors_AllFarEndsFilteredReturnsEmptyNonNil covers the second path to
// an empty result: edges exist, but every far end is dropped.
func TestNeighbors_AllFarEndsFilteredReturnsEmptyNonNil(test *testing.T) {
	nodes := &fakeNodeLister{byID: map[string]index.NodeRow{
		"notes/aaa":    fileNode("notes/aaa", "note", "AAA"),
		"notes/aaa#s1": subUnitNode("notes/aaa#s1", "notes/aaa", "section", "Section 1"),
	}}
	edges := &fakeEdgeLister{all: []index.EdgeRow{
		edgeRow("contains", "notes/aaa", "notes/aaa#s1", "structural"),
		edgeRow("references", "notes/aaa", "notes/ghost", "direct"),
	}}

	got, err := Neighbors(nodes, edges, "notes/aaa")

	if err != nil {
		test.Fatalf("Neighbors: %v", err)
	}

	if got == nil {
		test.Fatal("neighbors = nil, want an empty non-nil slice (nil marshals to null, not [])")
	}

	if len(got) != 0 {
		test.Fatalf("neighbors = %+v, want empty", got)
	}
}

// TestNeighbors_CarriesFarNodeAndEdgeRows pins the payload each view projects
// from: the far node row and the edge row, not a view shape.
func TestNeighbors_CarriesFarNodeAndEdgeRows(test *testing.T) {
	nodes := &fakeNodeLister{byID: map[string]index.NodeRow{
		"notes/aaa": fileNode("notes/aaa", "note", "AAA"),
		"notes/bbb": fileNode("notes/bbb", "task", "BBB"),
	}}
	edges := &fakeEdgeLister{all: []index.EdgeRow{
		edgeRow("references", "notes/aaa", "notes/bbb", "direct"),
	}}

	got, err := Neighbors(nodes, edges, "notes/aaa")

	if err != nil {
		test.Fatalf("Neighbors: %v", err)
	}

	if len(got) != 1 {
		test.Fatalf("len(neighbors) = %d, want 1", len(got))
	}

	adj := got[0]

	if adj.Node.ID != "notes/bbb" || adj.Node.Type != "task" || adj.Node.Title != "BBB" {
		test.Fatalf("far node = %+v, want the notes/bbb row", adj.Node)
	}

	if adj.Edge.Type != "references" || adj.Edge.Kind != "direct" {
		test.Fatalf("edge = %+v, want references/direct", adj.Edge)
	}

	if adj.Direction != "out" {
		test.Fatalf("direction = %q, want out", adj.Direction)
	}
}

// TestNeighbors_ResolvesFarEndsInOneDeduplicatedBatch pins the lookup contract
// the views rely on: far ends are hydrated by a single ListByIDs call carrying
// each distinct far id once, never one lookup per edge. The fixture points four
// edges at just two distinct far nodes, so a lost de-duplication shows up as a
// repeated id rather than only as a hidden extra query.
func TestNeighbors_ResolvesFarEndsInOneDeduplicatedBatch(test *testing.T) {
	nodes := &fakeNodeLister{byID: map[string]index.NodeRow{
		"notes/aaa": fileNode("notes/aaa", "note", "AAA"),
		"notes/bbb": fileNode("notes/bbb", "note", "BBB"),
		"notes/ccc": fileNode("notes/ccc", "note", "CCC"),
	}}
	edges := &fakeEdgeLister{all: []index.EdgeRow{
		edgeRow("references", "notes/aaa", "notes/bbb", "direct"),
		edgeRow("mentions", "notes/aaa", "notes/bbb", "derived"),
		edgeRow("references", "notes/bbb", "notes/aaa", "direct"),
		edgeRow("references", "notes/ccc", "notes/aaa", "direct"),
	}}

	got, err := Neighbors(nodes, edges, "notes/aaa")

	if err != nil {
		test.Fatalf("Neighbors: %v", err)
	}

	if len(got) != 4 {
		test.Fatalf("len(neighbors) = %d, want 4: %+v", len(got), got)
	}

	if nodes.calls != 1 {
		test.Fatalf("ListByIDs calls = %d, want exactly 1 batched lookup", nodes.calls)
	}

	seen := make(map[string]int, len(nodes.requestedIDs))

	for _, id := range nodes.requestedIDs {
		seen[id]++
	}

	for id, count := range seen {
		if count > 1 {
			test.Fatalf("ListByIDs got id %q %d times, want distinct ids only: %v", id, count, nodes.requestedIDs)
		}
	}

	if len(nodes.requestedIDs) != 2 {
		test.Fatalf("ListByIDs got %v, want the 2 distinct far ids", nodes.requestedIDs)
	}
}

func TestNeighbors_PropagatesErrors(test *testing.T) {
	sentinel := errors.New("boom")
	nodes := &fakeNodeLister{byID: map[string]index.NodeRow{
		"notes/aaa": fileNode("notes/aaa", "note", "AAA"),
		"notes/bbb": fileNode("notes/bbb", "note", "BBB"),
	}}

	cases := []struct {
		name  string
		edges *fakeEdgeLister
		nodes *fakeNodeLister
	}{
		{
			name:  "list by source fails",
			edges: &fakeEdgeLister{sourceErr: sentinel},
			nodes: nodes,
		},
		{
			name:  "list by target fails",
			edges: &fakeEdgeLister{targetErr: sentinel},
			nodes: nodes,
		},
		{
			name:  "far-end hydration fails",
			edges: &fakeEdgeLister{all: []index.EdgeRow{edgeRow("references", "notes/aaa", "notes/bbb", "direct")}},
			nodes: &fakeNodeLister{listErr: sentinel},
		},
	}

	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			got, err := Neighbors(testCase.nodes, testCase.edges, "notes/aaa")

			if !errors.Is(err, sentinel) {
				test.Fatalf("err = %v, want %v", err, sentinel)
			}

			if got != nil {
				test.Fatalf("neighbors = %+v, want nil on error", got)
			}
		})
	}
}
