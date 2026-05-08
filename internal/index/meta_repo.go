package index

import (
	"database/sql"
	"fmt"
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
