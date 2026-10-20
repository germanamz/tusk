package index

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
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

// TestSwapInPlace_DestinationNeverMissing pins #839: the swap renames the new
// file over the old one, so a process opening dstDB at any moment finds a file
// there. One that found the path missing would create an empty index of its
// own and race the swap.
func TestSwapInPlace_DestinationNeverMissing(test *testing.T) {
	dir := test.TempDir()
	dstDB := filepath.Join(dir, "index.db")
	srcDB := filepath.Join(dir, "index.db.new")

	if writeErr := os.WriteFile(dstDB, []byte("old"), 0o644); writeErr != nil {
		test.Fatalf("seed dst: %v", writeErr)
	}

	var stop, missing atomic.Bool

	done := make(chan struct{})

	go func() {
		defer close(done)

		for !stop.Load() {
			if _, statErr := os.Stat(dstDB); errors.Is(statErr, os.ErrNotExist) {
				missing.Store(true)
			}
		}
	}()

	for range 500 {
		if writeErr := os.WriteFile(srcDB, []byte("new"), 0o644); writeErr != nil {
			test.Fatalf("seed src: %v", writeErr)
		}

		if _, swapErr := SwapInPlace(srcDB, dstDB); swapErr != nil {
			test.Fatalf("SwapInPlace: %v", swapErr)
		}
	}

	stop.Store(true)
	<-done

	if missing.Load() {
		test.Error("dst was missing for a moment during SwapInPlace")
	}
}

// TestRecreate_EmptiesTheIndexInPlace pins what tusk reset leaves at the index
// path: the same file, holding a bootstrapped, empty index, which a handle
// opened before the reset reads too, and no temp left behind (#839).
func TestRecreate_EmptiesTheIndexInPlace(test *testing.T) {
	dbPath := filepath.Join(test.TempDir(), "index.db")

	seed, openErr := Open(dbPath)

	if openErr != nil {
		test.Fatalf("seed open: %v", openErr)
	}

	defer seed.Close()

	if _, execErr := seed.DB().Exec(`INSERT INTO meta(key, value) VALUES ('probe', '1')`); execErr != nil {
		test.Fatalf("seed write: %v", execErr)
	}

	before, statErr := os.Stat(dbPath)

	if statErr != nil {
		test.Fatalf("stat before: %v", statErr)
	}

	replaced, recreateErr := Recreate(dbPath)

	if recreateErr != nil {
		test.Fatalf("Recreate: %v", recreateErr)
	}

	if len(replaced) != 1 || replaced[0] != dbPath {
		test.Errorf("replaced = %v, want [%s]", replaced, dbPath)
	}

	after, restatErr := os.Stat(dbPath)

	if restatErr != nil {
		test.Fatalf("stat after: %v", restatErr)
	}

	if !os.SameFile(before, after) {
		test.Error("Recreate replaced the index file instead of emptying it in place")
	}

	for _, suffix := range []string{"", "-wal", "-shm"} {
		if _, tmpErr := os.Stat(dbPath + ".reset" + suffix); !errors.Is(tmpErr, os.ErrNotExist) {
			test.Errorf("temp %s left behind (stat err: %v)", dbPath+".reset"+suffix, tmpErr)
		}
	}

	// The handle opened before the reset reads the empty index.
	meta := NewMetaRepo(seed)

	if probe, getErr := meta.Get("probe"); getErr != nil || probe != "" {
		test.Errorf("old row survived Recreate: probe = %q (err %v)", probe, getErr)
	}

	if version, getErr := meta.Get(MetaSchemaVersionKey); getErr != nil || version != SchemaVersion {
		test.Errorf("schema_version = %q (err %v), want %q", version, getErr, SchemaVersion)
	}
}

// TestRecreate_SwapsOutACorruptFile pins the fallback: an index SQLite can't
// read is replaced by file, so reset still clears a wedged index.
func TestRecreate_SwapsOutACorruptFile(test *testing.T) {
	dbPath := filepath.Join(test.TempDir(), "index.db")

	if writeErr := os.WriteFile(dbPath, []byte(strings.Repeat("not a database ", 512)), 0o644); writeErr != nil {
		test.Fatalf("seed: %v", writeErr)
	}

	before, statErr := os.Stat(dbPath)

	if statErr != nil {
		test.Fatalf("stat before: %v", statErr)
	}

	replaced, recreateErr := Recreate(dbPath)

	if recreateErr != nil {
		test.Fatalf("Recreate: %v", recreateErr)
	}

	if len(replaced) != 1 || replaced[0] != dbPath {
		test.Errorf("replaced = %v, want [%s]", replaced, dbPath)
	}

	after, restatErr := os.Stat(dbPath)

	if restatErr != nil {
		test.Fatalf("stat after: %v", restatErr)
	}

	if os.SameFile(before, after) {
		test.Error("the corrupt file is still in place; Recreate should have swapped it out")
	}

	store, openErr := Open(dbPath)

	if openErr != nil {
		test.Fatalf("open after Recreate: %v", openErr)
	}

	defer store.Close()

	if version, getErr := NewMetaRepo(store).Get(MetaSchemaVersionKey); getErr != nil || version != SchemaVersion {
		test.Errorf("schema_version = %q (err %v), want %q", version, getErr, SchemaVersion)
	}
}

// TestRecreate_CreatesAMissingIndex pins that a reset of a workspace with no
// index file builds one and reports nothing replaced.
func TestRecreate_CreatesAMissingIndex(test *testing.T) {
	dbPath := filepath.Join(test.TempDir(), "index.db")

	replaced, recreateErr := Recreate(dbPath)

	if recreateErr != nil {
		test.Fatalf("Recreate: %v", recreateErr)
	}

	if len(replaced) != 0 {
		test.Errorf("replaced = %v, want none", replaced)
	}

	store, openErr := Open(dbPath)

	if openErr != nil {
		test.Fatalf("open after Recreate: %v", openErr)
	}

	defer store.Close()

	if version, getErr := NewMetaRepo(store).Get(MetaSchemaVersionKey); getErr != nil || version != SchemaVersion {
		test.Errorf("schema_version = %q (err %v), want %q", version, getErr, SchemaVersion)
	}
}
