package filter_test

import (
	"database/sql"
	"path/filepath"
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
