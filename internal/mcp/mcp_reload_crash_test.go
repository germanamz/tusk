package mcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
)

// TestMCPReload_CrashRecovery pins crash recovery (spec §8): when an originating
// reload crashes after bumping the manifest-epoch but before the async reindex
// completes, siblings still converge to the fresh manifest (schema consistent),
// and the lagging index is repaired by a subsequent reindex tick (or manual tusk_reindex).
func TestMCPReload_CrashRecovery(test *testing.T) {
	root := setupServerWorkspace(test)

	// Start daemon A (originating) and daemon B (sibling).
	daemonA := newServerForRoot(test, root)
	daemonB := newServerForRoot(test, root)

	ctxA, cancelA := context.WithCancel(context.Background())
	defer cancelA()

	ctxB, cancelB := context.WithCancel(context.Background())
	defer cancelB()

	go func() { _ = daemonA.RunBackground(ctxA) }()
	go func() { _ = daemonB.RunBackground(ctxB) }()

	time.Sleep(150 * time.Millisecond)

	// Prepare the manifest with a new type.
	manifestPath := filepath.Join(root, "tusk.toml")
	manifestBytes, readErr := os.ReadFile(manifestPath)
	if readErr != nil {
		test.Fatalf("read manifest: %v", readErr)
	}

	newManifest := string(manifestBytes) + "\n\n[node-types.decision]\nproperties = []\n"
	if writeErr := os.WriteFile(manifestPath, []byte(newManifest), 0o644); writeErr != nil {
		test.Fatalf("write manifest: %v", writeErr)
	}

	// Daemon A calls tusk_reload.
	reloadReq := mcpgo.CallToolRequest{}
	reloadReq.Params.Name = "tusk_reload"
	reloadReq.Params.Arguments = map[string]any{"no_reindex": false, "no_embed": false}

	reloadResult, reloadErr := daemonA.HandleToolCall(context.Background(), reloadReq)
	if reloadErr != nil {
		test.Fatalf("tusk_reload: %v", reloadErr)
	}

	if reloadResult.IsError {
		test.Fatalf("tusk_reload failed: %s", textOf(reloadResult))
	}

	var reloadPayload struct {
		ManifestEpoch int64 `json:"manifest_epoch"`
	}

	if unmarshalErr := json.Unmarshal([]byte(textOf(reloadResult)), &reloadPayload); unmarshalErr != nil {
		test.Fatalf("unmarshal reload response: %v", unmarshalErr)
	}

	newEpoch := reloadPayload.ManifestEpoch

	// Simulate daemon A crashing BEFORE async reindex completes: cancel its
	// background context (stopping drainers) but leave daemon B running.
	cancelA()
	time.Sleep(100 * time.Millisecond)

	// Daemon B must converge to the fresh manifest (schema consistent).
	deadline := time.Now().Add(5 * time.Second)

	for time.Now().Before(deadline) {
		if daemonB.seenManifestEpoch.Load() >= newEpoch {
			break
		}

		time.Sleep(20 * time.Millisecond)
	}

	if seen := daemonB.seenManifestEpoch.Load(); seen < newEpoch {
		test.Fatalf("sibling B should converge manifest after originator A crashes; seenManifestEpoch=%d want>=%d", seen, newEpoch)
	}

	// Daemon B's manifest includes the new type (schema consistent).
	rtBAfter := daemonB.snapshotRuntime()
	if _, typeExists := rtBAfter.Manifest.NodeTypes["decision"]; !typeExists {
		test.Fatalf("sibling B should have the new type after convergence; got: %v", rtBAfter.Manifest.NodeTypes)
	}

	// Now verify that a manual reindex (or watcher-driven reindex from B) would
	// repair the stale index. Daemon B's index is unchanged (A crashed before
	// reindex), but the schema is fresh. Rebuild to assert content indexing still works.
	rebuildIndex(test, daemonB)

	listResult, listErr := daemonB.HandleToolCall(context.Background(), nodeListRequest())
	if listErr != nil || listResult.IsError {
		test.Fatalf("node list after rebuild: err=%v result=%s", listErr, textOf(listResult))
	}

	// Cleanup.
	cancelB()
	time.Sleep(50 * time.Millisecond)
}
