package subunit_test

import (
	"context"
	"database/sql"
	"testing"
)

// A unit a `^block-id` marker names carries the id's block key, which a block
// reference is matched on (#819). Renaming the id turns the row over without
// a re-embed, since the marker is not part of what the unit embeds.
func TestSync_BlockRowsCarryTheirBlockKey(test *testing.T) {
	store := openSyncTestIndex(test)
	sync, nodes, _, queue := newSync(store, referencesManifest(test))

	parent := seedFileRow(test, nodes, "notes/blocks", "notes/blocks.md")

	if _, applyErr := sync.ApplyFile(context.Background(), parent, parseWithLines(test, "Quoted. ^Quote-1\n\nPlain.\n")); applyErr != nil {
		test.Fatalf("ApplyFile: %v", applyErr)
	}

	if quoted, _ := nodes.Get("notes/blocks#P1"); quoted == nil || quoted.NameKey != (sql.NullString{String: "^quote-1", Valid: true}) {
		test.Fatalf("quoted row = %+v, want name_key ^quote-1", quoted)
	}

	if plain, _ := nodes.Get("notes/blocks#P2"); plain == nil || plain.NameKey.Valid {
		test.Errorf("plain row = %+v, want a NULL name_key", plain)
	}

	drainEmbedQueue(test, queue)

	result, applyErr := sync.ApplyFile(context.Background(), parent, parseWithLines(test, "Quoted. ^renamed\n\nPlain.\n"))

	if applyErr != nil {
		test.Fatalf("ApplyFile again: %v", applyErr)
	}

	if result.Reordered != 1 {
		test.Errorf("Reordered = %d, want the renamed block turned over", result.Reordered)
	}

	if renamed, _ := nodes.Get("notes/blocks#P1"); renamed.NameKey.String != "^renamed" {
		test.Errorf("renamed name_key = %+v", renamed.NameKey)
	}

	if depth, _ := queue.Depth(); depth != 0 {
		test.Errorf("renaming a block id re-enqueued %d embeds, want 0", depth)
	}
}
