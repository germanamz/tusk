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

func TestEdgeRepo_NeighborsByEdgeTypes(test *testing.T) {
	repo := newTestEdgeRepo(test, "f1", "f2", "f3", "f4", "f5")

	// f1 -references-> f2
	if upsertErr := repo.UpsertAll("f1", "f1.md", []index.EdgeRow{
		{Type: "references", SourceID: "f1", TargetID: "f2", SourcePath: "f1.md"},
		{Type: "references", SourceID: "f1", TargetID: "f3", SourcePath: "f1.md"},
		{Type: "tagged", SourceID: "f1", TargetID: "f5", SourcePath: "f1.md"},
	}); upsertErr != nil {
		test.Fatalf("UpsertAll f1: %v", upsertErr)
	}

	// f3 -references-> f4
	if upsertErr := repo.UpsertAll("f3", "f3.md", []index.EdgeRow{
		{Type: "references", SourceID: "f3", TargetID: "f4", SourcePath: "f3.md"},
	}); upsertErr != nil {
		test.Fatalf("UpsertAll f3: %v", upsertErr)
	}

	// f4 -references-> f5
	if upsertErr := repo.UpsertAll("f4", "f4.md", []index.EdgeRow{
		{Type: "references", SourceID: "f4", TargetID: "f5", SourcePath: "f4.md"},
	}); upsertErr != nil {
		test.Fatalf("UpsertAll f4: %v", upsertErr)
	}

	// Seeds {f1, f3}, types ["references"]: should match
	// (f1,f2,references), (f1,f3,references), (f3,f4,references).
	// Not (f1,f5,tagged) [wrong type] and not (f4,f5,references)
	// [neither endpoint is in seeds].
	rows, queryErr := repo.NeighborsByEdgeTypes(
		[]string{"f1", "f3"},
		[]string{"references"},
	)

	if queryErr != nil {
		test.Fatalf("NeighborsByEdgeTypes: %v", queryErr)
	}

	if len(rows) != 3 {
		test.Fatalf("len = %d, want 3 (rows=%+v)", len(rows), rows)
	}

	matched := map[string]bool{}

	for _, row := range rows {
		matched[row.Type+"|"+row.SourceID+"|"+row.TargetID] = true
	}

	for _, want := range []string{
		"references|f1|f2",
		"references|f1|f3",
		"references|f3|f4",
	} {
		if !matched[want] {
			test.Errorf("missing %q in %+v", want, rows)
		}
	}

	// Add tagged so seeds {f1} with types ["references","tagged"] returns
	// the tagged edge too.
	multi, multiErr := repo.NeighborsByEdgeTypes(
		[]string{"f1"},
		[]string{"references", "tagged"},
	)

	if multiErr != nil {
		test.Fatalf("NeighborsByEdgeTypes multi: %v", multiErr)
	}

	if len(multi) != 3 {
		test.Fatalf("multi len = %d, want 3 (rows=%+v)", len(multi), multi)
	}
}

func TestEdgeRepo_NeighborsByEdgeTypes_EmptyInputs(test *testing.T) {
	repo := newTestEdgeRepo(test, "x")

	rows, queryErr := repo.NeighborsByEdgeTypes(nil, []string{"references"})

	if queryErr != nil {
		test.Fatalf("nil sources: %v", queryErr)
	}

	if len(rows) != 0 {
		test.Errorf("nil sources rows = %+v, want empty", rows)
	}

	rows, queryErr = repo.NeighborsByEdgeTypes([]string{"x"}, nil)

	if queryErr != nil {
		test.Fatalf("nil types: %v", queryErr)
	}

	if len(rows) != 0 {
		test.Errorf("nil types rows = %+v, want empty", rows)
	}
}

func TestEdgeRepo_NeighborsByEdgeTypes_UnknownEdgeType(test *testing.T) {
	repo := newTestEdgeRepo(test, "a", "b")

	repo.UpsertAll("a", "a.md", []index.EdgeRow{
		{Type: "references", SourceID: "a", TargetID: "b", SourcePath: "a.md"},
	})

	rows, queryErr := repo.NeighborsByEdgeTypes(
		[]string{"a"},
		[]string{"does-not-exist"},
	)

	if queryErr != nil {
		test.Fatalf("NeighborsByEdgeTypes: %v", queryErr)
	}

	if len(rows) != 0 {
		test.Errorf("unknown type rows = %+v, want empty", rows)
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
