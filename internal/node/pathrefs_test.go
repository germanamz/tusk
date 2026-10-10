package node_test

import (
	"fmt"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/linenum"
	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/node"
)

func pathRefEdgeTypes() manifest.EdgeTypes {
	return manifest.EdgeTypes{
		"describes": {From: []string{"technical"}, Cardinality: manifest.CardinalityManyToMany, Paths: true},
		"related":   {From: []string{"*"}, To: []string{"*"}, Cardinality: manifest.CardinalityManyToMany},
	}
}

func newPathRefService(test *testing.T) (*node.Service, *index.NodeRepo, *index.EdgeRepo, *index.FileStateRepo, string) {
	test.Helper()

	root := test.TempDir()

	store, openErr := index.Open(filepath.Join(root, ".tusk", "index.db"))

	if openErr != nil {
		test.Fatalf("open index: %v", openErr)
	}

	test.Cleanup(func() { store.Close() })

	nodes := index.NewNodeRepo(store)
	edges := index.NewEdgeRepo(store)
	fileState := index.NewFileStateRepo(store)

	service := node.NewServiceWithDeps(node.ServiceDeps{
		WorkspaceRoot: root,
		Repo:          nodes,
		Edges:         edges,
		EdgeTypes:     pathRefEdgeTypes(),
		FileState:     fileState,
		WorkerID:      "test-worker",
		LeaseTTL:      time.Minute,
		LineNumbering: linenum.SchemeLF,
	})

	return service, nodes, edges, fileState, root
}

func pathRefLines(test *testing.T, edges *index.EdgeRepo, id string) []string {
	test.Helper()

	refs, listErr := edges.PathRefsFrom([]string{id})

	if listErr != nil {
		test.Fatalf("PathRefsFrom: %v", listErr)
	}

	rendered := make([]string, 0, len(refs))

	for _, ref := range refs {
		rendered = append(rendered, fmt.Sprintf("%s:%d", ref.Target, ref.Line))
	}

	return rendered
}

func TestService_WritesKeepPathRefsInStep(test *testing.T) {
	service, nodes, edges, fileState, root := newPathRefService(test)

	// type, title and the two delimiters put the body's first line at 5.
	if _, createErr := service.Create(node.CreateInput{
		RelPath: "technical/ledger.md",
		Type:    "technical",
		Title:   "Ledger",
		Body:    []byte("Lives in `server/ledger/service.go`.\n"),
	}); createErr != nil {
		test.Fatalf("Create: %v", createErr)
	}

	if _, createErr := service.Create(node.CreateInput{RelPath: "notes/other.md", Type: "note", Body: []byte("`server/x.go`\n")}); createErr != nil {
		test.Fatalf("Create note: %v", createErr)
	}

	created := pathRefLines(test, edges, "technical/ledger")

	if len(created) != 1 {
		test.Fatalf("after Create = %v, want one ref", created)
	}

	if got := pathRefLines(test, edges, "notes/other"); len(got) != 0 {
		test.Errorf("a note is outside describes' from, got %v", got)
	}

	if _, modifyErr := service.Modify(node.ModifyInput{ID: "technical/ledger", SetProps: map[string]any{"owner": "ledger-team"}}); modifyErr != nil {
		test.Fatalf("Modify: %v", modifyErr)
	}

	modified := pathRefLines(test, edges, "technical/ledger")

	if len(modified) != 1 || modified[0] == created[0] {
		test.Errorf("after a frontmatter Modify = %v, want the line moved from %v", modified, created)
	}

	if addErr := service.AddEdge("related", "technical/ledger", "notes/other"); addErr != nil {
		test.Fatalf("AddEdge: %v", addErr)
	}

	added := pathRefLines(test, edges, "technical/ledger")

	if len(added) != 1 || added[0] == modified[0] {
		test.Errorf("after AddEdge = %v, want the line moved from %v", added, modified)
	}

	if _, renameErr := node.Rename(
		root, nodes, edges, fileState, "test-worker", time.Minute,
		pathRefEdgeTypes(), nil, nil, "technical/ledger", "technical/ledger-core.md",
	); renameErr != nil {
		test.Fatalf("Rename: %v", renameErr)
	}

	if got := pathRefLines(test, edges, "technical/ledger-core"); !slices.Equal(got, added) {
		test.Errorf("after Rename = %v, want %v carried over", got, added)
	}

	if got := pathRefLines(test, edges, "technical/ledger"); len(got) != 0 {
		test.Errorf("old id still has refs: %v", got)
	}
}
