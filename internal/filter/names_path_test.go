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

func pathRefsManifest() manifest.Manifest {
	return manifest.Manifest{
		NodeTypes: map[string]manifest.NodeType{"technical": {}, "note": {}},
		EdgeTypes: map[string]manifest.EdgeType{
			"describes":  {From: []string{"technical"}, Cardinality: manifest.CardinalityManyToMany, Paths: true},
			"references": {From: []string{"*"}, To: []string{"*"}, Cardinality: manifest.CardinalityManyToMany},
		},
	}
}

func TestParse_NamesPath(test *testing.T) {
	cases := []struct {
		filter string
		want   filter.NamesPathPredicate
	}{
		{"names-path=server/ledger/core/service.go", filter.NamesPathPredicate{Value: "server/ledger/core/service.go"}},
		{"names-path:server/ledger/", filter.NamesPathPredicate{Value: "server/ledger/"}},
		{"names-path!=server/**", filter.NamesPathPredicate{Value: "server/**", Pattern: true, Negated: true}},
		{`names-path="docs/*.md"`, filter.NamesPathPredicate{Value: "docs/*.md"}},
	}

	for _, testCase := range cases {
		expr, parseErrs := filter.NewParser(testCase.filter).Parse()

		if len(parseErrs) > 0 {
			test.Fatalf("parse %q: %v", testCase.filter, parseErrs)
		}

		got, ok := expr.(*filter.NamesPathPredicate)

		if !ok {
			test.Fatalf("parse %q = %T, want *NamesPathPredicate", testCase.filter, expr)
		}

		got.Pos = 0

		if *got != testCase.want {
			test.Errorf("parse %q = %+v, want %+v", testCase.filter, *got, testCase.want)
		}
	}

	if _, parseErrs := filter.NewParser("names-path>server/x.go").Parse(); len(parseErrs) == 0 {
		test.Errorf("names-path with > should not parse")
	}
}

func TestValidate_NamesPath(test *testing.T) {
	validateFilter := func(input string, loaded manifest.Manifest) []filter.ValidationError {
		expr, parseErrs := filter.NewParser(input).Parse()

		if len(parseErrs) > 0 {
			test.Fatalf("parse %q: %v", input, parseErrs)
		}

		return filter.Validate(expr, loaded)
	}

	if errs := validateFilter("names-path=server/x.go", pathRefsManifest()); len(errs) != 0 {
		test.Errorf("valid filter: %v", errs)
	}

	noPaths := manifest.Manifest{EdgeTypes: map[string]manifest.EdgeType{"references": {}}}

	if errs := validateFilter("names-path=server/x.go", noPaths); len(errs) != 1 || !strings.Contains(errs[0].Message, "paths = true") {
		test.Errorf("without a paths edge type: %v, want one paths = true error", errs)
	}

	for _, bad := range []string{"names-path=/etc/hosts", `names-path="../up.go"`} {
		if errs := validateFilter(bad, pathRefsManifest()); len(errs) != 1 {
			test.Errorf("%q: %v, want one error", bad, errs)
		}
	}
}

func TestCompile_NamesPathSQL(test *testing.T) {
	cases := []struct {
		name       string
		filter     string
		wantSQL    string
		wantParams []any
	}{
		{
			"literal expands to ancestors", "names-path=server/ledger/core.go",
			"id IN (SELECT source_id FROM path_refs WHERE target IN (?, ?, ?))",
			[]any{"server/ledger/core.go", "server/ledger", "server"},
		},
		{
			"glob matches the named path", "names-path=server/**",
			"id IN (SELECT source_id FROM path_refs WHERE target REGEXP ?)",
			[]any{`^server/([^/]*/)*[^/]+$`},
		},
		{
			"negated", "names-path!=go.mod",
			"NOT (id IN (SELECT source_id FROM path_refs WHERE target IN (?)))",
			[]any{"go.mod"},
		},
		{
			"after an edge arrow", "references-> names-path=go.mod",
			"n0.id IN (SELECT source_id FROM path_refs WHERE target IN (?))",
			[]any{"references", "go.mod"},
		},
	}

	for _, testCase := range cases {
		test.Run(testCase.name, func(subtest *testing.T) {
			expr, parseErrs := filter.NewParser(testCase.filter).Parse()

			if len(parseErrs) > 0 {
				subtest.Fatalf("parse: %v", parseErrs)
			}

			sqlText, params, compileErr := filter.Compile(expr, filter.CompileOptions{})

			if compileErr != nil {
				subtest.Fatalf("compile: %v", compileErr)
			}

			if !strings.Contains(sqlText, testCase.wantSQL) {
				subtest.Errorf("sql missing %q\ngot: %s", testCase.wantSQL, sqlText)
			}

			if !reflect.DeepEqual(params, testCase.wantParams) {
				subtest.Errorf("params = %v, want %v", params, testCase.wantParams)
			}
		})
	}
}

