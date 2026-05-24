package index_test

import (
	"testing"

	"github.com/germanamz/tusk/internal/index"
)

// newTestEdgeRepo opens a fresh index, seeds the provided node ids
// (required since the P2 migration added a foreign key from
// edges.source_id to nodes.id), and returns an EdgeRepo against it.
func newTestEdgeRepo(test *testing.T, nodeIDs ...string) *index.EdgeRepo {
	test.Helper()

	store := openTestIndex(test)

	if len(nodeIDs) > 0 {
		seedNodes(test, store, nodeIDs...)
	}

	return index.NewEdgeRepo(store)
}

func TestEdgeRepo_UpsertAllAndListBySource(test *testing.T) {
	repo := newTestEdgeRepo(test, "tickets/foo")

	edges := []index.EdgeRow{
		{Type: "parent", SourceID: "tickets/foo", TargetID: "tickets/epic", SourcePath: "tickets/foo.md"},
		{Type: "blocks", SourceID: "tickets/foo", TargetID: "tickets/bar", SourcePath: "tickets/foo.md"},
	}

	if upsertErr := repo.UpsertAll("tickets/foo", "tickets/foo.md", edges); upsertErr != nil {
		test.Fatalf("UpsertAll: %v", upsertErr)
	}

	listed, listErr := repo.ListBySource("tickets/foo")

	if listErr != nil {
		test.Fatalf("ListBySource: %v", listErr)
	}

	if len(listed) != 2 {
		test.Errorf("len = %d, want 2", len(listed))
	}

	triples := map[string]bool{}

	for _, row := range listed {
		triples[row.Type+"|"+row.SourceID+"|"+row.TargetID] = true
	}

	if !triples["parent|tickets/foo|tickets/epic"] || !triples["blocks|tickets/foo|tickets/bar"] {
		test.Errorf("missing expected triples in listed = %+v", listed)
	}
}

func TestEdgeRepo_UpsertAllReplacesExistingEdgesForSource(test *testing.T) {
	repo := newTestEdgeRepo(test, "x")

	first := []index.EdgeRow{
		{Type: "parent", SourceID: "x", TargetID: "y", SourcePath: "x.md"},
		{Type: "blocks", SourceID: "x", TargetID: "z", SourcePath: "x.md"},
	}

	repo.UpsertAll("x", "x.md", first)

	second := []index.EdgeRow{
		{Type: "parent", SourceID: "x", TargetID: "y2", SourcePath: "x.md"},
	}

	if upsertErr := repo.UpsertAll("x", "x.md", second); upsertErr != nil {
		test.Fatalf("second UpsertAll: %v", upsertErr)
	}

	listed, _ := repo.ListBySource("x")

	if len(listed) != 1 {
		test.Errorf("len = %d, want 1 after replace", len(listed))
	}

	if listed[0].TargetID != "y2" {
		test.Errorf("Target = %q, want y2", listed[0].TargetID)
	}
}

func TestEdgeRepo_ListByTarget(test *testing.T) {
	repo := newTestEdgeRepo(test, "a", "b")

	repo.UpsertAll("a", "a.md", []index.EdgeRow{
		{Type: "blocks", SourceID: "a", TargetID: "z", SourcePath: "a.md"},
	})

	repo.UpsertAll("b", "b.md", []index.EdgeRow{
		{Type: "blocks", SourceID: "b", TargetID: "z", SourcePath: "b.md"},
	})

	listed, listErr := repo.ListByTarget("z")

	if listErr != nil {
		test.Fatalf("ListByTarget: %v", listErr)
	}

	if len(listed) != 2 {
		test.Errorf("len = %d, want 2", len(listed))
	}
}

func TestEdgeRepo_ListByType(test *testing.T) {
	repo := newTestEdgeRepo(test, "a")

	repo.UpsertAll("a", "a.md", []index.EdgeRow{
		{Type: "blocks", SourceID: "a", TargetID: "x", SourcePath: "a.md"},
		{Type: "parent", SourceID: "a", TargetID: "y", SourcePath: "a.md"},
	})

	listed, listErr := repo.ListByType("blocks")

	if listErr != nil {
		test.Fatalf("ListByType: %v", listErr)
	}

	if len(listed) != 1 || listed[0].TargetID != "x" {
		test.Errorf("listed = %+v", listed)
	}
}

func TestEdgeRepo_DeleteBySource(test *testing.T) {
	repo := newTestEdgeRepo(test, "doomed")

	repo.UpsertAll("doomed", "doomed.md", []index.EdgeRow{
		{Type: "parent", SourceID: "doomed", TargetID: "x", SourcePath: "doomed.md"},
	})

	if deleteErr := repo.DeleteBySource("doomed"); deleteErr != nil {
		test.Fatalf("DeleteBySource: %v", deleteErr)
	}

	listed, _ := repo.ListBySource("doomed")

	if len(listed) != 0 {
		test.Errorf("len = %d, want 0", len(listed))
	}
}
