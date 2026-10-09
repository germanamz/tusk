package filter_test

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/germanamz/tusk/internal/filter"
	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/manifest"
)

// TestCompile_EdgeInnerTermRows pins the rows an edge traversal returns when
// the term after the arrow is more than one property predicate: a group, a NOT,
// a recency check, a hierarchy shortcut, or another traversal nested in either.
// Each filter runs the real parse→validate→compile→SQLite path. It is the
// regression for #761, where a group failed to parse, NOT bound to the outer
// node, and modified-since/tree= were read as frontmatter keys.
//
//	totals ──references──▶ recording ──references──▶ ledger ──parent──▶ area
//	(product, stale)       (product, stale)          (technical, fresh)
func TestCompile_EdgeInnerTermRows(test *testing.T) {
	store, openErr := index.Open(filepath.Join(test.TempDir(), "index.db"))

	if openErr != nil {
		test.Fatalf("open index: %v", openErr)
	}

	defer store.Close()

	nodes := index.NewNodeRepo(store)
	edges := index.NewEdgeRepo(store)
	now := time.Now()
	stale := now.Add(-30 * 24 * time.Hour).UnixNano()
	fresh := now.Add(-1 * time.Hour).UnixNano()

	fixtures := []index.NodeRow{
		{ID: "totals", Type: "note", Path: "totals.md", PropertiesJSON: `{"domain":"product"}`, LastMtime: stale},
		{ID: "recording", Type: "note", Path: "recording.md", PropertiesJSON: `{"domain":"product"}`, LastMtime: stale},
		{ID: "ledger", Type: "note", Path: "ledger.md", PropertiesJSON: `{"domain":"technical"}`, LastMtime: fresh},
		{ID: "area", Type: "area", Path: "area.md", PropertiesJSON: `{}`, LastMtime: stale},
	}

	for _, row := range fixtures {
		if upsertErr := nodes.Upsert(row); upsertErr != nil {
			test.Fatalf("upsert %s: %v", row.ID, upsertErr)
		}
	}

	links := []index.EdgeRow{
		{Type: "references", SourceID: "totals", TargetID: "recording", SourcePath: "totals.md", Kind: "direct"},
		{Type: "references", SourceID: "recording", TargetID: "ledger", SourcePath: "recording.md", Kind: "direct"},
		{Type: "parent", SourceID: "ledger", TargetID: "area", SourcePath: "ledger.md", Kind: "direct"},
	}

	for _, link := range links {
		if upsertErr := edges.UpsertAll(link.SourceID, link.SourcePath, []index.EdgeRow{link}); upsertErr != nil {
			test.Fatalf("edge %s→%s: %v", link.SourceID, link.TargetID, upsertErr)
		}
	}

	loaded := manifest.Manifest{
		NodeTypes: map[string]manifest.NodeType{
			"note": {Properties: []manifest.PropertyDecl{
				{Name: "domain", Type: "enum", Values: []string{"product", "technical"}},
			}},
			"area": {},
		},
		EdgeTypes: map[string]manifest.EdgeType{
			"references": {Cardinality: manifest.CardinalityManyToMany},
			"parent":     {Cardinality: manifest.CardinalityManyToOne, Hierarchy: "area", HierarchyDefault: true},
		},
	}

	cases := []struct {
		name   string
		filter string
		want   []string
	}{
		{"single property (unchanged)", "domain=product AND references-> domain=technical", []string{"recording"}},
		{"group", "domain=product AND references-> (domain=technical OR domain=product)", []string{"recording", "totals"}},
		{"OR group stays inside the edge join", "references-> (domain=technical OR domain=product)", []string{"recording", "totals"}},
		{"NOT binds to the target", "domain=product AND references-> NOT domain=product", []string{"recording"}},
		{"NOT of a nested traversal", "references-> NOT references->", []string{"recording"}},
		{"modified-since on the target", "references-> modified-since:7d", []string{"recording"}},
		{"tree= on the target", "references-> tree=area", []string{"recording"}},
		{"tree= on the target after a param-bearing term", "domain=product AND references-> tree=area", []string{"recording"}},
		{"parent= on the target", "references-> parent=area", []string{"recording"}},
		{"root= on the target", "references-> root=ledger", []string{"recording"}},
		{"traversal inside a group", "references-> (domain=technical OR references-> domain=technical)", []string{"recording", "totals"}},
		{"conjunction inside a group", "references-> (type=note domain=product)", []string{"totals"}},
		{"arrow still takes one term", "references-> domain=technical domain=product", []string{"recording"}},
		{"NOT still takes one term", "references-> NOT domain=product domain=product", []string{"recording"}},
		{"bare traversal then AND NOT", "references-> AND NOT domain=technical", []string{"recording", "totals"}},
		{"incoming traversal with a group", "references<- (domain=product modified-since:365d)", []string{"ledger", "recording"}},
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

// TestCompile_InnerShortcutDoesNotOrderResults pins that a hierarchy shortcut
// inside an edge predicate leaves the result order alone: its edge's ordering
// property belongs to the traversal target, not to the rows returned.
func TestCompile_InnerShortcutDoesNotOrderResults(test *testing.T) {
	inner := &filter.EdgePredicate{
		EdgeType: "references",
		Inner:    &filter.TraversalShortcut{Kind: filter.ShortcutTree, NodeID: "area", EdgeType: "parent", OrderedBy: "rank"},
	}

	sqlText, _, compileErr := filter.Compile(inner, filter.CompileOptions{})

	if compileErr != nil {
		test.Fatalf("compile: %v", compileErr)
	}

	if want := "n0.id IN (SELECT node_id FROM descendants_1)"; !strings.Contains(sqlText, want) {
		test.Errorf("sql missing %q\ngot: %s", want, sqlText)
	}

	if strings.Contains(sqlText, "ORDER BY") {
		test.Errorf("inner shortcut set a default ORDER BY: %s", sqlText)
	}
}
