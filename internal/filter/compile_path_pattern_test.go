package filter_test

import (
	"database/sql"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/germanamz/tusk/internal/filter"
	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/manifest"
)

// TestCompile_PathPatternEmitsRegexp pins the SQL a path/id pattern compiles to:
// a REGEXP match against the translated, anchored regex, negated for !=, on the
// aliased row inside an edge predicate. A quoted value stays an equality.
func TestCompile_PathPatternEmitsRegexp(test *testing.T) {
	cases := []struct {
		name       string
		filter     string
		wantSQL    string
		wantParams []any
	}{
		{"equality", "path=docs/product/*", "path REGEXP ?", []any{`^docs/product/[^/]*$`}},
		{"negation", "id!=docs/**", "NOT (id REGEXP ?)", []any{`^docs/([^/]*/)*[^/]+$`}},
		{"after an edge arrow", "references-> path=docs/?.md", "n0.path REGEXP ?", []any{"references", `^docs/[^/]\.md$`}},
		{"quoted stays literal", `path="docs/*"`, "path = ?", []any{"docs/*"}},
	}

	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			expr, parseErrs := filter.NewParser(testCase.filter).Parse()

			if len(parseErrs) > 0 {
				test.Fatalf("parse %q: %v", testCase.filter, parseErrs)
			}

			sqlText, params, compileErr := filter.Compile(expr, filter.CompileOptions{})

			if compileErr != nil {
				test.Fatalf("compile %q: %v", testCase.filter, compileErr)
			}

			if !strings.Contains(sqlText, testCase.wantSQL) {
				test.Errorf("%q: sql missing %q\ngot: %s", testCase.filter, testCase.wantSQL, sqlText)
			}

			if !reflect.DeepEqual(params, testCase.wantParams) {
				test.Errorf("%q: params = %v, want %v", testCase.filter, params, testCase.wantParams)
			}
		})
	}
}

// TestCompile_PathPatternRows runs path and id patterns through the real
// parse→validate→compile→SQLite path, so the REGEXP function the index
// registers is what decides each row (#763).
func TestCompile_PathPatternRows(test *testing.T) {
	store, openErr := index.Open(filepath.Join(test.TempDir(), "index.db"))

	if openErr != nil {
		test.Fatalf("open index: %v", openErr)
	}

	defer store.Close()

	nodes := index.NewNodeRepo(store)

	for _, id := range []string{
		"docs/product/recording",
		"docs/product/totals",
		"docs/product/sub/deep",
		"docs/guide/index",
		"docs/index",
		"other/docs/product/stray",
		"readme",
		"notes/odd*",
		"notes/odd-two",
	} {
		row := index.NodeRow{ID: id, Type: "note", Path: id + ".md", PropertiesJSON: `{}`}

		if upsertErr := nodes.Upsert(row); upsertErr != nil {
			test.Fatalf("upsert %s: %v", id, upsertErr)
		}
	}

	// A sub-unit shares its file's path and extends its id with `#…`, so a
	// pattern treats `#` as an ordinary character and matches it too.
	paragraph := index.NodeRow{
		ID: "docs/product/totals#P1", Type: "paragraph", Path: "docs/product/totals.md", PropertiesJSON: `{}`,
		ParentID: sql.NullString{String: "docs/product/totals", Valid: true},
	}

	if upsertErr := nodes.BulkUpsert([]index.NodeRow{paragraph}, ""); upsertErr != nil {
		test.Fatalf("upsert paragraph: %v", upsertErr)
	}

	link := index.EdgeRow{Type: "references", SourceID: "readme", TargetID: "docs/product/totals", SourcePath: "readme.md", Kind: "direct"}

	if upsertErr := index.NewEdgeRepo(store).UpsertAll(link.SourceID, link.SourcePath, []index.EdgeRow{link}); upsertErr != nil {
		test.Fatalf("edge: %v", upsertErr)
	}

	loaded := manifest.Manifest{
		NodeTypes: map[string]manifest.NodeType{"note": {}},
		EdgeTypes: map[string]manifest.EdgeType{
			"references": {Cardinality: manifest.CardinalityManyToMany},
		},
	}

	cases := []struct {
		name   string
		filter string
		want   []string
	}{
		{"direct children only", "type=note AND path=docs/product/*", []string{"docs/product/recording", "docs/product/totals"}},
		{"whole subtree", "type=note AND path=docs/product/**", []string{"docs/product/recording", "docs/product/sub/deep", "docs/product/totals"}},
		{"sub-units share the file's path", "path=docs/product/t*", []string{"docs/product/totals", "docs/product/totals#P1"}},
		{"sub-unit ids match like any other", "id=docs/product/t*", []string{"docs/product/totals", "docs/product/totals#P1"}},
		{"type keeps to files", "type=note AND id=docs/product/t*", []string{"docs/product/totals"}},
		{"wildcard directory", "id=docs/*/index", []string{"docs/guide/index"}},
		{"any depth", "id=**/index", []string{"docs/guide/index", "docs/index"}},
		{"root level only", "path=*.md", []string{"readme"}},
		{"single character", "id=docs/product/?otals", []string{"docs/product/totals"}},
		{"negated", "path!=docs/**", []string{"notes/odd*", "notes/odd-two", "other/docs/product/stray", "readme"}},
		{"under NOT", "NOT id=docs/** AND NOT id=notes/*", []string{"other/docs/product/stray", "readme"}},
		{"after an edge arrow", "references-> path=docs/product/*", []string{"readme"}},
		{"bare star is a wildcard", "path=notes/odd*.md", []string{"notes/odd*", "notes/odd-two"}},
		{"quoted star is literal", `path="notes/odd*.md"`, []string{"notes/odd*"}},
	}

	for _, testCase := range cases {
		test.Run(testCase.name, func(subtest *testing.T) {
			expr, parseErrs := filter.NewParser(testCase.filter).Parse()

			if len(parseErrs) > 0 {
				subtest.Fatalf("parse %q: %v", testCase.filter, parseErrs)
			}

			if validateErrs := filter.Validate(expr, loaded); len(validateErrs) > 0 {
				subtest.Fatalf("validate %q: %v", testCase.filter, validateErrs)
			}

			got := runHierarchyFilter(subtest, store, expr)

			if !reflect.DeepEqual(got, testCase.want) {
				subtest.Errorf("%q: ids = %v, want %v", testCase.filter, got, testCase.want)
			}
		})
	}
}
