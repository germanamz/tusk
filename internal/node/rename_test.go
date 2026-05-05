package node_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/node"
)

func TestDelete_RemovesFileAndEdges(test *testing.T) {
	root := test.TempDir()

	store, _ := index.Open(filepath.Join(root, ".tusk", "index.db"))
	defer store.Close()

	nodeRepo := index.NewNodeRepo(store)
	edgeRepo := index.NewEdgeRepo(store)

	edgeTypes := manifest.EdgeTypes{
		"parent": manifest.EdgeType{From: []string{"ticket"}, To: []string{"ticket"}, Cardinality: manifest.CardinalityManyToOne},
	}

	service := node.NewServiceWithManifest(root, nodeRepo, edgeRepo, edgeTypes)

	if _, parentErr := service.Create(node.CreateInput{
		RelPath: "tickets/parent.md", Type: "ticket", Title: "Parent",
	}); parentErr != nil {
		test.Fatalf("create parent: %v", parentErr)
	}

	if _, childErr := service.Create(node.CreateInput{
		RelPath:    "tickets/child.md",
		Type:       "ticket",
		Title:      "Child",
		Properties: map[string]any{"parent": "tickets/parent"},
	}); childErr != nil {
		test.Fatalf("create child: %v", childErr)
	}

	if deleteErr := node.Delete(root, nodeRepo, edgeRepo, "tickets/child"); deleteErr != nil {
		test.Fatalf("Delete: %v", deleteErr)
	}

	if _, statErr := os.Stat(filepath.Join(root, "tickets/child.md")); !os.IsNotExist(statErr) {
		test.Errorf("expected file removed, got stat err = %v", statErr)
	}

	if _, getErr := nodeRepo.Get("tickets/child"); getErr != index.ErrNodeNotFound {
		test.Errorf("expected ErrNodeNotFound, got %v", getErr)
	}

	outgoing, _ := edgeRepo.ListBySource("tickets/child")

	if len(outgoing) != 0 {
		test.Errorf("expected zero outgoing edges, got %+v", outgoing)
	}
}

func TestDelete_ReturnsErrorWhenNodeNotFound(test *testing.T) {
	root := test.TempDir()

	store, _ := index.Open(filepath.Join(root, ".tusk", "index.db"))
	defer store.Close()

	nodeRepo := index.NewNodeRepo(store)
	edgeRepo := index.NewEdgeRepo(store)

	deleteErr := node.Delete(root, nodeRepo, edgeRepo, "tickets/missing")

	if deleteErr == nil {
		test.Fatalf("expected error for missing node")
	}
}

func TestRename_MovesFileAndRewritesReferringEdgesInFrontmatter(test *testing.T) {
	root := test.TempDir()

	store, _ := index.Open(filepath.Join(root, ".tusk", "index.db"))
	defer store.Close()

	nodeRepo := index.NewNodeRepo(store)
	edgeRepo := index.NewEdgeRepo(store)

	edgeTypes := manifest.EdgeTypes{
		"parent": manifest.EdgeType{From: []string{"ticket"}, To: []string{"ticket"}, Cardinality: manifest.CardinalityManyToOne},
	}

	service := node.NewServiceWithManifest(root, nodeRepo, edgeRepo, edgeTypes)

	if _, parentErr := service.Create(node.CreateInput{
		RelPath: "tickets/old-parent.md", Type: "ticket", Title: "Parent",
	}); parentErr != nil {
		test.Fatalf("create parent: %v", parentErr)
	}

	if _, childErr := service.Create(node.CreateInput{
		RelPath:    "tickets/child.md",
		Type:       "ticket",
		Title:      "Child",
		Properties: map[string]any{"parent": "tickets/old-parent"},
	}); childErr != nil {
		test.Fatalf("create child: %v", childErr)
	}

	plan, renameErr := node.Rename(root, nodeRepo, edgeRepo, edgeTypes, "tickets/old-parent", "tickets/new-parent.md")

	if renameErr != nil {
		test.Fatalf("Rename: %v", renameErr)
	}

	if plan.NewID != "tickets/new-parent" {
		test.Errorf("NewID = %q, want tickets/new-parent", plan.NewID)
	}

	if _, statErr := os.Stat(filepath.Join(root, "tickets/new-parent.md")); statErr != nil {
		test.Errorf("expected new file: %v", statErr)
	}

	if _, statErr := os.Stat(filepath.Join(root, "tickets/old-parent.md")); !os.IsNotExist(statErr) {
		test.Errorf("expected old file gone, stat err = %v", statErr)
	}

	if _, getErr := nodeRepo.Get("tickets/new-parent"); getErr != nil {
		test.Errorf("Get(new) = %v", getErr)
	}

	childEdges, _ := edgeRepo.ListBySource("tickets/child")

	found := false

	for _, edge := range childEdges {
		if edge.Type == "parent" && edge.TargetID == "tickets/new-parent" {
			found = true
		}
	}

	if !found {
		test.Errorf("child edge should now target tickets/new-parent: %+v", childEdges)
	}

	childContent, _ := os.ReadFile(filepath.Join(root, "tickets/child.md"))

	if !strings.Contains(string(childContent), "parent: tickets/new-parent") {
		test.Errorf("child frontmatter not rewritten:\n%s", string(childContent))
	}
}

func TestRename_ReturnsErrorWhenTargetExists(test *testing.T) {
	root := test.TempDir()

	store, _ := index.Open(filepath.Join(root, ".tusk", "index.db"))
	defer store.Close()

	nodeRepo := index.NewNodeRepo(store)
	edgeRepo := index.NewEdgeRepo(store)

	service := node.NewServiceWithManifest(root, nodeRepo, edgeRepo, manifest.EdgeTypes{})

	_, _ = service.Create(node.CreateInput{RelPath: "a.md", Type: "note"})
	_, _ = service.Create(node.CreateInput{RelPath: "b.md", Type: "note"})

	_, renameErr := node.Rename(root, nodeRepo, edgeRepo, manifest.EdgeTypes{}, "a", "b.md")

	if renameErr == nil {
		test.Fatalf("expected error renaming over existing target")
	}
}
