package node_test

import (
	"strings"
	"testing"

	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/node"
)

// TestEdgeRows_RecordsAuthoredPosition pins #848 finding 16: each row records
// the 1-based place of its target among its type's targets, in the order the
// file writes them (a frontmatter list, then body links), so edge listings can
// keep that order instead of sorting by id. A repeated target keeps its first
// place and does not use one up.
func TestEdgeRows_RecordsAuthoredPosition(test *testing.T) {
	parsed := &node.Node{
		ID:   "doc",
		Path: "doc.md",
		Type: "note",
		Edges: map[string][]string{
			"describes":  {"zeta", "alpha", "zeta", "mid"},
			"references": {"omega", "beta"},
		},
	}

	positions := map[string]int{}

	for _, row := range node.EdgeRows(parsed, map[string]manifest.NodeType{}) {
		positions[row.Type+" "+row.TargetID] = row.Position
	}

	want := map[string]int{
		"describes zeta":   1,
		"describes alpha":  2,
		"describes mid":    3,
		"references omega": 1,
		"references beta":  2,
	}

	if len(positions) != len(want) {
		test.Errorf("rows = %v, want %v", positions, want)
	}

	for key, wantPosition := range want {
		if positions[key] != wantPosition {
			test.Errorf("%s: position %d, want %d", key, positions[key], wantPosition)
		}
	}
}

// TestGetRun_EdgeOptionsImplyEdges pins that an edge filter asks for edges the
// way include=edges does, and only for edges: the caller wants a structured
// envelope of them, not the raw file.
func TestGetRun_EdgeOptionsImplyEdges(test *testing.T) {
	service, _ := newTestService(test)

	if _, createErr := service.Create(node.CreateInput{RelPath: "notes/a.md", Type: "note", Title: "A"}); createErr != nil {
		test.Fatalf("Create: %v", createErr)
	}

	for _, req := range []node.GetRequest{
		{ID: "notes/a", EdgeType: "references"},
		{ID: "notes/a", EdgeDirection: "in"},
		{ID: "notes/a", SubUnitEdges: true},
	} {
		result, runErr := node.GetRun(service, req)

		if runErr != nil {
			test.Fatalf("GetRun(%+v): %v", req, runErr)
		}

		if !result.IncludeEdges || !result.HasIncludeFilter {
			test.Errorf("GetRun(%+v): IncludeEdges %v, HasIncludeFilter %v; want both", req, result.IncludeEdges, result.HasIncludeFilter)
		}

		if result.IncludeBody || result.IncludeProperties {
			test.Errorf("GetRun(%+v): body %v, properties %v; want edges only", req, result.IncludeBody, result.IncludeProperties)
		}
	}
}

// TestGetRun_RejectsUnknownDirection pins the one check every transport shares
// for the direction filter.
func TestGetRun_RejectsUnknownDirection(test *testing.T) {
	_, runErr := node.GetRun(nil, node.GetRequest{ID: "notes/a", EdgeDirection: "sideways"})

	if runErr == nil || !strings.Contains(runErr.Error(), "in, out or both") {
		test.Fatalf("GetRun error = %v, want a direction error", runErr)
	}
}
