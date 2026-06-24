package mcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/germanamz/tusk/internal/epoch"
	mcpgo "github.com/mark3labs/mcp-go/mcp"
)

func resetRequest(confirm bool) mcpgo.CallToolRequest {
	req := mcpgo.CallToolRequest{}
	req.Params.Name = "tusk_reset"
	req.Params.Arguments = map[string]any{"confirm": confirm}

	return req
}

func TestResetTool_RequiresConfirm(test *testing.T) {
	srv := buildTestServer(test) // from reset_helpers_test.go (Phase 5 Task 4)

	result, err := srv.HandleToolCall(context.Background(), resetRequest(false))
	if err != nil {
		test.Fatalf("HandleToolCall returned transport error: %v", err)
	}

	if result == nil || !result.IsError {
		test.Fatalf("expected an error result when confirm is false, got %+v", result)
	}

	// Assert the error is the CONFIRM gate, not the unknown-tool fallback — so the
	// red step is genuinely red before the tool is registered.
	if body := textOf(result); !strings.Contains(body, "confirm") {
		test.Fatalf("expected a confirm-gate error mentioning 'confirm', got: %s", body)
	}
}

func TestResetTool_SwapsAndKeepsServing(test *testing.T) {
	srv := buildTestServer(test) // workspace has one node on disk

	rt := srv.snapshotRuntime()
	root := rt.Root

	result, err := srv.HandleToolCall(context.Background(), resetRequest(true))
	if err != nil {
		test.Fatalf("reset transport error: %v", err)
	}

	if result.IsError {
		test.Fatalf("reset returned error result: %s", textOf(result))
	}

	// Async-guaranteed facts: epoch advanced, the daemon still serves a non-error
	// list against the FRESH handle (proves the swap worked and the DB is open).
	if onDisk, _ := epoch.Index.Read(root); onDisk != 1 {
		test.Fatalf("expected epoch 1 after reset, got %d", onDisk)
	}

	if listResult, listErr := srv.HandleToolCall(context.Background(), nodeListRequest()); listErr != nil || listResult.IsError {
		test.Fatalf("node list errored immediately after reset: err=%v result=%s", listErr, textOf(listResult))
	}

	// tusk_reset kicks only an Async walk (enqueues kind='reindex' jobs; it does
	// NOT write node/edge rows itself, and no background drainer runs in this
	// test). Drive a synchronous rebuild to materialize the structural rows, then
	// assert the node is back.
	rebuildIndex(test, srv)

	listResult, listErr := srv.HandleToolCall(context.Background(), nodeListRequest())
	if listErr != nil || listResult.IsError {
		test.Fatalf("node list after rebuild: err=%v result=%s", listErr, textOf(listResult))
	}

	if !strings.Contains(textOf(listResult), seededNodeID(test)) {
		test.Fatalf("rebuilt index missing the seeded node; got: %s", textOf(listResult))
	}
}

func TestResetTool_ConcurrentReadsAreSafe(test *testing.T) {
	srv := buildTestServer(test)

	var wg sync.WaitGroup

	// Fire many concurrent node_list reads while a reset swaps the handle.
	for range 20 {
		wg.Add(1)

		go func() {
			defer wg.Done()

			res, err := srv.HandleToolCall(context.Background(), nodeListRequest())
			if err != nil {
				test.Errorf("concurrent list transport error: %v", err)

				return
			}

			// A read may legitimately race a reset and see a transient empty
			// list, but it must NEVER surface a DB-closed/error result.
			if res.IsError {
				test.Errorf("concurrent list errored during reset: %s", textOf(res))
			}
		}()
	}

	wg.Add(1)

	go func() {
		defer wg.Done()

		if _, err := srv.HandleToolCall(context.Background(), resetRequest(true)); err != nil {
			test.Errorf("reset transport error: %v", err)
		}
	}()

	wg.Wait()
}

func TestResetTool_SummaryAndRepeat(test *testing.T) {
	srv := buildTestServer(test)

	first, err := srv.HandleToolCall(context.Background(), resetRequest(true))
	if err != nil || first.IsError {
		test.Fatalf("first reset failed: err=%v result=%s", err, textOf(first))
	}

	var payload struct {
		Indexed          int      `json:"indexed"`
		Removed          int      `json:"removed"`
		Skipped          int      `json:"skipped"`
		Epoch            int64    `json:"epoch"`
		DeletedArtifacts []string `json:"deleted_artifacts"`
	}

	if unmarshalErr := json.Unmarshal([]byte(textOf(first)), &payload); unmarshalErr != nil {
		test.Fatalf("reset summary is not JSON: %v (%s)", unmarshalErr, textOf(first))
	}

	if payload.Epoch != 1 {
		test.Errorf("expected epoch 1, got %d", payload.Epoch)
	}

	if len(payload.DeletedArtifacts) == 0 {
		test.Error("expected deleted_artifacts to list the dropped index file(s)")
	}

	// A second reset must succeed and advance the epoch to 2.
	second, err := srv.HandleToolCall(context.Background(), resetRequest(true))
	if err != nil || second.IsError {
		test.Fatalf("second reset failed: err=%v result=%s", err, textOf(second))
	}

	var second2 struct {
		Epoch int64 `json:"epoch"`
	}

	if unmarshalErr := json.Unmarshal([]byte(textOf(second)), &second2); unmarshalErr != nil {
		test.Fatalf("second reset summary not JSON: %v", unmarshalErr)
	}

	if second2.Epoch != 2 {
		test.Errorf("expected epoch 2 after repeated reset, got %d", second2.Epoch)
	}
}

func TestResetTool_UpdatesSeenEpoch(test *testing.T) {
	srv := buildTestServer(test)

	if _, err := srv.HandleToolCall(context.Background(), resetRequest(true)); err != nil {
		test.Fatalf("reset: %v", err)
	}

	// The resetting daemon must record its own bump so the epoch-watcher does
	// not treat it as a foreign reset.
	if seen := srv.seenEpoch.Load(); seen != 1 {
		test.Fatalf("expected seenEpoch 1 after own reset, got %d", seen)
	}
}

// TestResetTool_RecoveryReopenKeepsServing pins the handler's recovery branch:
// when reset.PerformLocked fails AFTER Quiesce has already closed the old handle,
// the handler best-effort reopens so the daemon keeps serving rather than being
// stranded on a closed DB. We inject a post-quiesce failure by making the
// .tusk/epoch sentinel a directory, so the epoch Bump (the last PerformLocked
// step) fails while the index file has already been recreated.
func TestResetTool_RecoveryReopenKeepsServing(test *testing.T) {
	srv := buildTestServer(test)
	root := srv.snapshotRuntime().Root

	if err := os.Mkdir(filepath.Join(root, ".tusk", "epoch"), 0o755); err != nil {
		test.Fatalf("inject epoch dir: %v", err)
	}

	result, err := srv.HandleToolCall(context.Background(), resetRequest(true))
	if err != nil {
		test.Fatalf("transport error: %v", err)
	}

	if result == nil || !result.IsError {
		test.Fatalf("expected reset to fail when the epoch bump is blocked, got %+v", result)
	}

	// The recovery-reopen must have left the daemon on a LIVE handle: a follow-up
	// read returns a non-error result, not "database is closed".
	listResult, listErr := srv.HandleToolCall(context.Background(), nodeListRequest())
	if listErr != nil {
		test.Fatalf("node list after recovered reset: %v", listErr)
	}

	if listResult.IsError {
		test.Fatalf("daemon left on a closed DB after a failed reset: %s", textOf(listResult))
	}
}
