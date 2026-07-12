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

// runHierarchyFilter compiles expr and runs it against store, returning the
// sorted set of matched node ids. It exercises the real compile→SQL→SQLite
// path so the test pins returned rows, not SQL substrings.
func runHierarchyFilter(test *testing.T, store *index.Index, expr filter.Expr) []string {
	test.Helper()

	sqlText, params, compileErr := filter.Compile(expr, filter.CompileOptions{})

	if compileErr != nil {
		test.Fatalf("Compile: %v", compileErr)
	}

	rows, queryErr := store.DB().Query(sqlText, params...)

	if queryErr != nil {
		test.Fatalf("Query: %v\nSQL: %s", queryErr, sqlText)
	}

	defer rows.Close()

	cols, _ := rows.Columns()
	holders := make([]sql.RawBytes, len(cols))
	scan := make([]any, len(cols))

	for col := range holders {
		scan[col] = &holders[col]
	}

	var ids []string

	for rows.Next() {
		if scanErr := rows.Scan(scan...); scanErr != nil {
			test.Fatalf("Scan: %v", scanErr)
		}

		ids = append(ids, string(holders[0]))
	}

	sort.Strings(ids)

	return ids
}

// TestCompile_HierarchyShortcutsReturnCorrectRows builds a child→parent tree
// (the default "parent" hierarchy: the property lives on the child, so the edge
// points child→parent) and asserts the rows each shortcut returns. This is the
// regression for tree=/root= walking the edge in the wrong direction.
//
//	root
//	├── a   (parent: root)
//	│   └── a1 (parent: a)
//	└── b   (parent: root)
func TestCompile_HierarchyShortcutsReturnCorrectRows(test *testing.T) {
	store, openErr := index.Open(filepath.Join(test.TempDir(), "index.db"))

	if openErr != nil {
		test.Fatalf("open index: %v", openErr)
	}

	defer store.Close()

	nodes := index.NewNodeRepo(store)
	edges := index.NewEdgeRepo(store)

	for _, id := range []string{"root", "a", "a1", "b"} {
		if upsertErr := nodes.Upsert(index.NodeRow{ID: id, Type: "ticket", Path: id + ".md", Title: id}); upsertErr != nil {
			test.Fatalf("upsert %s: %v", id, upsertErr)
		}
	}

	links := []struct {
		child, parent string
	}{
		{"a", "root"},
		{"a1", "a"},
		{"b", "root"},
	}

	for _, link := range links {
		if upsertErr := edges.UpsertAll(link.child, link.child+".md", []index.EdgeRow{
			{Type: "parent", SourceID: link.child, TargetID: link.parent, SourcePath: link.child + ".md", Kind: "direct"},
		}); upsertErr != nil {
			test.Fatalf("edge %s→%s: %v", link.child, link.parent, upsertErr)
		}
	}

	cases := []struct {
		name string
		expr filter.Expr
		want []string
	}{
		{
			name: "tree=root returns the whole subtree below root",
			expr: &filter.TraversalShortcut{Kind: filter.ShortcutTree, NodeID: "root", EdgeType: "parent"},
			want: []string{"a", "a1", "b"},
		},
		{
			name: "tree=a returns a's descendants",
			expr: &filter.TraversalShortcut{Kind: filter.ShortcutTree, NodeID: "a", EdgeType: "parent"},
			want: []string{"a1"},
		},
		{
			name: "parent=root returns direct children only",
			expr: &filter.TraversalShortcut{Kind: filter.ShortcutParentOf, NodeID: "root", EdgeType: "parent"},
			want: []string{"a", "b"},
		},
		{
			name: "root=a1 returns the whole tree containing a1",
			expr: &filter.TraversalShortcut{Kind: filter.ShortcutRoot, NodeID: "a1", EdgeType: "parent"},
			want: []string{"a", "a1", "b", "root"},
		},
	}

	for _, testCase := range cases {
		test.Run(testCase.name, func(subtest *testing.T) {
			got := runHierarchyFilter(test, store, testCase.expr)

			if len(got) != len(testCase.want) {
				subtest.Fatalf("ids = %v, want %v", got, testCase.want)
			}

			for pos := range got {
				if got[pos] != testCase.want[pos] {
					subtest.Fatalf("ids = %v, want %v", got, testCase.want)
				}
			}
		})
	}
}

