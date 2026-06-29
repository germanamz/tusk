package node_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/node"
)

// TestServiceAddEdge_UndeclaredReturnsSentinel pins the C3 contract that the MCP
// reload hint depends on: AddEdge rejects an undeclared edge type with the
// ErrEdgeTypeNotDeclared sentinel (matchable via errors.Is), and the message
// still names the type. The declared-type check happens before any repo access,
// so a minimal service suffices.
func TestServiceAddEdge_UndeclaredReturnsSentinel(test *testing.T) {
	service := node.NewServiceWithManifest(test.TempDir(), nil, nil, manifest.EdgeTypes{})

	addErr := service.AddEdge("blocks", "tickets/a", "tickets/b")

	if !errors.Is(addErr, node.ErrEdgeTypeNotDeclared) {
		test.Fatalf("AddEdge err = %v, want wrap of ErrEdgeTypeNotDeclared", addErr)
	}

	if got := addErr.Error(); got != `edge type "blocks" not declared in manifest` {
		test.Errorf("AddEdge err = %q, want it to name the edge type", got)
	}
}

// TestServiceRemoveEdge_UndeclaredReturnsSentinel mirrors the above for RemoveEdge.
func TestServiceRemoveEdge_UndeclaredReturnsSentinel(test *testing.T) {
	service := node.NewServiceWithManifest(test.TempDir(), nil, nil, manifest.EdgeTypes{})

	removeErr := service.RemoveEdge("blocks", "tickets/a", "tickets/b")

	if !errors.Is(removeErr, node.ErrEdgeTypeNotDeclared) {
		test.Fatalf("RemoveEdge err = %v, want wrap of ErrEdgeTypeNotDeclared", removeErr)
	}
}

func TestAddEdgeToFrontmatter_AddsKeyToScalar(test *testing.T) {
	dir := test.TempDir()
	sourcePath := filepath.Join(dir, "src.md")

	if writeErr := os.WriteFile(sourcePath, []byte("---\ntype: ticket\ntitle: T\n---\nbody\n"), 0o644); writeErr != nil {
		test.Fatalf("seed: %v", writeErr)
	}

	edgeTypes := manifest.EdgeTypes{
		"blocks": manifest.EdgeType{
			From: []string{"ticket"}, To: []string{"ticket"},
			Cardinality: manifest.CardinalityManyToMany,
		},
	}

	if err := node.AddEdgeToFrontmatter(dir, "src", "blocks", "tickets/x", edgeTypes); err != nil {
		test.Fatalf("AddEdgeToFrontmatter: %v", err)
	}

	body, _ := os.ReadFile(sourcePath)

	parsed, parseErr := node.ParseFile("src.md", body)

	if parseErr != nil {
		test.Fatalf("reparse: %v", parseErr)
	}

	if value, ok := parsed.Properties["blocks"]; !ok || value != "tickets/x" {
		test.Errorf("expected blocks: tickets/x, got %v", parsed.Properties["blocks"])
	}
}

func TestAddEdgeToFrontmatter_AppendsToListWhenMultiTarget(test *testing.T) {
	// blocks is many-to-many; second add appends.
	dir := test.TempDir()
	sourcePath := filepath.Join(dir, "src.md")

	if writeErr := os.WriteFile(sourcePath, []byte("---\ntype: ticket\nblocks: tickets/x\n---\n"), 0o644); writeErr != nil {
		test.Fatalf("seed: %v", writeErr)
	}

	edgeTypes := manifest.EdgeTypes{
		"blocks": manifest.EdgeType{
			From: []string{"ticket"}, To: []string{"ticket"},
			Cardinality: manifest.CardinalityManyToMany,
		},
	}

	if err := node.AddEdgeToFrontmatter(dir, "src", "blocks", "tickets/y", edgeTypes); err != nil {
		test.Fatalf("AddEdgeToFrontmatter: %v", err)
	}

	body, _ := os.ReadFile(sourcePath)
	parsed, _ := node.ParseFile("src.md", body)

	list, ok := parsed.Properties["blocks"].([]any)

	if !ok {
		test.Fatalf("expected list, got %T (%v)", parsed.Properties["blocks"], parsed.Properties["blocks"])
	}

	if len(list) != 2 || list[0] != "tickets/x" || list[1] != "tickets/y" {
		test.Errorf("expected [tickets/x tickets/y], got %v", list)
	}
}

func TestAddEdgeToFrontmatter_NoOpWhenTargetAlreadyPresent(test *testing.T) {
	dir := test.TempDir()
	sourcePath := filepath.Join(dir, "src.md")

	if writeErr := os.WriteFile(sourcePath, []byte("---\ntype: ticket\nblocks: tickets/x\n---\n"), 0o644); writeErr != nil {
		test.Fatalf("seed: %v", writeErr)
	}

	edgeTypes := manifest.EdgeTypes{
		"blocks": manifest.EdgeType{
			From: []string{"ticket"}, To: []string{"ticket"},
			Cardinality: manifest.CardinalityManyToMany,
		},
	}

	if err := node.AddEdgeToFrontmatter(dir, "src", "blocks", "tickets/x", edgeTypes); err != nil {
		test.Errorf("idempotent re-add should be a no-op, got: %v", err)
	}
}

