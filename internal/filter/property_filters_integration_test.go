package filter_test

import (
	"database/sql"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/germanamz/tusk/internal/filter"
	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/manifest"
)

// propertyFiltersNow is the fixed clock for the relative-date cases: 3 pm on
// 2026-10-27 at UTC-5, so local midnight is 05:00Z and now is 20:00Z.
var propertyFiltersNow = time.Date(2026, 10, 27, 15, 0, 0, 0, time.FixedZone("test", -5*60*60))

// propertyFiltersManifest declares a service type with an enum, two lists, a
// date, a datetime and a string. tags, mood and flag stay undeclared.
func propertyFiltersManifest() manifest.Manifest {
	return manifest.Manifest{
		NodeTypes: map[string]manifest.NodeType{
			"service": {Properties: []manifest.PropertyDecl{
				{Name: "domain", Type: "enum", Values: []string{"billing", "checkout"}},
				{Name: "products", Type: "list-of", ItemType: "enum", Values: []string{"a", "b", "c"}},
				{Name: "scores", Type: "list-of", ItemType: "int"},
				{Name: "checked", Type: "date"},
				{Name: "seen", Type: "datetime"},
				{Name: "owner", Type: "string"},
			}},
		},
	}
}

// TestPropertyFilters_EndToEndAgainstIndex runs parse → validate → compile →
// query against a real index for issue #846: NOT and != as exact complements,
// `*` presence, list membership (~= and [a, b]) and relative dates.
func TestPropertyFilters_EndToEndAgainstIndex(test *testing.T) {
	store, openErr := index.Open(filepath.Join(test.TempDir(), "index.db"))

	if openErr != nil {
		test.Fatalf("index.Open: %v", openErr)
	}

	defer store.Close()

	repo := index.NewNodeRepo(store)

	fixtures := []index.NodeRow{
		{ID: "s/a", Type: "service", Path: "s/a.md", Title: "A", PropertiesJSON: `{"domain":"billing","products":["a","b","c"],"scores":[1,5],"checked":"2026-07-01","seen":"2026-10-27T09:00:00Z","tags":["x","y"],"owner":"ann"}`, LastChecksum: "a"},
		{ID: "s/b", Type: "service", Path: "s/b.md", Title: "B", PropertiesJSON: `{"domain":"checkout","products":["b"],"scores":[],"checked":"2026-10-20","seen":"2026-10-25T12:00:00Z","tags":"x","owner":""}`, LastChecksum: "b"},
		{ID: "s/c", Type: "service", Path: "s/c.md", Title: "", PropertiesJSON: `{}`, LastChecksum: "c"},
		{ID: "s/d", Type: "service", Path: "s/d.md", Title: "D", PropertiesJSON: `{"domain":null,"products":[],"checked":"2026-01-15","flag":true}`, LastChecksum: "d"},
	}

	for _, row := range fixtures {
		if err := repo.Upsert(row); err != nil {
			test.Fatalf("Upsert %s: %v", row.ID, err)
		}
	}

	cases := []struct {
		name    string
		input   string
		wantIDs []string
	}{
		// NOT and != are the exact complement of =, so a node without the
		// property (or with a null) matches them.
		{"eq", "type=service domain=billing", []string{"s/a"}},
		{"not_eq", "type=service NOT domain=billing", []string{"s/b", "s/c", "s/d"}},
		{"ne", "type=service domain!=billing", []string{"s/b", "s/c", "s/d"}},
		{"not_or", "type=service NOT (domain=billing OR domain=checkout)", []string{"s/c", "s/d"}},
		{"not_ordering", "type=service NOT checked<2026-06-01", []string{"s/a", "s/b", "s/c"}},
		{"ne_undeclared", "type=service mood!=happy", []string{"s/a", "s/b", "s/c", "s/d"}},
		{"ne_string", "type=service owner!=ann", []string{"s/b", "s/c", "s/d"}},

		// `*` is any value: absent and null are missing, "" is a value, and a
		// list needs at least one item.
		{"has", "type=service domain=*", []string{"s/a", "s/b"}},
		{"missing", "type=service domain!=*", []string{"s/c", "s/d"}},
		{"not_has", "type=service NOT domain=*", []string{"s/c", "s/d"}},
		{"has_empty_string", "type=service owner=*", []string{"s/a", "s/b"}},
		{"has_list", "type=service products=*", []string{"s/a", "s/b"}},
		{"missing_list", "type=service products!=*", []string{"s/c", "s/d"}},
		{"has_undeclared", "type=service tags=*", []string{"s/a", "s/b"}},
		{"missing_title", "type=service title!=*", []string{"s/c"}},

		// ~= tests one item, in a declared list or undeclared frontmatter.
		{"contains", "type=service products~=b", []string{"s/a", "s/b"}},
		{"contains_one", "type=service products~=c", []string{"s/a"}},
		{"not_contains", "type=service NOT products~=b", []string{"s/c", "s/d"}},
		{"contains_int", "type=service scores~=5", []string{"s/a"}},
		{"contains_undeclared_list_or_scalar", "type=service tags~=x", []string{"s/a", "s/b"}},
		{"contains_undeclared_none", "type=service tags~=z", nil},
		{"contains_bool", "type=service flag~=true", []string{"s/d"}},

		// [a, b] needs every item, in any order.
		{"list_all", "type=service products=[a,b]", []string{"s/a"}},
		{"list_any_order", "type=service products=[c, a]", []string{"s/a"}},
		{"list_one", "type=service products=[b]", []string{"s/a", "s/b"}},
		{"list_ne", "type=service products!=[a,b]", []string{"s/b", "s/c", "s/d"}},
		{"list_quoted_item", `type=service products=["a", c]`, []string{"s/a"}},
		{"list_undeclared", "type=service tags=[x,y]", []string{"s/a"}},

		// Relative dates resolve against the compile clock, on the local
		// calendar: today is 2026-10-27 and local midnight is 05:00Z.
		{"rel_before", "type=service checked<today-90d", []string{"s/a", "s/d"}},
		{"rel_since_week", "type=service checked>=today-1w", []string{"s/b"}},
		{"rel_eq", "type=service checked=today-7d", []string{"s/b"}},
		{"rel_month", "type=service checked<today-1m", []string{"s/a", "s/d"}},
		{"rel_year", "type=service checked<today-1y", nil},
		{"rel_range", "type=service checked=today-1y..today", []string{"s/a", "s/b", "s/d"}},
		{"rel_future", "type=service checked<today+2w", []string{"s/a", "s/b", "s/d"}},
		{"rel_not", "type=service NOT checked<today-90d", []string{"s/b", "s/c"}},
		{"rel_datetime_today", "type=service seen>=today", []string{"s/a"}},
		{"rel_datetime_now_hours", "type=service seen>=now-48h", []string{"s/a"}},
		{"rel_datetime_now_days", "type=service seen>=now-3d", []string{"s/a", "s/b"}},
	}

	loaded := propertyFiltersManifest()

	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			expr, parseErrs := filter.NewParser(testCase.input).Parse()

			if len(parseErrs) > 0 {
				test.Fatalf("parse %q: %v", testCase.input, parseErrs)
			}

			if validateErrs := filter.Validate(expr, loaded); len(validateErrs) > 0 {
				test.Fatalf("validate %q: %v", testCase.input, validateErrs)
			}

			sqlQuery, params, compileErr := filter.Compile(expr, filter.CompileOptions{Now: propertyFiltersNow})

			if compileErr != nil {
				test.Fatalf("compile %q: %v", testCase.input, compileErr)
			}

			got := queryIDs(test, store.DB(), sqlQuery, params)

			if !equalIDs(got, testCase.wantIDs) {
				test.Errorf("%q\n  sql: %s\n  params: %v\n  got %v, want %v", testCase.input, sqlQuery, params, got, testCase.wantIDs)
			}
		})
	}
}

func queryIDs(test *testing.T, database *sql.DB, sqlQuery string, params []any) []string {
	test.Helper()

	rows, queryErr := database.Query(sqlQuery, params...)

	if queryErr != nil {
		test.Fatalf("query: %v\n  sql: %s", queryErr, sqlQuery)
	}

	defer rows.Close()

	var ids []string

	for rows.Next() {
		var (
			id, nodeType, path, title, properties, checksum string
			mtime, size                                     int64
			parentID                                        sql.NullString
		)

		if scanErr := rows.Scan(&id, &nodeType, &path, &title, &properties, &mtime, &size, &checksum, &parentID); scanErr != nil {
			test.Fatalf("scan: %v", scanErr)
		}

		ids = append(ids, id)
	}

	if rowsErr := rows.Err(); rowsErr != nil {
		test.Fatalf("rows: %v", rowsErr)
	}

	sort.Strings(ids)

	return ids
}

func equalIDs(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}

	for index := range got {
		if got[index] != want[index] {
			return false
		}
	}

	return true
}