// TestCompile_NamesPathRows runs names-path through parse, validate, compile
// and SQLite against real path_refs rows.
func TestCompile_NamesPathRows(test *testing.T) {
	store, openErr := index.Open(filepath.Join(test.TempDir(), "index.db"))

	if openErr != nil {
		test.Fatalf("open index: %v", openErr)
	}

	defer store.Close()

	nodes := index.NewNodeRepo(store)
	edges := index.NewEdgeRepo(store)

	for _, page := range []struct{ id, kind string }{
		{"technical/ledger", "technical"},
		{"technical/arch", "technical"},
		{"technical/build", "technical"},
		{"notes/plain", "note"},
		{"notes/index", "note"},
	} {
		if upsertErr := nodes.Upsert(index.NodeRow{ID: page.id, Type: page.kind, Path: page.id + ".md", PropertiesJSON: `{}`}); upsertErr != nil {
			test.Fatalf("upsert %s: %v", page.id, upsertErr)
		}
	}

	paragraph := index.NodeRow{
		ID: "technical/ledger#P1", Type: "paragraph", Path: "technical/ledger.md", PropertiesJSON: `{}`,
		ParentID: sql.NullString{String: "technical/ledger", Valid: true},
	}

	if upsertErr := nodes.BulkUpsert([]index.NodeRow{paragraph}, ""); upsertErr != nil {
		test.Fatalf("upsert paragraph: %v", upsertErr)
	}

	refs := map[string][]string{
		"technical/ledger": {"server/ledger/core/service.go", "server/wire.go"},
		"technical/arch":   {"server/ledger"},
		"technical/build":  {"go.mod", "node.Node"},
	}

	for source, targets := range refs {
		rows := make([]index.PathRefRow, 0, len(targets))

		for line, target := range targets {
			rows = append(rows, index.PathRefRow{Type: "describes", Target: target, Line: line + 1})
		}

		if replaceErr := edges.ReplacePathRefs(source, rows); replaceErr != nil {
			test.Fatalf("refs %s: %v", source, replaceErr)
		}
	}

	link := index.EdgeRow{Type: "references", SourceID: "notes/index", TargetID: "technical/ledger", SourcePath: "notes/index.md", Kind: "direct"}

	if upsertErr := edges.UpsertAll(link.SourceID, link.SourcePath, []index.EdgeRow{link}); upsertErr != nil {
		test.Fatalf("edge: %v", upsertErr)
	}

	cases := []struct {
		name   string
		filter string
		want   []string
	}{
		{"exact path and the directory above it", "names-path=server/ledger/core/service.go", []string{"technical/arch", "technical/ledger"}},
		{"a trailing slash and line suffix are cleaned", "names-path=server/ledger/:3", []string{"technical/arch"}},
		{"a directory query alone matches its own refs", "names-path=server/ledger", []string{"technical/arch"}},
		{"glob over named paths", "names-path=server/**", []string{"technical/arch", "technical/ledger"}},
		{"root file", "names-path=go.mod", []string{"technical/build"}},
		{"negated keeps pages without the ref", "type=technical AND names-path!=go.mod", []string{"technical/arch", "technical/ledger"}},
		{"composes with NOT", "type=technical AND NOT names-path=server/**", []string{"technical/build"}},
		{"after an edge arrow", "references-> names-path=server/wire.go", []string{"notes/index"}},
		{"nothing names it", "names-path=server/other.go", nil},
	}

	for _, testCase := range cases {
		test.Run(testCase.name, func(subtest *testing.T) {
			expr, parseErrs := filter.NewParser(testCase.filter).Parse()

			if len(parseErrs) > 0 {
				subtest.Fatalf("parse %q: %v", testCase.filter, parseErrs)
			}

			if validateErrs := filter.Validate(expr, pathRefsManifest()); len(validateErrs) > 0 {
				subtest.Fatalf("validate %q: %v", testCase.filter, validateErrs)
			}

			got := runHierarchyFilter(subtest, store, expr)

			if !reflect.DeepEqual(got, testCase.want) {
				subtest.Errorf("%q: ids = %v, want %v", testCase.filter, got, testCase.want)
			}
		})
	}
}

func TestNamesPathPredicate_MatchesAndOuter(test *testing.T) {
	expr, parseErrs := filter.NewParser("type=technical AND (names-path=server/ledger/core.go OR names-path=docs/**) AND NOT names-path=go.mod").Parse()

	if len(parseErrs) > 0 {
		test.Fatalf("parse: %v", parseErrs)
	}

	outer := filter.OuterNamesPaths(expr)

	if len(outer) != 2 {
		test.Fatalf("OuterNamesPaths = %d predicates, want 2 (the NOT one excluded)", len(outer))
	}

	literal, glob := outer[0], outer[1]

	for target, want := range map[string]bool{"server/ledger/core.go": true, "server/ledger": true, "server": true, "server/ledger/other.go": false} {
		if got := literal.Matches(target); got != want {
			test.Errorf("literal.Matches(%q) = %v, want %v", target, got, want)
		}
	}

	for target, want := range map[string]bool{"docs/a/b.md": true, "docs": false, "server/x": false} {
		if got := glob.Matches(target); got != want {
			test.Errorf("glob.Matches(%q) = %v, want %v", target, got, want)
		}
	}
}
