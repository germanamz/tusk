package node_test

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/node"
)

// D2/C2: `edge add` on a node carrying a bare-title ref property must keep the
// ref's RESOLVED edge id in the index (not overwrite it with the raw title),
// and must leave the node's index row checksum consistent with the file on disk.
func TestAddEdge_PreservesResolvedRefEdgeAndNodeRow(test *testing.T) {
	dir := test.TempDir()
	idx, _ := index.Open(filepath.Join(dir, ".tusk", "index.db"))
	defer idx.Close()

	repo := index.NewNodeRepo(idx)
	edgeRepo := index.NewEdgeRepo(idx)

	nodeTypes := map[string]manifest.NodeType{
		"person": {},
		"ticket": {Properties: []manifest.PropertyDecl{{Name: "assignee", Type: "ref", To: "person"}}},
	}
	edgeTypes := manifest.EdgeTypes{
		"assignee": {From: []string{"ticket"}, To: []string{"person"}, Cardinality: manifest.CardinalityManyToOne},
		"blocks":   {From: []string{"ticket"}, To: []string{"ticket"}, Cardinality: manifest.CardinalityManyToMany, Acyclic: true},
	}

	service := node.NewServiceWithBehaviors(
		dir, repo, edgeRepo, edgeTypes, nil,
		nodeTypes, nil, nil, nil, nil,
		node.NewIndexRefLookup(repo),
		index.NewFileStateRepo(idx), "test-worker", time.Minute,
	)

	if _, err := service.Create(node.CreateInput{RelPath: "people/jane.md", Type: "person", Title: "Jane Doe"}); err != nil {
		test.Fatalf("create person: %v", err)
	}
	if _, err := service.Create(node.CreateInput{RelPath: "tickets/other.md", Type: "ticket", Title: "Other"}); err != nil {
		test.Fatalf("create other: %v", err)
	}
	// assignee authored as a bare title -> resolves to people/jane.
	if _, err := service.Create(node.CreateInput{
		RelPath: "tickets/auth.md", Type: "ticket", Title: "Auth",
		Properties: map[string]any{"assignee": "Jane Doe"},
	}); err != nil {
		test.Fatalf("create auth: %v", err)
	}

	assigneeTarget := func() string {
		rows, _ := edgeRepo.ListBySource("tickets/auth")
		for _, r := range rows {
			if r.Type == "assignee" {
				return r.TargetID
			}
		}
		return ""
	}

	if got := assigneeTarget(); got != "people/jane" {
		test.Fatalf("assignee edge before add = %q, want people/jane", got)
	}

	// Add an UNRELATED edge; must not corrupt the resolved assignee edge.
	if err := service.AddEdge("blocks", "tickets/auth", "tickets/other"); err != nil {
		test.Fatalf("AddEdge: %v", err)
	}

	if got := assigneeTarget(); got != "people/jane" {
		test.Errorf("assignee edge after add = %q, want people/jane (raw title clobbered resolved id)", got)
	}

	// C2: node row checksum must match the file on disk after the edge write.
	onDisk, _ := os.ReadFile(filepath.Join(dir, "tickets/auth.md"))
	sum := sha256.Sum256(onDisk)
	row, getErr := repo.Get("tickets/auth")
	if getErr != nil {
		test.Fatalf("Get node row: %v", getErr)
	}
	if row.LastChecksum != hex.EncodeToString(sum[:]) {
		test.Errorf("node row checksum stale after edge add:\n row = %s\n disk= %s", row.LastChecksum, hex.EncodeToString(sum[:]))
	}
}
