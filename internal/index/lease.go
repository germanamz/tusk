package index

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"time"
)

// ErrBusy is returned by FileStateRepo.Claim when no row exists for
// the requested path, or when another worker holds an unexpired lease
// on it. The sentinel mirrors lock.ErrBusy in shape so callers can
// branch on either uniformly.
var ErrBusy = errors.New("index: file_state row is busy (no row, or another worker holds an unexpired lease)")

// ClaimedLease is the result of a successful FileStateRepo.Claim. It
// carries the metadata needed to drive the write flow to either
// Release path. ContentHash records the on-disk state observed under
// the lease; the caller uses it to decide whether to re-stage or
// skip.
type ClaimedLease struct {
	Path          string
	WorkerID      string
	LeasedUntilNs int64
	ContentHash   string
}

// ReleaseContext carries the commit outcome of a write performed under
// a claimed lease. On Success, the observed-state columns
// (content_hash, mtime_ns, size, and optionally state) are updated to
// the values supplied here. On abandon (Success = false), those
// columns are left untouched. Either way, leased_by, leased_until_ns,
// pending_temp_path, and pending_hash are cleared.
type ReleaseContext struct {
	Path     string
	WorkerID string

	// Success is true when the staged write committed (os.Rename
	// succeeded). When false, the release path is "abandon" and the
	// observed-state fields below are ignored.
	Success bool

	// State, when non-empty on the Success path, replaces the row's
	// state column ('live' | 'tombstone'). Empty means leave state
	// unchanged.
	State string

	ContentHash string
	MtimeNs     int64
	Size        int64
}

// Claim atomically reserves the file_state row for path on behalf of
// workerID, with a lease expiring ttl from now. ErrBusy is returned
// when no row exists, or when another worker holds an unexpired
// lease.
//
// On successful claim, if the row carried a pending_temp_path left by
// a crashed predecessor, the file at that path is removed (os.Remove,
// ENOENT non-fatal) and the pending_* columns are cleared on the row.
// This is the only path that reaps stale .tusk/staging/ temps — there
// is no separate sweep.
func (repo *FileStateRepo) Claim(path, workerID string, ttl time.Duration) (*ClaimedLease, error) {
	now := time.Now().UnixNano()
	leasedUntil := now + ttl.Nanoseconds()

	var (
		staleTemp   sql.NullString
		contentHash string
	)

	scanErr := repo.db.QueryRow(`
		UPDATE file_state
		SET    leased_by       = ?,
		       leased_until_ns = ?,
		       updated_at_ns   = ?
		WHERE  path = ?
		  AND  (leased_by IS NULL OR leased_until_ns < ?)
		RETURNING pending_temp_path, content_hash
	`, workerID, leasedUntil, now, path, now).Scan(&staleTemp, &contentHash)

	if errors.Is(scanErr, sql.ErrNoRows) {
		return nil, ErrBusy
	}

	if scanErr != nil {
		return nil, fmt.Errorf("fileStateRepo: claim %s: %w", path, scanErr)
	}

	if staleTemp.Valid && staleTemp.String != "" {
		if rmErr := os.Remove(staleTemp.String); rmErr != nil && !errors.Is(rmErr, os.ErrNotExist) {
			return nil, fmt.Errorf("fileStateRepo: claim %s: remove stale temp %s: %w", path, staleTemp.String, rmErr)
		}

		if _, clearErr := repo.db.Exec(`
			UPDATE file_state
			SET    pending_temp_path = NULL,
			       pending_hash      = NULL,
			       updated_at_ns     = ?
			WHERE  path = ? AND leased_by = ?
		`, time.Now().UnixNano(), path, workerID); clearErr != nil {
			return nil, fmt.Errorf("fileStateRepo: claim %s: clear pending: %w", path, clearErr)
		}
	}

	return &ClaimedLease{
		Path:          path,
		WorkerID:      workerID,
		LeasedUntilNs: leasedUntil,
		ContentHash:   contentHash,
	}, nil
}

// Release clears the lease and pending_* columns on outcome.Path. On
// the Success path, the observed-state columns are updated to the
// values in outcome; on the abandon path, those columns stay where
// they were.
//
// The UPDATE is guarded by `leased_by = outcome.WorkerID` so a row
// already reclaimed by a later worker (lease expired, fresh claimant)
// is left alone — the late Release is a silent no-op rather than an
// overwrite.
func (repo *FileStateRepo) Release(outcome ReleaseContext) error {
	now := time.Now().UnixNano()

	// All three release paths clear the lease + pending columns and stamp
	// updated_at_ns under the same worker-guarded WHERE. Only the leading
	// observed-state SET assignments differ between abandon and the two
	// commit variants, so those are built per-path and the shared lease-clear
	// suffix is appended once.
	const leaseClearSet = `
		       leased_by         = NULL,
		       leased_until_ns   = NULL,
		       pending_temp_path = NULL,
		       pending_hash      = NULL,
		       updated_at_ns     = ?
		WHERE  path = ? AND leased_by = ?`

	var (
		observedSet string
		args        []any
		label       string
	)

	switch {
	case !outcome.Success:
		observedSet = ""
		args = []any{now, outcome.Path, outcome.WorkerID}
		label = "abandon"
	case outcome.State != "":
		observedSet = `content_hash      = ?,
		       mtime_ns          = ?,
		       size              = ?,
		       state             = ?,`
		args = []any{outcome.ContentHash, outcome.MtimeNs, outcome.Size, outcome.State, now, outcome.Path, outcome.WorkerID}
		label = "commit"
	default:
		observedSet = `content_hash      = ?,
		       mtime_ns          = ?,
		       size              = ?,`
		args = []any{outcome.ContentHash, outcome.MtimeNs, outcome.Size, now, outcome.Path, outcome.WorkerID}
		label = "commit"
	}

	query := "UPDATE file_state\n\t\tSET    " + observedSet + leaseClearSet

	if _, execErr := repo.db.Exec(query, args...); execErr != nil {
		return fmt.Errorf("fileStateRepo: release %s %s: %w", label, outcome.Path, execErr)
	}

	return nil
}
