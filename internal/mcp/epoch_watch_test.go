package mcp

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/indexepoch"
	"github.com/germanamz/tusk/internal/lock"
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

func TestTwoDaemons_ConvergeAfterReset(test *testing.T) {
	root := setupServerWorkspace(test) // a workspace dir with a node on disk; see helper note

	daemonA := newServerForRoot(test, root)
	daemonB := newServerForRoot(test, root)

	// Daemon A resets the shared index.
	if _, err := daemonA.HandleToolCall(context.Background(), resetRequest(true)); err != nil {
		test.Fatalf("A reset: %v", err)
	}

	// Daemon B detects the epoch advance and reopens onto the fresh DB.
	reopened, err := daemonB.maybeReopenForEpoch(context.Background(), 5*time.Second)
	if err != nil {
		test.Fatalf("B converge: %v", err)
	}

	if !reopened {
		test.Fatal("B did not reopen after A's reset")
	}

	// B must serve a non-error list against the fresh handle (proves convergence).
	if listResult, listErr := daemonB.HandleToolCall(context.Background(), nodeListRequest()); listErr != nil || listResult.IsError {
		test.Fatalf("B list after convergence failed: err=%v result=%s", listErr, textOf(listResult))
	}

	// Neither daemon runs a drainer in this test, so A's Async walk left the fresh
	// DB with only enqueued reindex jobs. Drive a synchronous rebuild through B and
	// assert the node materializes (the lease-coordinated queues make the
	// resetter+sibling walks idempotent in production).
	rebuildIndex(test, daemonB)

	listResult, listErr := daemonB.HandleToolCall(context.Background(), nodeListRequest())
	if listErr != nil || listResult.IsError {
		test.Fatalf("B list after rebuild failed: err=%v result=%s", listErr, textOf(listResult))
	}

	if !strings.Contains(textOf(listResult), seededNodeID(test)) {
		test.Fatalf("B missing rebuilt node; got: %s", textOf(listResult))
	}
}

func TestSiblingReopen_HungResetterReturnsBusy(test *testing.T) {
	srv := buildTestServer(test)
	root := srv.snapshotRuntime().Root

	// Realistic precondition: a hung resetter bumped the epoch (recreate+bump
	// happen before it hangs holding the flock), so the re-check inside
	// siblingReopen proceeds to the flock-await.
	if _, err := indexepoch.Bump(root); err != nil {
		test.Fatalf("bump: %v", err)
	}

	// Simulate a hung-but-alive resetter holding the flock.
	holder, _ := lock.NewWorkspaceLock(root)
	holdCtx, holdCancel := context.WithTimeout(context.Background(), 2*time.Second)

	defer holdCancel()

	if acquireErr := holder.Acquire(holdCtx); acquireErr != nil {
		test.Fatalf("holder acquire: %v", acquireErr)
	}

	err := srv.siblingReopen(context.Background(), 200*time.Millisecond)
	if !errors.Is(err, lock.ErrBusy) {
		test.Fatalf("expected ErrBusy while resetter holds the flock, got %v", err)
	}

	// The daemon kept its handle and still serves.
	if listResult, listErr := srv.HandleToolCall(context.Background(), nodeListRequest()); listErr != nil || listResult.IsError {
		test.Fatalf("daemon stopped serving after busy reopen: err=%v result=%s", listErr, textOf(listResult))
	}

	// Resetter "dies": release the flock. Now a reopen succeeds (takeover).
	_ = holder.Release()

	if reopenErr := srv.siblingReopen(context.Background(), 5*time.Second); reopenErr != nil {
		test.Fatalf("takeover reopen after flock freed: %v", reopenErr)
	}
}
