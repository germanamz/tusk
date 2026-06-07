package index

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRemoveArtifacts_RemovesTriplet(test *testing.T) {
	dir := test.TempDir()
	dbPath := filepath.Join(dir, "index.db")

	for _, suffix := range []string{"", "-wal", "-shm"} {
		if writeErr := os.WriteFile(dbPath+suffix, []byte("x"), 0o644); writeErr != nil {
			test.Fatalf("seed %s: %v", suffix, writeErr)
		}
	}

	removed, err := RemoveArtifacts(dbPath)

	if err != nil {
		test.Fatalf("RemoveArtifacts: %v", err)
	}

	if len(removed) != 3 {
		test.Fatalf("expected 3 removed, got %d: %v", len(removed), removed)
	}

	for _, suffix := range []string{"", "-wal", "-shm"} {
		if _, statErr := os.Stat(dbPath + suffix); !os.IsNotExist(statErr) {
			test.Errorf("artifact %q still present", dbPath+suffix)
		}
	}
}

func TestRemoveArtifacts_ToleratesAbsent(test *testing.T) {
	dir := test.TempDir()
	dbPath := filepath.Join(dir, "index.db")

	// Only the main DB exists; sidecars absent.
	if writeErr := os.WriteFile(dbPath, []byte("x"), 0o644); writeErr != nil {
		test.Fatalf("seed: %v", writeErr)
	}

	removed, err := RemoveArtifacts(dbPath)

	if err != nil {
		test.Fatalf("RemoveArtifacts: %v", err)
	}

	if len(removed) != 1 || removed[0] != dbPath {
		test.Fatalf("expected only main db removed, got %v", removed)
	}
}

func TestSidecarDeletionUnderOpenHandle_RecoverableNotPanic(test *testing.T) {
	dir := test.TempDir()
	dbPath := filepath.Join(dir, "index.db")

	store, openErr := Open(dbPath)

	if openErr != nil {
		test.Fatalf("open: %v", openErr)
	}

	defer store.Close()

	// Force WAL/SHM to exist by doing a write under the open handle.
	if _, execErr := store.DB().Exec(`INSERT INTO meta(key, value) VALUES ('probe', '1')`); execErr != nil {
		test.Fatalf("seed write: %v", execErr)
	}

	// Delete the sidecars out from under the still-open connection (simulating a
	// sibling daemon's reset). Delete ONLY the sidecars, not index.db.
	for _, suffix := range []string{"-wal", "-shm"} {
		if rmErr := os.Remove(dbPath + suffix); rmErr != nil && !os.IsNotExist(rmErr) {
			test.Fatalf("remove %s: %v", suffix, rmErr)
		}
	}

	// A subsequent operation must return a Go error (or succeed) but MUST NOT
	// panic / crash the process. Guard with recover to assert no panic.
	func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				test.Fatalf("operation panicked after sidecar deletion: %v", recovered)
			}
		}()
		// Either of these is acceptable: a non-nil error or a clean result. The
		// assertion is "no panic/crash".
		_, _ = store.ListTables()
		_, _ = store.DB().Exec(`INSERT INTO meta(key, value) VALUES ('probe2', '2')`)
	}()
}
