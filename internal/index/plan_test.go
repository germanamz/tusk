package index

import (
	"path/filepath"
	"strings"
	"testing"
)

// The index carries no sqlite_stat1, so the planner can't tell a two- or
// three-value column (edges.kind, edges.type, nodes.kind) from a selective one
// and probed through the few-valued index, walking most of the table per probe
// (#838). These tests pin the plans of the statements that fell for it. Without
// stats the plan does not depend on the rows, so an empty index shows it.

// explainPlan returns the EXPLAIN QUERY PLAN detail lines of statement.
func explainPlan(test *testing.T, store *Index, statement string, args ...any) string {
	test.Helper()

	rows, queryErr := store.DB().Query(`EXPLAIN QUERY PLAN `+statement, args...)

	if queryErr != nil {
		test.Fatalf("EXPLAIN: %v\nSQL: %s", queryErr, statement)
	}

	defer rows.Close()

	var plan strings.Builder

	for rows.Next() {
		var selectID, order, from int

		var detail string

		if scanErr := rows.Scan(&selectID, &order, &from, &detail); scanErr != nil {
			test.Fatalf("scan plan: %v", scanErr)
		}

		plan.WriteString(detail + "\n")
	}

	return plan.String()
}

func openPlanIndex(test *testing.T) *Index {
	test.Helper()

	store, openErr := Open(filepath.Join(test.TempDir(), "index.db"))

	if openErr != nil {
		test.Fatalf("Open: %v", openErr)
	}

	test.Cleanup(func() { store.Close() })

	return store
}

// TestResolveLinks_TwinLookupsUseTheEdgeIdentity pins how ResolveLinks finds an
// edge's twin: through the (source_id, target_id) index. It probed through
// edges_kind_idx, so the dedupe was quadratic in link edges and a reset of a
// 3,300-note vault ran for half an hour.
func TestResolveLinks_TwinLookupsUseTheEdgeIdentity(test *testing.T) {
	store := openPlanIndex(test)

	for _, statement := range []struct {
		name string
		sql  string
		args []any
	}{
		{"dedupe", dedupeLinks, nil},
		{"park", parkLinks, nil},
		{"twin", countPathTwins, []any{"", "", "", "", ""}},
	} {
		plan := explainPlan(test, store, statement.sql, statement.args...)

		if !strings.Contains(plan, "edges_source_target_idx (source_id=? AND target_id=?)") {
			test.Errorf("%s does not look twins up by (source_id, target_id):\n%s", statement.name, plan)
		}
	}
}

// TestSubUnitLookups_ReadOnlyTheirFile pins that the reads of one file's
// sections and block ids walk that file's id range. They probed through
// nodes_kind_type_idx, which reads every section, or every sub-unit, in the
// vault on each call.
func TestSubUnitLookups_ReadOnlyTheirFile(test *testing.T) {
	store := openPlanIndex(test)

	for name, statement := range map[string]string{
		"sections":      sectionRows,
		"blocks":        blockRows,
		"section spans": sectionSpanRows,
	} {
		plan := explainPlan(test, store, statement, "", "")

		if !strings.Contains(plan, "(id>? AND id<?)") {
			test.Errorf("%s does not read the file's id range:\n%s", name, plan)
		}
	}
}

// TestOpen_EdgesHaveNoFewValuedIndex pins that no edges index leads with type
// or kind: a lookup by source_id or target_id that also names a type or kind
// (a sub-unit sync's DeleteBySourceAndType, every filter arrow) took the
// few-valued index, and a reset of a 3,300-note vault spent most of its time
// there.
func TestOpen_EdgesHaveNoFewValuedIndex(test *testing.T) {
	store := openPlanIndex(test)

	rows, queryErr := store.DB().Query(
		`SELECT list.name, info.name FROM pragma_index_list('edges') AS list
		 JOIN pragma_index_info(list.name) AS info WHERE info.seqno = 0`,
	)

	if queryErr != nil {
		test.Fatalf("list edges indexes: %v", queryErr)
	}

	defer rows.Close()

	for rows.Next() {
		var name, leading string

		if scanErr := rows.Scan(&name, &leading); scanErr != nil {
			test.Fatalf("scan index: %v", scanErr)
		}

		if leading == "type" || leading == "kind" {
			test.Errorf("index %s leads with edges.%s", name, leading)
		}
	}
}
