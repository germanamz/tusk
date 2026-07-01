package filter_test

import (
	"database/sql"
	"path/filepath"
	"sort"
	"testing"

	"github.com/germanamz/tusk/internal/filter"
	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/manifest"
)

// TestTypedOrdering_EndToEndAgainstIndex is the executable proof for issue
// #661: it runs the full parse → validate → compile → query path against a real
// SQLite index and asserts the row sets for ordering/range comparisons on date
// and enum properties — the cases that previously CAST to integer and silently
// returned zero rows. The existing compile_test.go suite only asserts SQL
// shape; this guards the actual numeric/lexical correctness.
func TestTypedOrdering_EndToEndAgainstIndex(test *testing.T) {
	store, openErr := index.Open(filepath.Join(test.TempDir(), "index.db"))

	if openErr != nil {
		test.Fatalf("index.Open: %v", openErr)
	}

	defer store.Close()

	repo := index.NewNodeRepo(store)

	// priority is the stored enum NAME; due is the canonical YYYY-MM-DD string;
	// order is a bare JSON number — matching how the indexer writes each type.
	fixtures := []index.NodeRow{
		{ID: "t/a", Type: "ticket", Path: "t/a.md", Title: "A", PropertiesJSON: `{"priority":"high","due":"2026-08-10","order":3}`, LastChecksum: "a"},
		{ID: "t/b", Type: "ticket", Path: "t/b.md", Title: "B", PropertiesJSON: `{"priority":"medium","due":"2026-09-09","order":1}`, LastChecksum: "b"},
		{ID: "t/c", Type: "ticket", Path: "t/c.md", Title: "C", PropertiesJSON: `{"priority":"low","due":"2026-11-09","order":10}`, LastChecksum: "c"},
		{ID: "t/d", Type: "ticket", Path: "t/d.md", Title: "D", PropertiesJSON: `{"priority":"high","due":"2026-12-08","order":2}`, LastChecksum: "d"},
		{ID: "t/e", Type: "ticket", Path: "t/e.md", Title: "E", PropertiesJSON: `{"priority":"low","due":"2026-01-15","order":5}`, LastChecksum: "e"},
	}

	for _, row := range fixtures {
		if err := repo.Upsert(row); err != nil {
			test.Fatalf("Upsert %s: %v", row.ID, err)
		}
	}

	loaded := manifest.Manifest{
		NodeTypes: map[string]manifest.NodeType{
			"ticket": {Properties: []manifest.PropertyDecl{
				{Name: "priority", Type: "enum", Values: []string{"low", "medium", "high"}},
				{Name: "due", Type: "date"},
				{Name: "order", Type: "int"},
			}},
		},
	}

	cases := []struct {
		name    string
		input   string
		wantIDs []string
	}{
		// date — the headline regressions
		{"date_ge", "type=ticket due>=2026-08-01", []string{"t/a", "t/b", "t/c", "t/d"}},
		{"date_gt", "type=ticket due>2026-01-01", []string{"t/a", "t/b", "t/c", "t/d", "t/e"}},
		{"date_range", "type=ticket due=2026-08-01..2026-12-31", []string{"t/a", "t/b", "t/c", "t/d"}},
		{"date_lt", "type=ticket due<2026-08-10", []string{"t/e"}},

		// enum by name
		{"enum_ge_name", "type=ticket priority>=medium", []string{"t/a", "t/b", "t/d"}},
		{"enum_gt_name", "type=ticket priority>medium", []string{"t/a", "t/d"}},
		{"enum_range_name", "type=ticket priority=low..medium", []string{"t/b", "t/c", "t/e"}},

		// enum by 0-based index
		{"enum_ge_index_2", "type=ticket priority>=2", []string{"t/a", "t/d"}},
		{"enum_ge_index_1", "type=ticket priority>=1", []string{"t/a", "t/b", "t/d"}},

		// enum exact still matches by name
		{"enum_eq", "type=ticket priority=high", []string{"t/a", "t/d"}},

		// int sanity check — unchanged behaviour
		{"int_ge", "type=ticket order>=2", []string{"t/a", "t/c", "t/d", "t/e"}},
	}

	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			expr, parseErrs := filter.NewParser(testCase.input).Parse()

			if len(parseErrs) != 0 {
				test.Fatalf("parse: %+v", parseErrs)
			}

			if validateErrs := filter.Validate(expr, loaded); len(validateErrs) != 0 {
				test.Fatalf("validate: %+v", validateErrs)
			}

			sqlQuery, params, compileErr := filter.Compile(expr, filter.CompileOptions{})

			if compileErr != nil {
				test.Fatalf("compile: %v", compileErr)
			}

			rows, queryErr := store.DB().Query(sqlQuery, params...)

			if queryErr != nil {
				test.Fatalf("query: %v\nsql=%s\nparams=%v", queryErr, sqlQuery, params)
			}

			defer rows.Close()

			var gotIDs []string

			for rows.Next() {
				var (
					id, nodeType, path, title, propertiesJSON, lastChecksum string
					lastMtime, lastSize                                     int64
					parentID                                                sql.NullString
				)

				if scanErr := rows.Scan(&id, &nodeType, &path, &title, &propertiesJSON, &lastMtime, &lastSize, &lastChecksum, &parentID); scanErr != nil {
					test.Fatalf("scan: %v", scanErr)
				}

				gotIDs = append(gotIDs, id)
			}

			if rowsErr := rows.Err(); rowsErr != nil {
				test.Fatalf("rows iteration: %v", rowsErr)
			}

			sort.Strings(gotIDs)
			sort.Strings(testCase.wantIDs)

			if !equalStringSlices(gotIDs, testCase.wantIDs) {
				test.Errorf("query %q\nids  = %v\nwant = %v\nsql  = %s\nparams = %v",
					testCase.input, gotIDs, testCase.wantIDs, sqlQuery, params)
			}
		})
	}
}

// TestTypedOrdering_InvalidComparisonsError confirms the issue's option (b):
// genuinely invalid ordering/range comparisons error at validation time rather
// than silently returning zero rows.
func TestTypedOrdering_InvalidComparisonsError(test *testing.T) {
	loaded := manifest.Manifest{
		NodeTypes: map[string]manifest.NodeType{
			"ticket": {Properties: []manifest.PropertyDecl{
				{Name: "priority", Type: "enum", Values: []string{"low", "medium", "high"}},
				{Name: "due", Type: "date"},
			}},
		},
	}

	for _, input := range []string{
		"type=ticket priority>=urgent",
		"type=ticket priority>=9",
		"type=ticket due>=not-a-date",
	} {
		expr, parseErrs := filter.NewParser(input).Parse()

		if len(parseErrs) != 0 {
			test.Fatalf("parse %q: %+v", input, parseErrs)
		}

		if validateErrs := filter.Validate(expr, loaded); len(validateErrs) == 0 {
			test.Errorf("expected a validation error for %q, got none", input)
		}
	}
}
