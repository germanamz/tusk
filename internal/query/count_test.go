package query_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/query"
)

// seedServices upserts four service rows and one note: domains billing,
// billing, checkout and missing; products [a, b], [a], [] and missing.
func seedServices(test *testing.T, store *index.Index) {
	test.Helper()

	nodes := index.NewNodeRepo(store)

	for _, row := range []index.NodeRow{
		{ID: "svc/a", Type: "service", PropertiesJSON: `{"domain":"billing","products":["a","b"]}`},
		{ID: "svc/b", Type: "service", PropertiesJSON: `{"domain":"billing","products":["a","a"]}`},
		{ID: "svc/c", Type: "service", PropertiesJSON: `{"domain":"checkout","products":[]}`},
		{ID: "svc/d", Type: "service", PropertiesJSON: `{}`},
		{ID: "notes/n", Type: "note", PropertiesJSON: `{}`},
	} {
		row.Path = row.ID + ".md"
		row.LastChecksum = "x"

		if upsertErr := nodes.Upsert(row); upsertErr != nil {
			test.Fatalf("upsert %s: %v", row.ID, upsertErr)
		}
	}
}

func countDeps(test *testing.T) query.Deps {
	test.Helper()

	store := openTestStore(test)
	seedServices(test, store)

	return query.Deps{Database: store.DB(), Manifest: &manifest.Manifest{}}
}

// TestQueryRun_CountIgnoresWindow pins #851: a count is the total of every
// match, whatever take, skip or the default page size say.
func TestQueryRun_CountIgnoresWindow(test *testing.T) {
	result, runErr := query.Run(context.Background(), countDeps(test), query.Request{
		Filter:                "type=service",
		Count:                 true,
		Take:                  1,
		Skip:                  1,
		Sort:                  "-id",
		StructuralDefaultTake: 2,
	})

	if runErr != nil {
		test.Fatalf("Run: %v", runErr)
	}

	if result.Count == nil {
		test.Fatal("Run(Count) returned no Count")
	}

	if result.Count.Total != 4 || len(result.Count.Groups) != 0 {
		test.Errorf("Count = %+v, want total 4 and no groups", result.Count)
	}

	encoded, marshalErr := json.Marshal(result.Count)

	if marshalErr != nil {
		test.Fatalf("marshal: %v", marshalErr)
	}

	if got, want := string(encoded), `{"count":4}`; got != want {
		test.Errorf("JSON = %s, want %s", got, want)
	}
}

func TestQueryRun_GroupByType(test *testing.T) {
	result, runErr := query.Run(context.Background(), countDeps(test), query.Request{GroupBy: "type"})

	if runErr != nil {
		test.Fatalf("Run: %v", runErr)
	}

	encoded, marshalErr := json.Marshal(result.Count)

	if marshalErr != nil {
		test.Fatalf("marshal: %v", marshalErr)
	}

	want := `{"count":5,"group_by":"type","groups":[{"value":"service","count":4},{"value":"note","count":1}]}`

	if string(encoded) != want {
		test.Errorf("JSON = %s, want %s", encoded, want)
	}
}

// TestQueryRun_GroupByProperty covers a scalar property (a missing value is
// its own null group, sorted last on a tie) and a list property, which counts
// a node once per distinct item and puts an empty list with the missing ones.
func TestQueryRun_GroupByProperty(test *testing.T) {
	deps := countDeps(test)

	scalar, scalarErr := query.Run(context.Background(), deps, query.Request{Filter: "type=service", GroupBy: "domain"})

	if scalarErr != nil {
		test.Fatalf("Run(domain): %v", scalarErr)
	}

	encoded, marshalErr := json.Marshal(scalar.Count)

	if marshalErr != nil {
		test.Fatalf("marshal: %v", marshalErr)
	}

	want := `{"count":4,"group_by":"domain","groups":[{"value":"billing","count":2},{"value":"checkout","count":1},{"value":null,"count":1}]}`

	if string(encoded) != want {
		test.Errorf("domain JSON = %s, want %s", encoded, want)
	}

	list, listErr := query.Run(context.Background(), deps, query.Request{Filter: "type=service", GroupBy: "products"})

	if listErr != nil {
		test.Fatalf("Run(products): %v", listErr)
	}

	listEncoded, listMarshalErr := json.Marshal(list.Count)

	if listMarshalErr != nil {
		test.Fatalf("marshal: %v", listMarshalErr)
	}

	listWant := `{"count":4,"group_by":"products","groups":[{"value":"a","count":2},{"value":null,"count":2},{"value":"b","count":1}]}`

	if string(listEncoded) != listWant {
		test.Errorf("products JSON = %s, want %s", listEncoded, listWant)
	}
}

func TestQueryRun_GroupByEmptyMatchKeepsGroupsArray(test *testing.T) {
	result, runErr := query.Run(context.Background(), countDeps(test), query.Request{Filter: "type=ticket", GroupBy: "domain"})

	if runErr != nil {
		test.Fatalf("Run: %v", runErr)
	}

	encoded, marshalErr := json.Marshal(result.Count)

	if marshalErr != nil {
		test.Fatalf("marshal: %v", marshalErr)
	}

	if got, want := string(encoded), `{"count":0,"group_by":"domain","groups":[]}`; got != want {
		test.Errorf("JSON = %s, want %s", got, want)
	}
}

func TestQueryRun_CountRejects(test *testing.T) {
	deps := countDeps(test)

	for _, tc := range []struct {
		name    string
		request query.Request
		want    string
	}{
		{"semantic", query.Request{Count: true, Semantic: "billing"}, "semantic"},
		{"unique key", query.Request{GroupBy: "id"}, `"id"`},
		{"bad fields", query.Request{Count: true, Fields: []string{"nope"}}, `"nope"`},
	} {
		_, runErr := query.Run(context.Background(), deps, tc.request)

		if runErr == nil || !strings.Contains(runErr.Error(), tc.want) {
			test.Errorf("%s: Run error = %v, want one mentioning %s", tc.name, runErr, tc.want)
		}
	}
}

// TestQueryRun_RejectsUnknownFields pins the shared check: every read path
// validates fields, whatever format renders the rows.
func TestQueryRun_RejectsUnknownFields(test *testing.T) {
	deps := countDeps(test)

	if _, runErr := query.Run(context.Background(), deps, query.Request{Fields: []string{"titel"}}); runErr == nil {
		test.Error("Run(fields=titel) = nil error")
	}

	if _, listErr := query.ListRun(deps.Database, deps.Manifest, query.ListRequest{Fields: []string{"titel"}}); listErr == nil {
		test.Error("ListRun(fields=titel) = nil error")
	}

	result, runErr := query.Run(context.Background(), deps, query.Request{Filter: "id=svc/a", Fields: []string{"id", "properties.domain"}})

	if runErr != nil {
		test.Fatalf("Run: %v", runErr)
	}

	if len(result.Rows) != 1 || result.Rows[0].Properties["domain"] != "billing" {
		test.Errorf("properties.domain did not load properties: %+v", result.Rows)
	}

	if !result.Fields.Any() {
		test.Error("Result.Fields is empty, want the parsed projection")
	}
}
