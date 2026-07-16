package bookview

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/germanamz/tusk/internal/index"
)

// TestIndexListsFileNodes pins GET /api/index's payload: every file-level row
// from NodeSource.ListFileNodes, with id/type/title/path round-tripping
// verbatim. There is no Parent field — every row ListFileNodes returns has a
// NULL parent_id by construction, so the Contents pane derives its tree from
// Path instead.
func TestIndexListsFileNodes(test *testing.T) {
	nodes := fakeNodes{file: []index.NodeRow{
		{ID: "a", Type: "note", Title: "A", Path: "a.md"},
		{ID: "b", Type: "spec", Title: "B", Path: "specs/b.md"},
	}}

	srv := New(Deps{Root: test.TempDir(), Nodes: nodes})

	rec := httptest.NewRecorder()
	srv.handleIndex(rec, httptest.NewRequest("GET", "/api/index", nil))

	var got IndexResponse

	if unmarshalErr := json.Unmarshal(rec.Body.Bytes(), &got); unmarshalErr != nil {
		test.Fatalf("unmarshal %q: %v", rec.Body.Bytes(), unmarshalErr)
	}

	if len(got.Nodes) != 2 {
		test.Fatalf("got %d nodes, want 2: %+v", len(got.Nodes), got.Nodes)
	}

	want := []IndexNode{
		{ID: "a", Type: "note", Title: "A", Path: "a.md"},
		{ID: "b", Type: "spec", Title: "B", Path: "specs/b.md"},
	}

	for idx, node := range want {
		if got.Nodes[idx] != node {
			test.Fatalf("node[%d] = %+v, want %+v", idx, got.Nodes[idx], node)
		}
	}
}

// TestIndexEmptyVaultMarshalsEmptyArray guards the nil-slice trap: IndexNode
// entries must marshal to "[]", not "null", when the vault has no file nodes —
// a nil slice would otherwise force every frontend consumer to null-check
// before iterating.
func TestIndexEmptyVaultMarshalsEmptyArray(test *testing.T) {
	srv := New(Deps{Root: test.TempDir(), Nodes: fakeNodes{}})

	rec := httptest.NewRecorder()
	srv.handleIndex(rec, httptest.NewRequest("GET", "/api/index", nil))

	if got := rec.Body.String(); got != `{"nodes":[]}`+"\n" {
		test.Fatalf("body=%q want %q", got, `{"nodes":[]}`+"\n")
	}
}
