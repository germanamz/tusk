package index

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
)

// MetaRepo persists workspace-scoped key/value pairs in the `meta` table.
type MetaRepo struct {
	db *sql.DB
}

// NewMetaRepo constructs a MetaRepo backed by idx.
func NewMetaRepo(idx *Index) *MetaRepo {
	return &MetaRepo{db: idx.DB()}
}

// Get returns the value for key. Missing keys return ("", nil).
func (repo *MetaRepo) Get(key string) (string, error) {
	var value string

	scanErr := repo.db.QueryRow(`SELECT value FROM meta WHERE key = ?`, key).Scan(&value)

	if scanErr == sql.ErrNoRows {
		return "", nil
	}

	if scanErr != nil {
		return "", fmt.Errorf("metaRepo: get %s: %w", key, scanErr)
	}

	return value, nil
}

// Incr reads key as a decimal int64 (missing or empty = 0), adds delta,
// writes the new value back, and returns it. The read and write run
// inside one transaction so concurrent callers each observe a distinct
// value. A non-numeric existing value returns an error without mutating
// the row.
func (repo *MetaRepo) Incr(key string, delta int64) (int64, error) {
	ctx := context.Background()

	// Pin the transaction to a single connection so BEGIN IMMEDIATE
	// upgrades to the write lock on the same handle that runs the
	// SELECT and INSERT. The default deferred Begin races on the
	// read→write upgrade and trips SQLITE_BUSY_SNAPSHOT under
	// contention; busy_timeout handles serialization on a fresh
	// IMMEDIATE acquisition.
	conn, connErr := repo.db.Conn(ctx)

	if connErr != nil {
		return 0, fmt.Errorf("metaRepo: incr %s conn: %w", key, connErr)
	}

	defer conn.Close()

	if _, beginErr := conn.ExecContext(ctx, `BEGIN IMMEDIATE`); beginErr != nil {
		return 0, fmt.Errorf("metaRepo: incr %s begin: %w", key, beginErr)
	}

	var raw string

	scanErr := conn.QueryRowContext(ctx, `SELECT value FROM meta WHERE key = ?`, key).Scan(&raw)

	if scanErr != nil && scanErr != sql.ErrNoRows {
		_, _ = conn.ExecContext(ctx, `ROLLBACK`)

		return 0, fmt.Errorf("metaRepo: incr %s read: %w", key, scanErr)
	}

	var current int64

	if raw != "" {
		parsed, parseErr := strconv.ParseInt(raw, 10, 64)

		if parseErr != nil {
			_, _ = conn.ExecContext(ctx, `ROLLBACK`)

			return 0, fmt.Errorf("metaRepo: incr %s parse %q: %w", key, raw, parseErr)
		}

		current = parsed
	}

	next := current + delta

	if _, execErr := conn.ExecContext(ctx, `
		INSERT INTO meta (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value
	`, key, strconv.FormatInt(next, 10)); execErr != nil {
		_, _ = conn.ExecContext(ctx, `ROLLBACK`)

		return 0, fmt.Errorf("metaRepo: incr %s write: %w", key, execErr)
	}

	if _, commitErr := conn.ExecContext(ctx, `COMMIT`); commitErr != nil {
		return 0, fmt.Errorf("metaRepo: incr %s commit: %w", key, commitErr)
	}

	return next, nil
}

// Set upserts the value for key.
func (repo *MetaRepo) Set(key, value string) error {
	_, execErr := repo.db.Exec(`
		INSERT INTO meta (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value
	`, key, value)

	if execErr != nil {
		return fmt.Errorf("metaRepo: set %s: %w", key, execErr)
	}

	return nil
}
