package filter_test

import (
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/germanamz/tusk/internal/filter"
	"github.com/germanamz/tusk/internal/index"
)

// edgeProbe matches a query-plan line that reads the edges table, under its
// own name (the tree=/root= CTEs) or an e<depth> alias (edge predicates).
var edgeProbe = regexp.MustCompile(`^(SEARCH|SCAN) (edges|e\d+)\b`)

// TestCompile_EdgeLookupsProbeByTheirNode pins that every edge lookup a filter
// compiles to is anchored at its node: the plan probes edges by source_id or
// target_id, never by type, kind or namespace alone. The index carries no
// sqlite_stat1, and the planner used to probe through edges_type_idx, or the
// (source, type) prefix of the edges UNIQUE index for a namespaced type: two
// or three values, so every row of the outer query walked most of the edges
// table. `type=note references->type=note` took 55 s on a 3,300-note vault
// (#838). Without stats the plan does not depend on the rows, so an empty index
// shows it.
func TestCompile_EdgeLookupsProbeByTheirNode(test *testing.T) {
	store, openErr := index.Open(filepath.Join(test.TempDir(), "index.db"))

	if openErr != nil {
		test.Fatalf("open index: %v", openErr)
	}

	defer store.Close()

	cases := map[string]filter.Expr{
		"tree=":   &filter.TraversalShortcut{Kind: filter.ShortcutTree, NodeID: "root", EdgeType: "parent"},
		"root=":   &filter.TraversalShortcut{Kind: filter.ShortcutRoot, NodeID: "a1", EdgeType: "parent"},
		"parent=": &filter.TraversalShortcut{Kind: filter.ShortcutParentOf, NodeID: "root", EdgeType: "parent"},
	}

	for _, source := range []string{
		"type=note references->type=note",
		"type=note references<-type=note",
		"type=note user:references->type=note",
		"type=note user:references<-type=note",
		"type=note markdown:contains->type=note",
		"type=note markdown:contains<-type=note",
	} {
		expr, parseErrs := filter.NewParser(source).Parse()

		if len(parseErrs) > 0 {
			test.Fatalf("parse %q: %v", source, parseErrs)
		}

		cases[source] = expr
	}

	for name, expr := range cases {
		sqlText, params, compileErr := filter.Compile(expr, filter.CompileOptions{})

		if compileErr != nil {
			test.Fatalf("%s: Compile: %v", name, compileErr)
		}

		rows, queryErr := store.DB().Query(`EXPLAIN QUERY PLAN `+sqlText, params...)

		if queryErr != nil {
			test.Fatalf("%s: EXPLAIN: %v\nSQL: %s", name, queryErr, sqlText)
		}

		var plan, misses []string

		for rows.Next() {
			var selectID, order, from int

			var detail string

			if scanErr := rows.Scan(&selectID, &order, &from, &detail); scanErr != nil {
				test.Fatalf("%s: scan plan: %v", name, scanErr)
			}

			plan = append(plan, detail)

			if edgeProbe.MatchString(detail) && !strings.Contains(detail, "(source_id=?") && !strings.Contains(detail, "(target_id=?") {
				misses = append(misses, detail)
			}
		}

		rows.Close()

		if len(misses) > 0 {
			test.Errorf("%s: edges probed by something other than its node: %v\nplan:\n%s", name, misses, strings.Join(plan, "\n"))
		}
	}
}
