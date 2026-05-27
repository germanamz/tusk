package index

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// File-state lifecycle values for the `state` column.
const (
	FileStateLive      = "live"
	FileStateTombstone = "tombstone"
)

// ErrFileStateNotFound is returned by FileStateRepo.Get when no row exists
// for the given path.
var ErrFileStateNotFound = errors.New("index: file_state not found")

// FileStateRow mirrors a row in the file_state table. Lease and pending
// columns are nullable: an unleased, idle row has all four set to NULL.
type FileStateRow struct {
	Path        string
	ContentHash string
	MtimeNs     int64
	Size        int64
	State       string

	LeasedBy        sql.NullString
	LeasedUntilNs   sql.NullInt64
	PendingTempPath sql.NullString
	PendingHash     sql.NullString

	LastSeenGen int64
	UpdatedAtNs int64
}

// FileStateRepo persists FileStateRow values in the SQLite index. Lease
// claim/release helpers live alongside this type in a later task; this
// file covers the CRUD methods used by handlers and reindex to record
// observed file state.
type FileStateRepo struct {
	db *sql.DB
}

// NewFileStateRepo constructs a FileStateRepo backed by idx.
func NewFileStateRepo(idx *Index) *FileStateRepo {
	return &FileStateRepo{db: idx.DB()}
}

// Get returns the file_state row for path, or ErrFileStateNotFound if no
// row exists.
func (repo *FileStateRepo) Get(path string) (*FileStateRow, error) {
	row := &FileStateRow{}

	scanErr := repo.db.QueryRow(`
		SELECT path, content_hash, mtime_ns, size, state,
		       leased_by, leased_until_ns, pending_temp_path, pending_hash,
		       last_seen_gen, updated_at_ns
		FROM file_state
		WHERE path = ?
	`, path).Scan(
		&row.Path, &row.ContentHash, &row.MtimeNs, &row.Size, &row.State,
		&row.LeasedBy, &row.LeasedUntilNs, &row.PendingTempPath, &row.PendingHash,
		&row.LastSeenGen, &row.UpdatedAtNs,
	)

	if errors.Is(scanErr, sql.ErrNoRows) {
		return nil, ErrFileStateNotFound
	}

	if scanErr != nil {
		return nil, fmt.Errorf("fileStateRepo: get %s: %w", path, scanErr)
	}

	return row, nil
}

// Upsert inserts or updates the observed file state for row.Path. Lease
// and pending columns on an existing row are preserved — Upsert never
// overwrites them. On a fresh insert, lease and pending columns are
// initialized to NULL. updated_at_ns is set to time.Now().
func (repo *FileStateRepo) Upsert(row FileStateRow) error {
	now := time.Now().UnixNano()

	_, execErr := repo.db.Exec(`
		INSERT INTO file_state (
			path, content_hash, mtime_ns, size, state,
			leased_by, leased_until_ns, pending_temp_path, pending_hash,
			last_seen_gen, updated_at_ns
		)
		VALUES (?, ?, ?, ?, ?, NULL, NULL, NULL, NULL, ?, ?)
		ON CONFLICT(path) DO UPDATE SET
			content_hash  = excluded.content_hash,
			mtime_ns      = excluded.mtime_ns,
			size          = excluded.size,
			state         = excluded.state,
			last_seen_gen = excluded.last_seen_gen,
			updated_at_ns = excluded.updated_at_ns
	`,
		row.Path, row.ContentHash, row.MtimeNs, row.Size, row.State,
		row.LastSeenGen, now,
	)

	if execErr != nil {
		return fmt.Errorf("fileStateRepo: upsert %s: %w", row.Path, execErr)
	}

	return nil
}

// Tombstone soft-deletes the row for path by setting state = 'tombstone'
// and bumping updated_at_ns. The row remains in the table as an audit
// trail; this is the only deletion convention exposed by FileStateRepo.
// If no row exists for path, Tombstone returns ErrFileStateNotFound.
func (repo *FileStateRepo) Tombstone(path string) error {
	result, execErr := repo.db.Exec(`
		UPDATE file_state
		SET    state         = ?,
		       updated_at_ns = ?
		WHERE  path = ?
	`, FileStateTombstone, time.Now().UnixNano(), path)

	if execErr != nil {
		return fmt.Errorf("fileStateRepo: tombstone %s: %w", path, execErr)
	}

	affected, affectedErr := result.RowsAffected()

	if affectedErr != nil {
		return fmt.Errorf("fileStateRepo: tombstone %s rows affected: %w", path, affectedErr)
	}

	if affected == 0 {
		return ErrFileStateNotFound
	}

	return nil
}

// ListByGenLessThan returns every live row whose last_seen_gen is strictly
// less than gen, ordered by path. Tombstoned rows are excluded — they are
// already past the reap horizon. Used by reindex reap to find files that
// disappeared since the previous walk.
func (repo *FileStateRepo) ListByGenLessThan(gen int64) ([]FileStateRow, error) {
	rows, queryErr := repo.db.Query(`
		SELECT path, content_hash, mtime_ns, size, state,
		       leased_by, leased_until_ns, pending_temp_path, pending_hash,
		       last_seen_gen, updated_at_ns
		FROM file_state
		WHERE state = ? AND last_seen_gen < ?
		ORDER BY path
	`, FileStateLive, gen)

	if queryErr != nil {
		return nil, fmt.Errorf("fileStateRepo: list by gen: %w", queryErr)
	}

	defer rows.Close()

	var out []FileStateRow

	for rows.Next() {
		var row FileStateRow

		scanErr := rows.Scan(
			&row.Path, &row.ContentHash, &row.MtimeNs, &row.Size, &row.State,
			&row.LeasedBy, &row.LeasedUntilNs, &row.PendingTempPath, &row.PendingHash,
			&row.LastSeenGen, &row.UpdatedAtNs,
		)

		if scanErr != nil {
			return nil, fmt.Errorf("fileStateRepo: list by gen scan: %w", scanErr)
		}

		out = append(out, row)
	}

	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, fmt.Errorf("fileStateRepo: list by gen rows: %w", rowsErr)
	}

	return out, nil
}
