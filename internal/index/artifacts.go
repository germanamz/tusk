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
