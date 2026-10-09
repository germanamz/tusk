package filter_test

import (
	"database/sql"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"github.com/germanamz/tusk/internal/filter"
	"github.com/germanamz/tusk/internal/index"
)

// TestCompile_FilesOnlyDropsSubUnits runs FilesOnly through SQLite: sub-units
// share their file's path, so without it a path filter returns every section
// and paragraph too. The OR case pins that the restriction wraps the whole
// WHERE clause instead of binding to its last disjunct.
func TestCompile_FilesOnlyDropsSubUnits(test *testing.T) {
	store, openErr := index.Open(filepath.Join(test.TempDir(), "index.db"))

	if openErr != nil {
		test.Fatalf("open index: %v", openErr)
	}

	defer store.Close()

	nodes := index.NewNodeRepo(store)

	for _, id := range []string{"docs/product/totals", "docs/guide"} {
		if upsertErr := nodes.Upsert(index.NodeRow{ID: id, Type: "note", Path: id + ".md", PropertiesJSON: `{}`}); upsertErr != nil {
			test.Fatalf("upsert %s: %v", id, upsertErr)
		}
	}

	subUnits := []index.NodeRow{
		{ID: "docs/product/totals#S1", Type: "section", Path: "docs/product/totals.md", PropertiesJSON: `{}`, ParentID: sql.NullString{String: "docs/product/totals", Valid: true}},
		{ID: "docs/guide#P1", Type: "paragraph", Path: "docs/guide.md", PropertiesJSON: `{}`, ParentID: sql.NullString{String: "docs/guide", Valid: true}},
	}

	if upsertErr := nodes.BulkUpsert(subUnits, ""); upsertErr != nil {
		test.Fatalf("upsert sub-units: %v", upsertErr)
	}

	cases := []struct {
		name      string
		filter    string
		filesOnly bool
		want      []string
	}{
		{"without FilesOnly sub-units match", "path=docs/**", false, []string{"docs/guide", "docs/guide#P1", "docs/product/totals", "docs/product/totals#S1"}},
		{"FilesOnly keeps files", "path=docs/**", true, []string{"docs/guide", "docs/product/totals"}},
		{"FilesOnly wraps a disjunction", "path=docs/guide.md OR path=docs/product/totals.md", true, []string{"docs/guide", "docs/product/totals"}},
		{"FilesOnly under NOT", "NOT path=docs/guide.md", true, []string{"docs/product/totals"}},
	}

	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			expr, parseErrs := filter.NewParser(testCase.filter).Parse()

			if len(parseErrs) > 0 {
				test.Fatalf("parse %q: %v", testCase.filter, parseErrs)
			}

			sqlText, params, compileErr := filter.Compile(expr, filter.CompileOptions{FilesOnly: testCase.filesOnly})

			if compileErr != nil {
				test.Fatalf("compile %q: %v", testCase.filter, compileErr)
			}

			rows, queryErr := store.DB().Query(sqlText, params...)

			if queryErr != nil {
				test.Fatalf("query %q: %v", sqlText, queryErr)
			}

			defer rows.Close()

			var got []string

			for rows.Next() {
				var (
					id, nodeType, path, title, properties, checksum string
					mtime, size                                     int64
					parentID                                        sql.NullString
				)

				if scanErr := rows.Scan(&id, &nodeType, &path, &title, &properties, &mtime, &size, &checksum, &parentID); scanErr != nil {
					test.Fatalf("scan: %v", scanErr)
				}

				got = append(got, id)
			}

			sort.Strings(got)

			if !reflect.DeepEqual(got, testCase.want) {
				test.Fatalf("%q (FilesOnly=%v) = %v, want %v", testCase.filter, testCase.filesOnly, got, testCase.want)
			}
		})
	}
}

// TestOuterTypes pins which `type=` names select rows positively at the outer
// level: through AND and OR, but not under NOT or past an edge arrow, where
// the name constrains something other than the matched row.
func TestOuterTypes(test *testing.T) {
	cases := []struct {
		filter string
		want   []string
	}{
		{"type=section", []string{"section"}},
		{"type=section AND heading-level=1", []string{"section"}},
		{"type=note OR type=section", []string{"note", "section"}},
		{"(type=section OR type=paragraph) AND NOT path=docs/**", []string{"paragraph", "section"}},
		{"type=markdown:section", []string{"section"}},
		{"NOT type=section", nil},
		{"type!=section", nil},
		{"references-> type=section", nil},
		{"path=docs/**", nil},
	}

	for _, testCase := range cases {
		test.Run(testCase.filter, func(test *testing.T) {
			expr, parseErrs := filter.NewParser(testCase.filter).Parse()

			if len(parseErrs) > 0 {
				test.Fatalf("parse %q: %v", testCase.filter, parseErrs)
			}

			if got := filter.OuterTypes(expr); !reflect.DeepEqual(got, testCase.want) {
				test.Fatalf("OuterTypes(%q) = %v, want %v", testCase.filter, got, testCase.want)
			}
		})
	}
}
