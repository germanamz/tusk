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
	// order is a bare JSON number; score is a bare JSON float; code is a string —
	// matching how the indexer writes each declared type.
	fixtures := []index.NodeRow{
		{ID: "t/a", Type: "ticket", Path: "t/a.md", Title: "A", PropertiesJSON: `{"priority":"high","due":"2026-08-10","order":3,"score":9.9,"code":"apple"}`, LastChecksum: "a"},
		{ID: "t/b", Type: "ticket", Path: "t/b.md", Title: "B", PropertiesJSON: `{"priority":"medium","due":"2026-09-09","order":1,"score":10.1,"code":"banana"}`, LastChecksum: "b"},
		{ID: "t/c", Type: "ticket", Path: "t/c.md", Title: "C", PropertiesJSON: `{"priority":"low","due":"2026-11-09","order":10,"score":10.0,"code":"cherry"}`, LastChecksum: "c"},
		{ID: "t/d", Type: "ticket", Path: "t/d.md", Title: "D", PropertiesJSON: `{"priority":"high","due":"2026-12-08","order":2,"score":2.5,"code":"date"}`, LastChecksum: "d"},
		{ID: "t/e", Type: "ticket", Path: "t/e.md", Title: "E", PropertiesJSON: `{"priority":"low","due":"2026-01-15","order":5,"score":0.5,"code":"elder"}`, LastChecksum: "e"},
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
				{Name: "score", Type: "float"},
				{Name: "code", Type: "string"},
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

		// int ordering — unchanged behaviour
		{"int_ge", "type=ticket order>=2", []string{"t/a", "t/c", "t/d", "t/e"}},
		// int EXACT equality — was silently zero rows before the H1 fix
		{"int_eq", "type=ticket order=2", []string{"t/d"}},
		{"int_ne", "type=ticket order!=2", []string{"t/a", "t/b", "t/c", "t/e"}},

		// float — integer coercion used to truncate the fractional part
		{"float_gt", "type=ticket score>9.5", []string{"t/a", "t/b", "t/c"}},
		{"float_gt_boundary", "type=ticket score>10", []string{"t/b"}},
		{"float_eq", "type=ticket score=2.5", []string{"t/d"}},
		{"float_range", "type=ticket score=0.5..9.9", []string{"t/a", "t/d", "t/e"}},

		// string — integer coercion used to collapse every value to 0
		{"string_lt", "type=ticket code<cherry", []string{"t/a", "t/b"}},
		{"string_ge", "type=ticket code>=cherry", []string{"t/c", "t/d", "t/e"}},
		{"string_eq", "type=ticket code=apple", []string{"t/a"}},
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

// TestTypedSort_EnumByDeclaredOrder_EndToEnd proves that `--sort` on an enum
// property orders by the manifest's declared order (low < medium < high), not
// lexically by the stored name (which would give medium < low < high). It runs
// the full parse → validate → ParseSort → ResolveSortKeys → compile → query
// path against a real index — issue #664 audit M3.
func TestTypedSort_EnumByDeclaredOrder_EndToEnd(test *testing.T) {
	store, openErr := index.Open(filepath.Join(test.TempDir(), "index.db"))

	if openErr != nil {
		test.Fatalf("index.Open: %v", openErr)
	}

	defer store.Close()

	repo := index.NewNodeRepo(store)

	fixtures := []index.NodeRow{
		{ID: "t/a", Type: "ticket", Path: "t/a.md", Title: "A", PropertiesJSON: `{"priority":"high"}`, LastChecksum: "a"},
		{ID: "t/b", Type: "ticket", Path: "t/b.md", Title: "B", PropertiesJSON: `{"priority":"medium"}`, LastChecksum: "b"},
		{ID: "t/c", Type: "ticket", Path: "t/c.md", Title: "C", PropertiesJSON: `{"priority":"low"}`, LastChecksum: "c"},
		{ID: "t/d", Type: "ticket", Path: "t/d.md", Title: "D", PropertiesJSON: `{"priority":"high"}`, LastChecksum: "d"},
		{ID: "t/e", Type: "ticket", Path: "t/e.md", Title: "E", PropertiesJSON: `{"priority":"low"}`, LastChecksum: "e"},
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
			}},
		},
	}

	expr, parseErrs := filter.NewParser("type=ticket").Parse()

	if len(parseErrs) != 0 {
		test.Fatalf("parse: %+v", parseErrs)
	}

	if validateErrs := filter.Validate(expr, loaded); len(validateErrs) != 0 {
		test.Fatalf("validate: %+v", validateErrs)
	}

	// `-priority` desc by declared order (high first); `+id` breaks ties so the
	// expected sequence is deterministic.
	sortKeys, sortErr := filter.ParseSort("-priority,+id")

	if sortErr != nil {
		test.Fatalf("ParseSort: %v", sortErr)
	}

	filter.ResolveSortKeys(expr, loaded, sortKeys)

	sqlQuery, params, compileErr := filter.Compile(expr, filter.CompileOptions{SortKeys: sortKeys})

	if compileErr != nil {
		test.Fatalf("compile: %v", compileErr)
	}

	rows, queryErr := store.DB().Query(sqlQuery, params...)

	if queryErr != nil {
		test.Fatalf("query: %v\nsql=%s", queryErr, sqlQuery)
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

	// Declared order high(2) > medium(1) > low(0): t/a,t/d (high), t/b (medium),
	// t/c,t/e (low). A lexical name sort would wrongly put medium before low.
	want := []string{"t/a", "t/d", "t/b", "t/c", "t/e"}

	if !equalStringSlices(gotIDs, want) {
		test.Errorf("enum sort order\n got = %v\nwant = %v\nsql = %s", gotIDs, want, sqlQuery)
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
