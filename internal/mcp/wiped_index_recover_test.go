package mcp

import (
	"context"
	"strings"
	"testing"

	"github.com/germanamz/tusk/internal/index"
	mcpgo "github.com/mark3labs/mcp-go/mcp"
)

// reindexRequest builds a tusk_reindex call. no_embed keeps it a fast,
// structural-only pass — the recovery under test is structural, not embedding.
func reindexRequest() mcpgo.CallToolRequest {
	req := mcpgo.CallToolRequest{}
	req.Params.Name = "tusk_reindex"
	req.Params.Arguments = map[string]any{"no_embed": true}

	return req
}

// dropAllTables strands idx's cached handle on a table-less database, mirroring
// the #705 incident where an out-of-band wipe left index.db an empty,
// table-less file while the long-running daemon kept serving off its open
// handle. Dropping every table through the live handle reproduces the exact
// observable state (SQLite schema changes are visible across the whole pool):
// subsequent queries surface a raw "no such table".
func dropAllTables(test *testing.T, idx *index.Index) {
	test.Helper()

	tables, listErr := idx.ListTables()

	if listErr != nil {
		test.Fatalf("list tables: %v", listErr)
	}

	if len(tables) == 0 {
		test.Fatal("precondition failed: index already table-less")
	}

	for _, name := range tables {
		if _, execErr := idx.DB().Exec("DROP TABLE IF EXISTS " + name); execErr != nil {
			test.Fatalf("drop table %s: %v", name, execErr)
		}
	}
}

// TestTool_Reindex_RecoversWipedIndex pins #705 Defect B: once the on-disk index
// is wiped table-less out of band, the daemon is stranded on its cached handle
// and every call surfaces a raw "no such table: meta" from reindex_gen's bump.
// tusk_reindex must self-heal — reopen (which re-bootstraps the schema, exactly
// like the CLI's fresh open) and rebuild — instead of forcing the destructive
// tusk_reset as the only recovery.
func TestTool_Reindex_RecoversWipedIndex(test *testing.T) {
	srv := buildTestServer(test)

	dropAllTables(test, srv.snapshotRuntime().Index)

	result, callErr := srv.HandleToolCall(context.Background(), reindexRequest())

	if callErr != nil {
		test.Fatalf("tusk_reindex after wipe: %v", callErr)
	}

	if result.IsError {
		test.Fatalf("tusk_reindex did not recover a wiped index (still stranded): %s", textOf(result))
	}

	// The seeded node is queryable again through the healed handle.
	listResult, listErr := srv.HandleToolCall(context.Background(), nodeListRequest())

	if listErr != nil || listResult.IsError {
		test.Fatalf("node list after recovery: err=%v result=%s", listErr, textOf(listResult))
	}

	if !strings.Contains(textOf(listResult), seededNodeID(test)) {
		test.Fatalf("recovered index missing seeded node %q: %s", seededNodeID(test), textOf(listResult))
	}
}
