package bookview

import (
	"database/sql"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/germanamz/tusk/internal/index"
)

// TestIndexListsFileNodes pins GET /api/index's payload: every file-level row
// from NodeSource.ListFileNodes, each carrying its ParentID through as Parent
// (empty when NULL) so the client can offer hierarchy grouping among files.
func TestIndexListsFileNodes(test *testing.T) {
	nodes := fakeNodes{file: []index.NodeRow{
		{ID: "a", Type: "note", Title: "A", Path: "a.md"},
		{ID: "b", Type: "spec", Title: "B", Path: "b.md", ParentID: sql.NullString{String: "a", Valid: true}},
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

	if got.Nodes[0].ID != "a" || got.Nodes[0].Parent != "" {
		test.Fatalf("root node = %+v, want id=a parent=\"\"", got.Nodes[0])
	}

	if got.Nodes[1].ID != "b" || got.Nodes[1].Parent != "a" {
		test.Fatalf("child node = %+v, want id=b parent=a", got.Nodes[1])
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