func TestAddEdgeToFrontmatter_RejectsConflictingSingleTarget(test *testing.T) {
	dir := test.TempDir()
	sourcePath := filepath.Join(dir, "src.md")

	if writeErr := os.WriteFile(sourcePath, []byte("---\ntype: child\nparent: parents/a\n---\n"), 0o644); writeErr != nil {
		test.Fatalf("seed: %v", writeErr)
	}

	edgeTypes := manifest.EdgeTypes{
		"parent": manifest.EdgeType{
			From: []string{"child"}, To: []string{"parents"},
			Cardinality: manifest.CardinalityManyToOne,
		},
	}

	err := node.AddEdgeToFrontmatter(dir, "src", "parent", "parents/b", edgeTypes)

	if err == nil {
		test.Fatalf("expected error: single-target conflict")
	}
}

func TestAddEdgeToFrontmatter_RejectsMissingSourceFile(test *testing.T) {
	dir := test.TempDir()
	edgeTypes := manifest.EdgeTypes{
		"blocks": manifest.EdgeType{
			From: []string{"ticket"}, To: []string{"ticket"},
			Cardinality: manifest.CardinalityManyToMany,
		},
	}

	if err := node.AddEdgeToFrontmatter(dir, "missing", "blocks", "tickets/x", edgeTypes); err == nil {
		test.Fatalf("expected error: source file does not exist")
	}
}

func TestRemoveEdgeFromFrontmatter_RemovesFromList(test *testing.T) {
	dir := test.TempDir()
	sourcePath := filepath.Join(dir, "src.md")

	if writeErr := os.WriteFile(sourcePath, []byte("---\ntype: ticket\nblocks:\n  - tickets/x\n  - tickets/y\n---\n"), 0o644); writeErr != nil {
		test.Fatalf("seed: %v", writeErr)
	}

	edgeTypes := manifest.EdgeTypes{
		"blocks": manifest.EdgeType{
			From: []string{"ticket"}, To: []string{"ticket"},
			Cardinality: manifest.CardinalityManyToMany,
		},
	}

	if err := node.RemoveEdgeFromFrontmatter(dir, "src", "blocks", "tickets/x", edgeTypes); err != nil {
		test.Fatalf("RemoveEdgeFromFrontmatter: %v", err)
	}

	body, _ := os.ReadFile(sourcePath)
	parsed, _ := node.ParseFile("src.md", body)

	value, ok := parsed.Properties["blocks"]

	if !ok {
		test.Fatalf("blocks key should still exist (other target remains)")
	}

	if value != "tickets/y" && !slicesEqual(value, []any{"tickets/y"}) {
		test.Errorf("expected tickets/y to remain, got %v", value)
	}
}

func TestRemoveEdgeFromFrontmatter_RemovesKeyWhenLastTarget(test *testing.T) {
	dir := test.TempDir()
	sourcePath := filepath.Join(dir, "src.md")

	if writeErr := os.WriteFile(sourcePath, []byte("---\ntype: ticket\nblocks: tickets/x\n---\n"), 0o644); writeErr != nil {
		test.Fatalf("seed: %v", writeErr)
	}

	edgeTypes := manifest.EdgeTypes{
		"blocks": manifest.EdgeType{
			From: []string{"ticket"}, To: []string{"ticket"},
			Cardinality: manifest.CardinalityManyToMany,
		},
	}

	if err := node.RemoveEdgeFromFrontmatter(dir, "src", "blocks", "tickets/x", edgeTypes); err != nil {
		test.Fatalf("RemoveEdgeFromFrontmatter: %v", err)
	}

	body, _ := os.ReadFile(sourcePath)
	parsed, _ := node.ParseFile("src.md", body)

	if _, ok := parsed.Properties["blocks"]; ok {
		test.Errorf("blocks key should have been dropped when last target removed")
	}
}

func TestRemoveEdgeFromFrontmatter_NoOpWhenAbsent(test *testing.T) {
	dir := test.TempDir()
	sourcePath := filepath.Join(dir, "src.md")

	if writeErr := os.WriteFile(sourcePath, []byte("---\ntype: ticket\n---\n"), 0o644); writeErr != nil {
		test.Fatalf("seed: %v", writeErr)
	}

	edgeTypes := manifest.EdgeTypes{
		"blocks": manifest.EdgeType{
			From: []string{"ticket"}, To: []string{"ticket"},
			Cardinality: manifest.CardinalityManyToMany,
		},
	}

	if err := node.RemoveEdgeFromFrontmatter(dir, "src", "blocks", "tickets/x", edgeTypes); err != nil {
		test.Errorf("idempotent no-op should not error: %v", err)
	}
}

func slicesEqual(value any, expected []any) bool {
	list, ok := value.([]any)

	if !ok {
		return false
	}

	if len(list) != len(expected) {
		return false
	}

	for index := range list {
		if list[index] != expected[index] {
			return false
		}
	}

	return true
}
