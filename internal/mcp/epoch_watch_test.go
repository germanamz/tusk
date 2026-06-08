package mcp

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/indexepoch"
)

// Sibling that finds the index file ABSENT (resetter crashed after delete)
// recreates it, reindexes from disk, and bumps the epoch.
func TestSiblingReopen_RecreatesWhenFileAbsent(test *testing.T) {
	srv := buildTestServer(test) // workspace has a node on disk
	rt := srv.snapshotRuntime()
	root, indexPath := rt.Root, rt.IndexPath

	// Realistic precondition: a reset advanced the epoch (that is what triggers
	// siblingReopen). seenEpoch is still 0, so the re-check inside siblingReopen
	// proceeds.
	bumped, _ := indexepoch.Bump(root)

	// Simulate a resetter that deleted the index and died before recreating.
	rt.Index.Close()

	if _, rmErr := index.RemoveArtifacts(indexPath); rmErr != nil {
		test.Fatalf("remove artifacts: %v", rmErr)
	}

	if err := srv.siblingReopen(context.Background(), 5*time.Second); err != nil {
		test.Fatalf("siblingReopen: %v", err)
	}

	// The recreator bumped the epoch again (beyond the triggering bump) and
	// seenEpoch followed.
	epoch, _ := indexepoch.Read(root)
	if epoch <= bumped || srv.seenEpoch.Load() != epoch {
		test.Fatalf("expected recreator to bump beyond %d and track it in seenEpoch; epoch=%d seen=%d", bumped, epoch, srv.seenEpoch.Load())
	}

	// The recreator kicked an Async rebuild (enqueues only). Drive it synchronously
	// so the structural rows materialize, then assert the node is rebuilt from disk.
	rebuildIndex(test, srv)

	listResult, _ := srv.HandleToolCall(context.Background(), nodeListRequest())
	if !strings.Contains(textOf(listResult), seededNodeID(test)) {
		test.Fatalf("recreator did not rebuild; got: %s", textOf(listResult))
	}
}

func TestMaybeReopenForEpoch(test *testing.T) {
	srv := buildTestServer(test)
	root := srv.runtime.Root

	// No epoch advance yet → no reopen.
	reopened, err := srv.maybeReopenForEpoch(context.Background(), 5*time.Second)
	if err != nil {
		test.Fatalf("maybeReopenForEpoch (no change): %v", err)
	}

	if reopened {
		test.Fatal("reopened despite unchanged epoch")
	}

	// Simulate a foreign reset: bump epoch out-of-band (another process would
	// have recreated the DB; here the existing file is still valid, so reopen
	// just re-points the handle).
	if _, bumpErr := indexepoch.Bump(root); bumpErr != nil {
		test.Fatalf("bump: %v", bumpErr)
	}

	reopened, err = srv.maybeReopenForEpoch(context.Background(), 5*time.Second)
	if err != nil {
		test.Fatalf("maybeReopenForEpoch (advanced): %v", err)
	}

	if !reopened {
		test.Fatal("expected a reopen after epoch advance")
	}

	if srv.seenEpoch.Load() != 1 {
		test.Fatalf("expected seenEpoch 1 after convergence, got %d", srv.seenEpoch.Load())
	}
}
