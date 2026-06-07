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
