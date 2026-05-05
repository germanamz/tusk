package index

import (
	"database/sql"
	"errors"
	"fmt"
)

// ErrNodeNotFound is returned by NodeRepo.Get when the node id is not in the index.
var ErrNodeNotFound = errors.New("index: node not found")

// NodeRow is the index representation of a node.
type NodeRow struct {
	ID             string
	Type           string
	Path           string
	Title          string
	PropertiesJSON string
	LastMtime      int64
	LastSize       int64
	LastChecksum   string
}

// ListFilter narrows a NodeRepo.List call. Plan 1b supports type only.
type ListFilter struct {
	Type string
}

// NodeRepo persists NodeRow values in the SQLite index.
type NodeRepo struct {
	db *sql.DB
}

// NewNodeRepo constructs a NodeRepo backed by idx.
func NewNodeRepo(idx *Index) *NodeRepo {
	return &NodeRepo{db: idx.DB()}
}

// Upsert inserts or replaces a node row.
func (repo *NodeRepo) Upsert(row NodeRow) error {
	_, execErr := repo.db.Exec(`
		INSERT INTO nodes (id, type, path, title, properties_json, last_mtime, last_size, last_checksum)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			type            = excluded.type,
			path            = excluded.path,
			title           = excluded.title,
			properties_json = excluded.properties_json,
			last_mtime      = excluded.last_mtime,
			last_size       = excluded.last_size,
			last_checksum   = excluded.last_checksum
	`, row.ID, row.Type, row.Path, row.Title, row.PropertiesJSON, row.LastMtime, row.LastSize, row.LastChecksum)

	if execErr != nil {
		return fmt.Errorf("nodeRepo: upsert %s: %w", row.ID, execErr)
	}

	return nil
}

// Get returns the row with the given id, or ErrNodeNotFound.
func (repo *NodeRepo) Get(nodeID string) (*NodeRow, error) {
	row := repo.db.QueryRow(`
		SELECT id, type, path, title, properties_json, last_mtime, last_size, last_checksum
		FROM nodes
		WHERE id = ?
	`, nodeID)

	loaded := &NodeRow{}
	scanErr := row.Scan(&loaded.ID, &loaded.Type, &loaded.Path, &loaded.Title, &loaded.PropertiesJSON, &loaded.LastMtime, &loaded.LastSize, &loaded.LastChecksum)

	if scanErr == sql.ErrNoRows {
		return nil, ErrNodeNotFound
	}

	if scanErr != nil {
		return nil, fmt.Errorf("nodeRepo: get %s: %w", nodeID, scanErr)
	}

	return loaded, nil
}

// List returns rows matching filter, ordered by id ASC.
func (repo *NodeRepo) List(filter ListFilter) ([]NodeRow, error) {
	query := `SELECT id, type, path, title, properties_json, last_mtime, last_size, last_checksum FROM nodes`
	args := []any{}

	if filter.Type != "" {
		query += ` WHERE type = ?`
		args = append(args, filter.Type)
	}

	query += ` ORDER BY id ASC`

	rows, queryErr := repo.db.Query(query, args...)

	if queryErr != nil {
		return nil, fmt.Errorf("nodeRepo: list: %w", queryErr)
	}

	defer rows.Close()

	var results []NodeRow

	for rows.Next() {
		row := NodeRow{}

		if scanErr := rows.Scan(&row.ID, &row.Type, &row.Path, &row.Title, &row.PropertiesJSON, &row.LastMtime, &row.LastSize, &row.LastChecksum); scanErr != nil {
			return nil, fmt.Errorf("nodeRepo: scan: %w", scanErr)
		}

		results = append(results, row)
	}

	return results, rows.Err()
}

// DeleteByPath removes the node row whose path equals filePath.
func (repo *NodeRepo) DeleteByPath(filePath string) error {
	_, execErr := repo.db.Exec(`DELETE FROM nodes WHERE path = ?`, filePath)

	if execErr != nil {
		return fmt.Errorf("nodeRepo: delete %s: %w", filePath, execErr)
	}

	return nil
}
