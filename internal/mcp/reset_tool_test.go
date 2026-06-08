package mcp

import (
	"context"
	"strings"
	"testing"

	"github.com/germanamz/tusk/internal/indexepoch"
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
	if epoch, _ := indexepoch.Read(root); epoch != 1 {
		test.Fatalf("expected epoch 1 after reset, got %d", epoch)
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
