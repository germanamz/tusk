package mcp

import (
	"context"
	"os"
	"testing"
)

func TestReopenInPlace_KeepsServing(test *testing.T) {
	srv := buildTestServer(test)

	before, beforeErr := srv.HandleToolCall(context.Background(), nodeListRequest())

	if beforeErr != nil {
		test.Fatalf("list before: %v", beforeErr)
	}

	if reopenErr := srv.reopenInPlace(); reopenErr != nil {
		test.Fatalf("reopenInPlace: %v", reopenErr)
	}

	after, afterErr := srv.HandleToolCall(context.Background(), nodeListRequest())

	if afterErr != nil {
		test.Fatalf("list after reopen: %v", afterErr)
	}

	if textOf(before) != textOf(after) {
		test.Fatalf("node list changed across a non-destructive reopen:\nbefore=%s\nafter=%s", textOf(before), textOf(after))
	}
}

// TestReopenInPlace_FailedReopenKeepsOldHandle pins that a reopen whose
// index.Open fails leaves the OLD (working) handle installed — the server keeps
// serving rather than being left on a closed DB. This guards the open-then-close
// ordering that Phase 6 (tusk_reset) and Phase 7 (siblingReopen) build on.
func TestReopenInPlace_FailedReopenKeepsOldHandle(test *testing.T) {
	srv := buildTestServer(test)

	before, beforeErr := srv.HandleToolCall(context.Background(), nodeListRequest())

	if beforeErr != nil {
		test.Fatalf("list before: %v", beforeErr)
	}

	// Inject an open failure: unlink the db file (the live handle keeps the
	// inode) and put a directory in its place so the reopen's index.Open fails.
	indexPath := srv.snapshotRuntime().IndexPath

	if rmErr := os.Remove(indexPath); rmErr != nil {
		test.Fatalf("remove index: %v", rmErr)
	}

	if mkErr := os.Mkdir(indexPath, 0o755); mkErr != nil {
		test.Fatalf("mkdir blocker: %v", mkErr)
	}

	if reopenErr := srv.reopenInPlace(); reopenErr == nil {
		test.Fatal("expected reopenInPlace to fail when the index path is unopenable")
	}

	// The server must still be serving the OLD handle, not a closed DB.
	after, afterErr := srv.HandleToolCall(context.Background(), nodeListRequest())

	if afterErr != nil {
		test.Fatalf("list after failed reopen: %v", afterErr)
	}

	if after.IsError {
		test.Fatalf("server left on a closed DB after a failed reopen: %s", textOf(after))
	}

	if textOf(before) != textOf(after) {
		test.Fatalf("data changed after a failed reopen:\nbefore=%s\nafter=%s", textOf(before), textOf(after))
	}
}
