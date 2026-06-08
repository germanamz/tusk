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

// TestMCPReload_SiblingConvergesManifestOnly pins locked decision #4: when
// daemon A calls tusk_reload (originating path), it owns the reindex. Sibling
// daemon B detects the manifest-epoch bump and reloads its manifest ONLY, never
// reindexing.
//
// The sibling-never-reindexes guarantee (locked decision #4) is unit-proven by
// TestSiblingReloadManifest_NeverReindexes; here A and B share one index.db so
// reindex_gen is not a clean per-daemon signal.
func TestMCPReload_SiblingConvergesManifestOnly(test *testing.T) {
	root := setupServerWorkspace(test)

	// Start two daemons sharing the same workspace.
	daemonA := newServerForRoot(test, root)
	daemonB := newServerForRoot(test, root)

	ctxA, cancelA := context.WithCancel(context.Background())
	defer cancelA()

	ctxB, cancelB := context.WithCancel(context.Background())
	defer cancelB()

	go func() { _ = daemonA.RunBackground(ctxA) }()
	go func() { _ = daemonB.RunBackground(ctxB) }()

	time.Sleep(150 * time.Millisecond)

	// Snapshot B's initial manifest: no "decision" type.
	rtBBefore := daemonB.snapshotRuntime()
	if _, typeExists := rtBBefore.Manifest.NodeTypes["decision"]; typeExists {
		test.Fatalf("daemon B initial manifest should not have 'decision' type")
	}

	// Daemon A calls tusk_reload (originating path): bump manifest, kick reindex.
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
		test.Fatalf("daemon A tusk_reload transport error: %v", reloadErr)
	}

	if reloadResult.IsError {
		test.Fatalf("daemon A tusk_reload returned error: %s", textOf(reloadResult))
	}

	var reloadPayload struct {
		ManifestEpoch int64 `json:"manifest_epoch"`
		Diff          struct {
			NodeTypes struct {
				Added   []string `json:"added"`
				Removed []string `json:"removed"`
			} `json:"node_types"`
		} `json:"diff"`
		Reindex struct {
			Kicked bool `json:"kicked"`
		} `json:"reindex"`
	}

	if unmarshalErr := json.Unmarshal([]byte(textOf(reloadResult)), &reloadPayload); unmarshalErr != nil {
		test.Fatalf("tusk_reload response not JSON: %v (%s)", unmarshalErr, textOf(reloadResult))
	}

	if reloadPayload.ManifestEpoch != 1 {
		test.Fatalf("expected manifest_epoch 1, got %d", reloadPayload.ManifestEpoch)
	}

	if !reloadPayload.Reindex.Kicked {
		test.Fatalf("expected reindex to be kicked=true")
	}

	if len(reloadPayload.Diff.NodeTypes.Added) == 0 || reloadPayload.Diff.NodeTypes.Added[0] != "decision" {
		test.Fatalf("expected diff.node_types.added to include 'decision'; got: %v", reloadPayload.Diff.NodeTypes.Added)
	}

	// Daemon A must record its own bump.
	if seen := daemonA.seenManifestEpoch.Load(); seen != 1 {
		test.Fatalf("daemon A should record its own reload; seenManifestEpoch=%d, want 1", seen)
	}

	// Daemon B's watcher must detect the bump and converge its manifest.
	deadline := time.Now().Add(5 * time.Second)

	for time.Now().Before(deadline) {
		if daemonB.seenManifestEpoch.Load() >= 1 {
			break
		}

		time.Sleep(20 * time.Millisecond)
	}

	if seen := daemonB.seenManifestEpoch.Load(); seen < 1 {
		test.Fatalf("daemon B did not converge manifest-epoch; seenManifestEpoch=%d", seen)
	}

	// Daemon B must have the new type in its in-memory manifest.
	rtBAfter := daemonB.snapshotRuntime()
	if _, typeExists := rtBAfter.Manifest.NodeTypes["decision"]; !typeExists {
		test.Fatalf("daemon B manifest did not converge to include 'decision' type")
	}

	// Cleanup.
	cancelA()
	cancelB()
	time.Sleep(50 * time.Millisecond)
}
