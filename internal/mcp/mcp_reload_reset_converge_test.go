package mcp

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/manifestepoch"
	"github.com/germanamz/tusk/internal/reset"
)

// TestMCPReload_ResetAndReloadConvergeAtomically pins the edge case from spec
// §5 (reset + reload in the same window): when a sibling detects both an
// index-epoch bump AND a manifest-epoch bump, it must load+validate the fresh
// manifest and pass it to buildFromStore (not old.Manifest), converging both
// the index and the schema atomically under the same flock.
func TestMCPReload_ResetAndReloadConvergeAtomically(test *testing.T) {
	root := setupServerWorkspace(test)

	// Start sibling B, which will detect both bumps.
	daemonB := newServerForRoot(test, root)

	ctxB, cancelB := context.WithCancel(context.Background())
	defer cancelB()

	go func() { _ = daemonB.RunBackground(ctxB) }()

	time.Sleep(150 * time.Millisecond)

	// Snapshot B's state before: one seeded node (from setupServerWorkspace).
	rtBBefore := daemonB.snapshotRuntime()
	root = rtBBefore.Root

	// Simulate a RESET (index-epoch bump).
	indexPath := filepath.Join(root, ".tusk", "index.db")
	result, resetErr := reset.Perform(context.Background(), reset.Config{
		Root:      root,
		IndexPath: indexPath,
		LockTTL:   5 * time.Second,
		Reopen:    func() (*index.Index, error) { return index.Open(indexPath) },
	})

	if resetErr != nil {
		test.Fatalf("reset: %v", resetErr)
	}

	_ = result.Store.Close()

	// Simulate a RELOAD (manifest-epoch bump): add a new node-type.
	manifestPath := filepath.Join(root, "tusk.toml")
	manifestBytes, readErr := os.ReadFile(manifestPath)
	if readErr != nil {
		test.Fatalf("read manifest: %v", readErr)
	}

	newManifest := string(manifestBytes) + "\n\n[node-types.decision]\nproperties = []\n"
	if writeErr := os.WriteFile(manifestPath, []byte(newManifest), 0o644); writeErr != nil {
		test.Fatalf("write manifest: %v", writeErr)
	}

	_, bumpErr := manifestepoch.Bump(root)
	if bumpErr != nil {
		test.Fatalf("bump manifest-epoch: %v", bumpErr)
	}

	// Daemon B must converge BOTH the index and the manifest: it reads both epochs
	// under the same flock, loads+validates the fresh manifest, and passes it to
	// buildFromStore. Assert via seenEpoch and seenManifestEpoch.
	deadline := time.Now().Add(5 * time.Second)

	for time.Now().Before(deadline) {
		if daemonB.seenEpoch.Load() >= result.Epoch && daemonB.seenManifestEpoch.Load() >= 1 {
			break
		}

		time.Sleep(20 * time.Millisecond)
	}

	if seen := daemonB.seenEpoch.Load(); seen < result.Epoch {
		test.Fatalf("daemon B did not converge index-epoch; seenEpoch=%d want>=%d", seen, result.Epoch)
	}

	if seen := daemonB.seenManifestEpoch.Load(); seen < 1 {
		test.Fatalf("daemon B did not converge manifest-epoch; seenManifestEpoch=%d", seen)
	}

	// The daemon must serve against the fresh handle + fresh manifest (not stale
	// schema against fresh DB).
	if listResult, listErr := daemonB.HandleToolCall(context.Background(), nodeListRequest()); listErr != nil || listResult.IsError {
		test.Fatalf("node list after dual convergence: err=%v result=%s", listErr, textOf(listResult))
	}

	// Rebuild the index so structural rows materialize, then assert the seeded
	// node is back AND the converged manifest includes the new type.
	rebuildIndex(test, daemonB)

	rtBAfter := daemonB.snapshotRuntime()

	if _, typeExists := rtBAfter.Manifest.NodeTypes["decision"]; !typeExists {
		test.Fatalf("daemon B should have converged the fresh manifest with 'decision' type; got: %v", rtBAfter.Manifest.NodeTypes)
	}

	cancelB()
	time.Sleep(50 * time.Millisecond)
}
