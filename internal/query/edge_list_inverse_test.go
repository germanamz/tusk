package query_test

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/query"
	"github.com/germanamz/tusk/internal/typeref"
)

// TestEdgeListRun_InverseAndUndeclaredTypes pins #850 for edge listings: an
// inverse label lists the edges of the type it names with From and To swapped,
// an undeclared type with nothing to list is an error rather than an empty
// table, and edges of a type tusk.toml no longer declares still list.
func TestEdgeListRun_InverseAndUndeclaredTypes(test *testing.T) {
	store, openErr := index.Open(filepath.Join(test.TempDir(), "index.db"))

	if openErr != nil {
		test.Fatalf("open index: %v", openErr)
	}

	defer store.Close()

	nodes := index.NewNodeRepo(store)
	edges := index.NewEdgeRepo(store)

	for _, id := range []string{"svc/a", "svc/b", "svc/c"} {
		if upsertErr := nodes.Upsert(index.NodeRow{ID: id, Type: "service", Path: id + ".md", Title: id, PropertiesJSON: `{}`}); upsertErr != nil {
			test.Fatalf("upsert %s: %v", id, upsertErr)
		}
	}

	links := map[string][]index.EdgeRow{
		"svc/a": {{Type: "depends-on", SourceID: "svc/a", TargetID: "svc/b", SourcePath: "svc/a.md", Kind: "direct"}},
		"svc/b": {
			{Type: "depends-on", SourceID: "svc/b", TargetID: "svc/c", SourcePath: "svc/b.md", Kind: "direct"},
			{Type: "retired", SourceID: "svc/b", TargetID: "svc/a", SourcePath: "svc/b.md", Kind: "direct"},
		},
	}

	for source, rows := range links {
		if upsertErr := edges.UpsertAll(source, source+".md", rows); upsertErr != nil {
			test.Fatalf("edges of %s: %v", source, upsertErr)
		}
	}

	edgeTypes := manifest.EdgeTypes{
		"depends-on": {From: []string{"*"}, To: []string{"*"}, Cardinality: manifest.CardinalityManyToMany, Inverse: "depended-on-by"},
	}

	userScoped := typeref.Ref{Scope: typeref.ScopeAny, Type: "depended-on-by"}

	cases := []struct {
		name    string
		request index.EdgeListRequest
		want    []string
		wantErr bool
	}{
		{"inverse_from", index.EdgeListRequest{Type: "depended-on-by", From: "svc/c"}, []string{"svc/b depends-on svc/c"}, false},
		{"inverse_to", index.EdgeListRequest{Type: "depended-on-by", To: "svc/a"}, []string{"svc/a depends-on svc/b"}, false},
		{"inverse_all", index.EdgeListRequest{Type: "depended-on-by"}, []string{"svc/a depends-on svc/b", "svc/b depends-on svc/c"}, false},
		{"inverse_typeref", index.EdgeListRequest{TypeRef: &userScoped, From: "svc/b"}, []string{"svc/a depends-on svc/b"}, false},
		{"forward", index.EdgeListRequest{Type: "depends-on", From: "svc/a"}, []string{"svc/a depends-on svc/b"}, false},
		{"dropped_type_still_lists", index.EdgeListRequest{Type: "retired"}, []string{"svc/b retired svc/a"}, false},
		{"undeclared", index.EdgeListRequest{Type: "depends"}, nil, true},
		{"undeclared_from", index.EdgeListRequest{Type: "retired", From: "svc/a"}, nil, true},
	}

	for _, testCase := range cases {
		test.Run(testCase.name, func(subtest *testing.T) {
			result, runErr := query.EdgeListRun(edges, nodes, query.EdgeListRequest{EdgeListRequest: testCase.request, EdgeTypes: edgeTypes})

			if testCase.wantErr {
				if !errors.Is(runErr, manifest.ErrEdgeTypeNotDeclared) {
					subtest.Fatalf("err = %v, want ErrEdgeTypeNotDeclared", runErr)
				}

				return
			}

			if runErr != nil {
				subtest.Fatalf("EdgeListRun: %v", runErr)
			}

			got := make([]string, 0, len(result.Rows))

			for _, row := range result.Rows {
				got = append(got, row.SourceID+" "+row.Type+" "+row.TargetID)
			}

			if len(got) != len(testCase.want) {
				subtest.Fatalf("rows = %v, want %v", got, testCase.want)
			}

			for position := range got {
				if got[position] != testCase.want[position] {
					subtest.Fatalf("rows = %v, want %v", got, testCase.want)
				}
			}
		})
	}

	// Without the manifest's edge types the listing behaves as before.
	if result, runErr := query.EdgeListRun(edges, nodes, query.EdgeListRequest{EdgeListRequest: index.EdgeListRequest{Type: "depends"}}); runErr != nil || len(result.Rows) != 0 {
		test.Errorf("no EdgeTypes: rows %v err %v, want an empty listing", result, runErr)
	}
}
