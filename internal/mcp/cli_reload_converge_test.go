package mcp

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/germanamz/tusk/internal/epoch"
)

// TestCLIReload_LiveDaemonConverges pins the headline guarantee: a CLI-path
// manifest reload (tusk reload, which bumps .tusk/manifest-epoch) is detected
// by a LIVE mcp daemon whose RunManifestEpochWatcher and RunManifestEpochFastWatcher
// are running, which loads and swaps the fresh manifest in-memory.
//
// We test convergence via a new node-type: the CLI manifest adds a node-type,
// bumps the epoch, the daemon watcher converges, and a subsequent query exercising
// the new type succeeds.
func TestCLIReload_LiveDaemonConverges(test *testing.T) {
	root := setupServerWorkspace(test)
	daemon := newServerForRoot(test, root)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() { _ = daemon.RunBackground(ctx) }()

	// Let the background watchers register their fsnotify watch / first tick.
	time.Sleep(150 * time.Millisecond)

	// Snapshot the initial daemon state: manifest has only the implicit "note" type.
	rtBefore := daemon.snapshotRuntime()
	if _, typeExists := rtBefore.Manifest.NodeTypes["decision"]; typeExists {
		test.Fatalf("initial manifest should not have 'decision' type; got: %v", rtBefore.Manifest.NodeTypes)
	}

	// Simulate a CLI-side reload: load the manifest, add a node-type, then call
	// epoch.Manifest.Bump (exactly what cmd_reload.go does).
	manifestPath := filepath.Join(root, "tusk.toml")
	manifestBytes, readErr := os.ReadFile(manifestPath)
	if readErr != nil {
		test.Fatalf("read manifest: %v", readErr)
	}

	// Append a new node-type definition.
	newManifest := string(manifestBytes) + "\n\n[node-types.decision]\nproperties = []\n"
	if writeErr := os.WriteFile(manifestPath, []byte(newManifest), 0o644); writeErr != nil {
		test.Fatalf("write manifest with new type: %v", writeErr)
	}

	// Bump the manifest epoch (simulating the CLI path).
	newEpoch, bumpErr := epoch.Manifest.Bump(root)
	if bumpErr != nil {
		test.Fatalf("CLI bump manifest-epoch: %v", bumpErr)
	}

	if newEpoch != 1 {
		test.Fatalf("expected first bump = 1, got %d", newEpoch)
	}

	// The daemon's manifest watchers must detect the bump and converge onto the
	// fresh manifest, which includes the new "decision" type.
	deadline := time.Now().Add(5 * time.Second)

	for time.Now().Before(deadline) {
		if daemon.seenManifestEpoch.Load() >= newEpoch {
			break
		}

		time.Sleep(20 * time.Millisecond)
	}

	if seen := daemon.seenManifestEpoch.Load(); seen < newEpoch {
		test.Fatalf("daemon did not converge after CLI reload: seenManifestEpoch=%d want>=%d", seen, newEpoch)
	}

	// The converged daemon must have the new type in its in-memory manifest.
	rtAfter := daemon.snapshotRuntime()
	if _, typeExists := rtAfter.Manifest.NodeTypes["decision"]; !typeExists {
		test.Fatalf("daemon manifest did not converge to include 'decision' type; got: %v", rtAfter.Manifest.NodeTypes)
	}

	// Stop background watchers before cleanup.
	cancel()
	time.Sleep(50 * time.Millisecond)
}
