package node_test

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/node"
)

// #670 (D1): modifying an unrelated scalar property must NOT drop the node's
// frontmatter-materialized edges from either the file or the index. Covers a
// single-target (parent) and a multi-target (blocks) edge.
func TestServiceModify_PreservesFrontmatterEdges(test *testing.T) {
	dir := test.TempDir()
	idx, _ := index.Open(filepath.Join(dir, ".tusk", "index.db"))
	defer idx.Close()

	repo := index.NewNodeRepo(idx)
	edgeRepo := index.NewEdgeRepo(idx)

	nodeTypes := map[string]manifest.NodeType{
		"ticket": {Properties: []manifest.PropertyDecl{{Name: "priority", Type: "string"}}},
	}
	edgeTypes := manifest.EdgeTypes{
		"parent": {From: []string{"ticket"}, To: []string{"ticket"}, Cardinality: manifest.CardinalityManyToOne},
		"blocks": {From: []string{"ticket"}, To: []string{"ticket"}, Cardinality: manifest.CardinalityManyToMany, Acyclic: true},
	}

	service := node.NewServiceWithBehaviors(
		dir, repo, edgeRepo, edgeTypes, nil,
		nodeTypes, nil, nil, nil, nil,
		node.NewIndexRefLookup(repo),
		index.NewFileStateRepo(idx), "test-worker", time.Minute,
	)

	for _, id := range []string{"a", "b"} {
		if _, err := service.Create(node.CreateInput{RelPath: "tickets/" + id + ".md", Type: "ticket", Title: id}); err != nil {
			test.Fatalf("create %s: %v", id, err)
		}
	}

	if _, err := service.Create(node.CreateInput{
		RelPath: "tickets/child.md",
		Type:    "ticket",
		Title:   "Child",
		Properties: map[string]any{
			"priority": "medium",
			"parent":   "tickets/a",
			"blocks":   []any{"tickets/a", "tickets/b"},
		},
	}); err != nil {
		test.Fatalf("create child: %v", err)
	}

	edgeKey := func() []string {
		rows, _ := edgeRepo.ListBySource("tickets/child")
		var out []string
		for _, r := range rows {
			out = append(out, r.Type+"->"+r.TargetID)
		}
		sort.Strings(out)
		return out
	}

	before := edgeKey()
	wantEdges := []string{"blocks->tickets/a", "blocks->tickets/b", "parent->tickets/a"}
	if !equalStrings(before, wantEdges) {
		test.Fatalf("edges before modify = %v, want %v", before, wantEdges)
	}

	// Modify an UNRELATED scalar property.
	if _, err := service.Modify(node.ModifyInput{
		ID:       "tickets/child",
		SetProps: map[string]any{"priority": "high"},
	}); err != nil {
		test.Fatalf("Modify: %v", err)
	}

	if after := edgeKey(); !equalStrings(after, wantEdges) {
		test.Errorf("edges after modify = %v, want %v (edges dropped from index)", after, wantEdges)
	}

	onDisk, _ := os.ReadFile(filepath.Join(dir, "tickets/child.md"))
	if !strings.Contains(string(onDisk), "parent:") || !strings.Contains(string(onDisk), "blocks:") {
		test.Errorf("edge keys missing from file after modify:\n%s", onDisk)
	}
	if !strings.Contains(string(onDisk), "priority: high") {
		test.Errorf("modified property not written:\n%s", onDisk)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
