package index

import (
	"errors"
	"fmt"
	"os"
)

// RemoveArtifacts deletes the SQLite index file at dbPath together with its
// WAL and SHM sidecars (dbPath+"-wal", dbPath+"-shm"). Absent files are not an
// error. Returns the paths actually removed, in deletion order. Used by the
// reset path and by OpenOrRebuild's schema-mismatch rebuild so both drop the
// full artifact set rather than leaving orphaned -wal/-shm sidecars behind.
// (A fresh index.Open salt-validates and rewrites a mismatched WAL, so the
// leftovers are not replayed onto the new DB — this is cleanup/hygiene, not a
// fix for observable corruption.)
func RemoveArtifacts(dbPath string) ([]string, error) {
	var removed []string

	for _, suffix := range []string{"", "-wal", "-shm"} {
		path := dbPath + suffix

		switch removeErr := os.Remove(path); {
		case removeErr == nil:
			removed = append(removed, path)
		case errors.Is(removeErr, os.ErrNotExist):
			// absent is fine
		default:
			return removed, fmt.Errorf("index: remove artifact %s: %w", path, removeErr)
		}
	}

	return removed, nil
}

// SwapInPlace atomically replaces the index at dstDB with the freshly built one
// at srcDB. It first drops srcDB's own -wal/-shm sidecars (a cleanly-closed WAL
// database has none, but be defensive so a stale one is not renamed-orphaned),
// then removes dstDB together with its -wal/-shm (so no stale WAL is replayed
// onto the swapped-in DB), then renames srcDB → dstDB — a single rename syscall,
// atomic on a POSIX filesystem, so a crash mid-swap leaves either the old or the
// new complete index, never a partial one. srcDB must be a checkpointed, closed
// database on the SAME filesystem as dstDB; a cross-device rename would fail.
// Used by OpenOrRebuild so a slow or interrupted rebuild never takes the live
// index offline (#705 Defect A).
func SwapInPlace(srcDB, dstDB string) error {
	for _, suffix := range []string{"-wal", "-shm"} {
		if rmErr := os.Remove(srcDB + suffix); rmErr != nil && !errors.Is(rmErr, os.ErrNotExist) {
			return fmt.Errorf("index: clear src sidecar %s: %w", srcDB+suffix, rmErr)
		}
	}

	if _, rmErr := RemoveArtifacts(dstDB); rmErr != nil {
		return fmt.Errorf("index: clear dst artifacts: %w", rmErr)
	}

	if renameErr := os.Rename(srcDB, dstDB); renameErr != nil {
		return fmt.Errorf("index: swap %s -> %s: %w", srcDB, dstDB, renameErr)
	}

	return nil
}
