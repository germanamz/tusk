package mcp

import (
	"testing"

	"github.com/germanamz/tusk/internal/manifestepoch"
)

// Test that NewServer initializes seenManifestEpoch from the persisted sentinel.
func TestNewServer_InitializesSeenManifestEpoch(test *testing.T) {
	root := setupServerWorkspace(test)

	// Manually bump manifest-epoch to 5, simulating a prior reload.
	for idx := 0; idx < 5; idx++ {
		if _, err := manifestepoch.Bump(root); err != nil {
			test.Fatalf("Bump: %v", err)
		}
	}

	// Open a fresh daemon.
	rt, err := Open(root)
	if err != nil {
		test.Fatalf("Open: %v", err)
	}
	defer rt.Close()

	srv := NewServer(rt)

	// seenManifestEpoch must have been initialized to 5, not 0.
	if got := srv.seenManifestEpoch.Load(); got != 5 {
		test.Fatalf("expected seenManifestEpoch=5, got %d", got)
	}
}
