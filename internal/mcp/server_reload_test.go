package mcp

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

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

// Test that maybeReloadManifestForEpoch returns false and makes no change when
// manifest-epoch has not advanced.
func TestMaybeReloadManifestForEpoch_NoChangeWhenNotAdvanced(test *testing.T) {
	srv := buildTestServer(test)

	// No epoch bump → no reload expected.
	reloaded, err := srv.maybeReloadManifestForEpoch(context.Background(), 5*time.Second)
	if err != nil {
		test.Fatalf("maybeReloadManifestForEpoch: %v", err)
	}

	if reloaded {
		test.Fatal("expected no reload when epoch is unchanged")
	}

	if got := srv.seenManifestEpoch.Load(); got != 0 {
		test.Fatalf("expected seenManifestEpoch=0, got %d", got)
	}
}

// Test that maybeReloadManifestForEpoch calls siblingReloadManifest when
// manifest-epoch has advanced.
func TestMaybeReloadManifestForEpoch_CallsSiblingWhenAdvanced(test *testing.T) {
	srv := buildTestServer(test)
	root := srv.runtime.Root

	// Bump manifest-epoch out-of-band (simulating a prior tusk_reload by another
	// process or a prior CLI tusk reload).
	if _, err := manifestepoch.Bump(root); err != nil {
		test.Fatalf("Bump: %v", err)
	}

	reloaded, err := srv.maybeReloadManifestForEpoch(context.Background(), 5*time.Second)
	if err != nil {
		test.Fatalf("maybeReloadManifestForEpoch: %v", err)
	}

	if !reloaded {
		test.Fatal("expected reload after epoch advance")
	}

	// seenManifestEpoch must track the bumped value.
	if got := srv.seenManifestEpoch.Load(); got != 1 {
		test.Fatalf("expected seenManifestEpoch=1 after reload, got %d", got)
	}
}

// Test that maybeReloadManifestForEpoch is idempotent (multiple calls with the
// same epoch value are no-ops).
func TestMaybeReloadManifestForEpoch_Idempotent(test *testing.T) {
	srv := buildTestServer(test)
	root := srv.runtime.Root

	if _, err := manifestepoch.Bump(root); err != nil {
		test.Fatalf("Bump: %v", err)
	}

	// First call reloads.
	reloaded1, err1 := srv.maybeReloadManifestForEpoch(context.Background(), 5*time.Second)
	if err1 != nil || !reloaded1 {
		test.Fatalf("first call: %v, reloaded=%v", err1, reloaded1)
	}

	// Second call is a no-op (same epoch).
	reloaded2, err2 := srv.maybeReloadManifestForEpoch(context.Background(), 5*time.Second)
	if err2 != nil {
		test.Fatalf("second call: %v", err2)
	}

	if reloaded2 {
		test.Fatal("second call should be idempotent (same epoch, no reload)")
	}

	if got := srv.seenManifestEpoch.Load(); got != 1 {
		test.Fatalf("expected seenManifestEpoch=1, got %d", got)
	}
}

// Test that siblingReloadManifest validates and swaps the manifest successfully
// when the TOML is valid and unchanged from the prior version.
func TestSiblingReloadManifest_SuccessfulReloadUnchanged(test *testing.T) {
	srv := buildTestServer(test)

	// Record the current manifest pointer.
	oldManifest := srv.runtime.Manifest

	// Bump manifest-epoch (no actual TOML change — tests the unchanged case).
	if _, err := manifestepoch.Bump(srv.runtime.Root); err != nil {
		test.Fatalf("Bump: %v", err)
	}

	// Call siblingReloadManifest.
	if err := srv.siblingReloadManifest(context.Background(), 5*time.Second); err != nil {
		test.Fatalf("siblingReloadManifest: %v", err)
	}

	// The manifest must have been swapped (fresh pointer; same content).
	newManifest := srv.runtime.Manifest
	if newManifest == oldManifest {
		test.Fatal("expected manifest pointer to change after reload")
	}

	if newManifest.Workspace.Name != oldManifest.Workspace.Name {
		test.Fatalf("manifest content changed unexpectedly")
	}

	// seenManifestEpoch must be updated to 1.
	if got := srv.seenManifestEpoch.Load(); got != 1 {
		test.Fatalf("expected seenManifestEpoch=1 after swap, got %d", got)
	}
}

// Test that siblingReloadManifest does NOT advance seenManifestEpoch when a
// blocking validation error occurs (parse error / behavior-engine failure).
// Non-blocking errors (alias/context) do NOT prevent the swap.
func TestSiblingReloadManifest_NoSwapOnParseError(test *testing.T) {
	srv := buildTestServer(test)
	root := srv.runtime.Root

	// Bump manifest-epoch.
	if _, err := manifestepoch.Bump(root); err != nil {
		test.Fatalf("Bump: %v", err)
	}

	// Corrupt the tusk.toml with invalid syntax.
	manifestPath := filepath.Join(root, "tusk.toml")
	if err := os.WriteFile(manifestPath, []byte("invalid toml [[["), 0o644); err != nil {
		test.Fatalf("corrupt tusk.toml: %v", err)
	}

	// Call siblingReloadManifest; expect an error.
	err := srv.siblingReloadManifest(context.Background(), 5*time.Second)
	if err == nil {
		test.Fatal("expected siblingReloadManifest to return an error on parse failure")
	}

	// seenManifestEpoch must NOT have advanced.
	if got := srv.seenManifestEpoch.Load(); got != 0 {
		test.Fatalf("expected seenManifestEpoch=0 (no advance on parse error), got %d", got)
	}
}

// Test that siblingReloadManifest does NOT reindex (unlike tusk_reload).
// Assert that reindex_gen stays constant before/after reload.
func TestSiblingReloadManifest_NeverReindexes(test *testing.T) {
	srv := buildTestServer(test)

	// Record the current reindex_gen.
	rt := srv.snapshotRuntime()
	metaRepo := rt.Meta
	oldGen, _ := metaRepo.Get("reindex_gen")

	// Bump and reload.
	if _, err := manifestepoch.Bump(srv.runtime.Root); err != nil {
		test.Fatalf("Bump: %v", err)
	}

	if err := srv.siblingReloadManifest(context.Background(), 5*time.Second); err != nil {
		test.Fatalf("siblingReloadManifest: %v", err)
	}

	// Snapshot the new runtime and check reindex_gen.
	newRT := srv.snapshotRuntime()
	newMetaRepo := newRT.Meta
	newGen, _ := newMetaRepo.Get("reindex_gen")

	if newGen != oldGen {
		test.Fatalf("expected reindex_gen to stay %q, got %q (sibling should NOT reindex)", oldGen, newGen)
	}
}
