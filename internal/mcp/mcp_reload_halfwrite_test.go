package mcp

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/germanamz/tusk/internal/manifestepoch"
)

// TestMCPReload_HalfwriteRecovery pins the partial-write recovery (spec §8):
// a sibling watcher detects a manifest-epoch bump while the originating process
// is mid-write to tusk.toml. The sibling loads the file, sees a parse error,
// does NOT advance seenManifestEpoch, logs WARN, and retries on the next 2s tick
// until the file is valid.
func TestMCPReload_HalfwriteRecovery(test *testing.T) {
	root := setupServerWorkspace(test)
	daemon := newServerForRoot(test, root)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() { _ = daemon.RunBackground(ctx) }()

	time.Sleep(150 * time.Millisecond)

	// Write a half-broken manifest (missing closing quotes on a string).
	manifestPath := filepath.Join(root, "tusk.toml")
	brokenManifest := `[workspace]
name = "test
`

	if writeErr := os.WriteFile(manifestPath, []byte(brokenManifest), 0o644); writeErr != nil {
		test.Fatalf("write broken manifest: %v", writeErr)
	}

	// Bump the manifest-epoch while the file is broken (simulating the originator
	// bumping before the write completes — a race condition this gate recovers from).
	bumpedEpoch, bumpErr := manifestepoch.Bump(root)
	if bumpErr != nil {
		test.Fatalf("bump while broken: %v", bumpErr)
	}

	// Wait a moment for the watcher to attempt a read.
	time.Sleep(200 * time.Millisecond)

	// Assert the daemon did NOT advance seenManifestEpoch (gate-on-success).
	if seen := daemon.seenManifestEpoch.Load(); seen >= bumpedEpoch {
		test.Fatalf("daemon should not have advanced seenManifestEpoch on a broken file; got %d", seen)
	}

	// Now fix the manifest.
	fixedManifest := `[workspace]
name = "test"
`

	if writeErr := os.WriteFile(manifestPath, []byte(fixedManifest), 0o644); writeErr != nil {
		test.Fatalf("write fixed manifest: %v", writeErr)
	}

	// The daemon's 2s tick must eventually retry and converge.
	deadline := time.Now().Add(5 * time.Second)

	for time.Now().Before(deadline) {
		if daemon.seenManifestEpoch.Load() >= bumpedEpoch {
			break
		}

		time.Sleep(20 * time.Millisecond)
	}

	if seen := daemon.seenManifestEpoch.Load(); seen < bumpedEpoch {
		test.Fatalf("daemon should have converged after file was fixed; seenManifestEpoch=%d want>=%d", seen, bumpedEpoch)
	}

	cancel()
	time.Sleep(50 * time.Millisecond)
}
