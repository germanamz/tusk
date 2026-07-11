package mcp_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/germanamz/tusk/internal/mcp"
)

// edgeDerivationMarkerKey mirrors the unexported reindex constant; the daemon
// stamps it in meta at the end of every reindex.Run pass. Kept in sync by hand
// (the value is asserted only for non-emptiness, so a bump does not break this).
const edgeDerivationMarkerKey = "edge_derivation_version"

// TestRunBackground_ReindexesAtBoot pins issue #681 finding #5: the MCP daemon
// must run reindex.Run's marker check at boot — not only on the first filesystem
// event — so an upgraded binary heals a pre-upgrade index (and indexes any
// content that changed while it was down) without a manual step.
//
// mcp.Open opens a fresh index WITHOUT reindexing (OpenOrRebuild reindexes only
// on a schema mismatch), so the pre-existing doc is unindexed and the
// edge-derivation marker is empty at boot. Only reindex.Run stamps that marker,
// and nothing but the file watcher runs it — and the watcher reacts to events,
// of which there are none here. So a stamped marker + an indexed doc after boot,
// with no filesystem event, proves the boot pass ran.
func TestRunBackground_ReindexesAtBoot(test *testing.T) {
	root := test.TempDir()

	if writeErr := os.WriteFile(filepath.Join(root, "tusk.toml"), []byte(`[workspace]
name = "x"

[node-types.note]
properties = []
`), 0o644); writeErr != nil {
		test.Fatalf("write manifest: %v", writeErr)
	}

	if mkErr := os.MkdirAll(filepath.Join(root, "notes"), 0o755); mkErr != nil {
		test.Fatalf("mkdir: %v", mkErr)
	}

	if writeErr := os.WriteFile(filepath.Join(root, "notes/doc.md"), []byte("---\ntype: note\n---\n# Doc\n\nSection body.\n"), 0o644); writeErr != nil {
		test.Fatalf("write doc: %v", writeErr)
	}

	rt, openErr := mcp.Open(root)

	if openErr != nil {
		test.Fatalf("Open: %v", openErr)
	}

	defer rt.Close()

	if rt.Workers < 1 {
		test.Fatalf("rt.Workers = %d, want >= 1", rt.Workers)
	}

	// Precondition: fresh index, so the marker is empty and the doc is unindexed.
	if marker, _ := rt.Meta.Get(edgeDerivationMarkerKey); marker != "" {
		test.Fatalf("precondition: expected empty edge-derivation marker on a fresh index, got %q", marker)
	}

	srv := mcp.NewServer(rt)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)

	go func() {
		done <- srv.RunBackground(ctx)
	}()

	// No filesystem event is ever driven; the boot pass alone must stamp the
	// marker and (via the reindex drainer) index the pre-existing doc.
	markerStamped := false
	docIndexed := false

	deadline := time.Now().Add(15 * time.Second)

	for time.Now().Before(deadline) {
		if !markerStamped {
			if marker, _ := rt.Meta.Get(edgeDerivationMarkerKey); marker != "" {
				markerStamped = true
			}
		}

		if !docIndexed {
			if _, getErr := rt.Nodes.Get("notes/doc"); getErr == nil {
				docIndexed = true
			}
		}

		if markerStamped && docIndexed {
			break
		}

		time.Sleep(100 * time.Millisecond)
	}

	cancel()

	select {
	case runErr := <-done:
		if runErr != nil && runErr != context.Canceled {
			test.Fatalf("RunBackground: %v", runErr)
		}
	case <-time.After(5 * time.Second):
		test.Fatalf("RunBackground did not return after cancel")
	}

	if !markerStamped {
		test.Errorf("edge-derivation marker was not stamped at boot (no reindex.Run ran)")
	}

	if !docIndexed {
		test.Errorf("pre-existing notes/doc was not indexed at boot")
	}
}
