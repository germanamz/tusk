package index

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"

	"modernc.org/sqlite"
)

// RemoveArtifacts deletes the SQLite index file at dbPath together with its
// WAL and SHM sidecars (dbPath+"-wal", dbPath+"-shm"). Absent files are not an
// error. Returns the paths actually removed, in deletion order. Used by
// OpenOrRebuild's schema-mismatch rebuild and by Recreate to clear a stale temp
// so they drop the full artifact set rather than leaving orphaned -wal/-shm
// sidecars behind. (A fresh index.Open salt-validates and rewrites a mismatched
// WAL, so the leftovers are not replayed onto the new DB — this is
// cleanup/hygiene, not a fix for observable corruption.)
func RemoveArtifacts(dbPath string) ([]string, error) {
	return removeFiles(dbPath, "", "-wal", "-shm")
}

// removeFiles deletes dbPath+suffix for each suffix, in order, skipping absent
// files, and returns the paths it removed.
func removeFiles(dbPath string, suffixes ...string) ([]string, error) {
	var removed []string

	for _, suffix := range suffixes {
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
// then removes dstDB's -wal/-shm (so no stale WAL is replayed onto the
// swapped-in DB), then renames srcDB over dstDB. The rename replaces dstDB in a
// single syscall, atomic on a POSIX filesystem, so dstDB is never missing: a
// process opening it finds the old index or the new one, never an empty path it
// would create a database at (#839), and a crash mid-swap leaves one complete
// index, never a partial one. srcDB must be a checkpointed, closed database on
// the SAME filesystem as dstDB; a cross-device rename would fail. Returns the
// dstDB artifacts it replaced or removed, main file first. Used by
// OpenOrRebuild so a slow or interrupted rebuild never takes the live index
// offline (#705 Defect A), and by Recreate.
func SwapInPlace(srcDB, dstDB string) ([]string, error) {
	if _, srcErr := removeFiles(srcDB, "-wal", "-shm"); srcErr != nil {
		return nil, fmt.Errorf("index: clear src sidecars: %w", srcErr)
	}

	replaced, dstErr := removeFiles(dstDB, "-wal", "-shm")

	if dstErr != nil {
		return replaced, fmt.Errorf("index: clear dst sidecars: %w", dstErr)
	}

	if _, statErr := os.Lstat(dstDB); statErr == nil {
		replaced = append([]string{dstDB}, replaced...)
	}

	if renameErr := os.Rename(srcDB, dstDB); renameErr != nil {
		return replaced, fmt.Errorf("index: swap %s -> %s: %w", srcDB, dstDB, renameErr)
	}

	return replaced, nil
}

// Recreate replaces the index at dbPath with a new, empty one, for tusk reset.
// It bootstraps the empty index at a sibling temp path, then copies it into
// dbPath through SQLite's online backup, as one write transaction. The file and
// its -wal and -shm stay where they are, so every process with the index open
// keeps sharing them under SQLite's locks and reads the old index or the empty
// one, never a mix. Swapping a new file in by name can't promise that: SQLite
// opens the -wal and -shm by path after the main file, so a process opening the
// index during the swap could pair the old file with the new one's sidecars.
// Deleting the index and reopening it, as reset used to, also left a gap in
// which another process created the file and raced the reset to switch it to
// WAL (#839). When the copy fails, on a file too corrupt to open or a lock
// nobody lets go of, Recreate falls back to SwapInPlace, so reset still clears
// a wedged index. Returns the old artifacts it replaced or removed. On error
// the old index file stays at dbPath.
func Recreate(dbPath string) ([]string, error) {
	tmpPath := dbPath + ".reset"

	// Clear any temp orphaned by a previously crashed reset so we start clean.
	if _, clearErr := RemoveArtifacts(tmpPath); clearErr != nil {
		return nil, fmt.Errorf("index: clear stale reset temp at %s: %w", tmpPath, clearErr)
	}

	fresh, openErr := Open(tmpPath)

	if openErr != nil {
		_, _ = RemoveArtifacts(tmpPath)

		return nil, fmt.Errorf("index: open reset temp: %w", openErr)
	}

	// Fold the WAL into the temp file so both the copy and the swap read a
	// self-contained database, then close it.
	checkpointErr := fresh.Checkpoint()
	closeErr := fresh.Close()

	if buildErr := errors.Join(checkpointErr, closeErr); buildErr != nil {
		_, _ = RemoveArtifacts(tmpPath)

		return nil, fmt.Errorf("index: build reset temp: %w", buildErr)
	}

	defer func() { _, _ = RemoveArtifacts(tmpPath) }()

	_, statErr := os.Lstat(dbPath)
	copyErr := copyInto(dbPath, tmpPath)

	if copyErr == nil && statErr != nil {
		return nil, nil
	}

	if copyErr == nil {
		return []string{dbPath}, nil
	}

	replaced, swapErr := SwapInPlace(tmpPath, dbPath)

	if swapErr != nil {
		return nil, errors.Join(copyErr, swapErr)
	}

	return replaced, nil
}

// copyInto overwrites the database at dstPath with the one at srcPath through
// SQLite's online backup API: one write transaction on dstPath, which waits up
// to busyTimeout for other connections' locks.
func copyInto(dstPath, srcPath string) error {
	db, openErr := sql.Open("sqlite", fmt.Sprintf("%s?_pragma=busy_timeout(%d)", dstPath, busyTimeout.Milliseconds()))

	if openErr != nil {
		return fmt.Errorf("index: open %s: %w", dstPath, openErr)
	}

	defer db.Close()

	conn, connErr := db.Conn(context.Background())

	if connErr != nil {
		return fmt.Errorf("index: open %s: %w", dstPath, connErr)
	}

	defer conn.Close()

	return conn.Raw(func(driverConn any) error {
		restorer, ok := driverConn.(interface {
			NewRestore(srcURI string) (*sqlite.Backup, error)
		})

		if !ok {
			return errors.New("index: the sqlite driver cannot restore a backup")
		}

		backup, initErr := restorer.NewRestore(srcPath)

		if initErr != nil {
			return fmt.Errorf("index: start copy into %s: %w", dstPath, initErr)
		}

		_, stepErr := backup.Step(-1)
		finishErr := backup.Finish()

		if copyErr := errors.Join(stepErr, finishErr); copyErr != nil {
			return fmt.Errorf("index: copy into %s: %w", dstPath, copyErr)
		}

		return nil
	})
}