// TestCompile_HierarchyShortcutOrderIndependentRows pins the AND-commutativity of
// tree=/root= shortcuts: a recursive-CTE shortcut must return the same rows
// regardless of whether it is the leftmost leaf of the expression. It is the
// regression for the bind-param bug where CTE placeholders (positionally first in
// the assembled `WITH RECURSIVE … SELECT`) were bound against AST-order params, so
// `type=ticket AND tree=root` seeded the walk from the bogus node id "ticket" and
// silently returned 0 rows while `tree=root AND type=ticket` worked.
//
//	root
//	├── a   (parent: root)
//	│   └── a1 (parent: a)
//	└── b   (parent: root)
func TestCompile_HierarchyShortcutOrderIndependentRows(test *testing.T) {
	store, openErr := index.Open(filepath.Join(test.TempDir(), "index.db"))

	if openErr != nil {
		test.Fatalf("open index: %v", openErr)
	}

	defer store.Close()

	nodes := index.NewNodeRepo(store)
	edges := index.NewEdgeRepo(store)

	for _, id := range []string{"root", "a", "a1", "b"} {
		if upsertErr := nodes.Upsert(index.NodeRow{ID: id, Type: "ticket", Path: id + ".md", Title: id}); upsertErr != nil {
			test.Fatalf("upsert %s: %v", id, upsertErr)
		}
	}

	links := []struct {
		child, parent string
	}{
		{"a", "root"},
		{"a1", "a"},
		{"b", "root"},
	}

	for _, link := range links {
		if upsertErr := edges.UpsertAll(link.child, link.child+".md", []index.EdgeRow{
			{Type: "parent", SourceID: link.child, TargetID: link.parent, SourcePath: link.child + ".md", Kind: "direct"},
		}); upsertErr != nil {
			test.Fatalf("edge %s→%s: %v", link.child, link.parent, upsertErr)
		}
	}

	typeTicket := func() filter.Expr {
		return &filter.PropertyPredicate{Property: "type", Op: filter.OpEQ, Value: filter.StringValue{V: "ticket"}}
	}
	tree := func(id string) filter.Expr {
		return &filter.TraversalShortcut{Kind: filter.ShortcutTree, NodeID: id, EdgeType: "parent"}
	}
	root := func(id string) filter.Expr {
		return &filter.TraversalShortcut{Kind: filter.ShortcutRoot, NodeID: id, EdgeType: "parent"}
	}

	cases := []struct {
		name          string
		shortcutFirst filter.Expr
		shortcutLast  filter.Expr
		want          []string
	}{
		{
			name:          "tree=root",
			shortcutFirst: &filter.AndExpr{Left: tree("root"), Right: typeTicket()},
			shortcutLast:  &filter.AndExpr{Left: typeTicket(), Right: tree("root")},
			want:          []string{"a", "a1", "b"},
		},
		{
			name:          "root=a1",
			shortcutFirst: &filter.AndExpr{Left: root("a1"), Right: typeTicket()},
			shortcutLast:  &filter.AndExpr{Left: typeTicket(), Right: root("a1")},
			want:          []string{"a", "a1", "b", "root"},
		},
	}

	for _, testCase := range cases {
		test.Run(testCase.name, func(subtest *testing.T) {
			gotFirst := runHierarchyFilter(subtest, store, testCase.shortcutFirst)
			gotLast := runHierarchyFilter(subtest, store, testCase.shortcutLast)

			if !reflect.DeepEqual(gotFirst, testCase.want) {
				subtest.Errorf("shortcut-first ids = %v, want %v", gotFirst, testCase.want)
			}

			if !reflect.DeepEqual(gotLast, testCase.want) {
				subtest.Errorf("shortcut-last ids = %v, want %v (AND must be commutative)", gotLast, testCase.want)
			}
		})
	}
}
