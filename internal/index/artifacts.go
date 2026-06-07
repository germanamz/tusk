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
// full artifact set — leaving a stale WAL/SHM behind is a corruption trap.
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
