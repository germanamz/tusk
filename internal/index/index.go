// Package index manages the local SQLite index that mirrors the workspace.
package index

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

// Index is the SQLite-backed local cache of the workspace.
type Index struct {
	db   *sql.DB
	path string
}

const schema = `
CREATE TABLE IF NOT EXISTS nodes (
	id              TEXT PRIMARY KEY,           -- workspace-relative path without extension
	type            TEXT NOT NULL,
	path            TEXT NOT NULL UNIQUE,       -- workspace-relative file path with extension
	title           TEXT,
	properties_json TEXT NOT NULL DEFAULT '{}', -- JSON object of all non-edge frontmatter properties
	last_mtime      INTEGER NOT NULL,           -- unix nanoseconds
	last_size       INTEGER NOT NULL,
	last_checksum   TEXT NOT NULL               -- sha256 hex
);

CREATE INDEX IF NOT EXISTS nodes_type_idx ON nodes(type);

CREATE TABLE IF NOT EXISTS edges (
	id          INTEGER PRIMARY KEY AUTOINCREMENT,
	type        TEXT NOT NULL,
	source_id   TEXT NOT NULL,
	target_id   TEXT NOT NULL,
	ordinal     INTEGER NOT NULL DEFAULT 0,
	source_path TEXT NOT NULL,
	UNIQUE(type, source_id, target_id, ordinal)
);

CREATE INDEX IF NOT EXISTS edges_source_idx      ON edges(source_id);
CREATE INDEX IF NOT EXISTS edges_target_idx      ON edges(target_id);
CREATE INDEX IF NOT EXISTS edges_type_idx        ON edges(type);
CREATE INDEX IF NOT EXISTS edges_source_path_idx ON edges(source_path);

CREATE TABLE IF NOT EXISTS embeddings (
	id           INTEGER PRIMARY KEY AUTOINCREMENT,
	node_id      TEXT NOT NULL,
	chunk_idx    INTEGER NOT NULL DEFAULT 0,
	model        TEXT NOT NULL,
	content_hash TEXT NOT NULL,
	vector       BLOB NOT NULL,
	dim          INTEGER NOT NULL,
	UNIQUE(node_id, chunk_idx)
);

CREATE INDEX IF NOT EXISTS embeddings_node_idx ON embeddings(node_id);

CREATE TABLE IF NOT EXISTS embed_queue (
	node_id     TEXT PRIMARY KEY,
	enqueued_at INTEGER NOT NULL,
	attempts    INTEGER NOT NULL DEFAULT 0,
	last_error  TEXT
);

CREATE TABLE IF NOT EXISTS manifest_snapshot (
	loaded_at INTEGER NOT NULL,                 -- unix nanoseconds
	body_json TEXT NOT NULL                     -- JSON-serialized snapshot of the manifest
);

CREATE TABLE IF NOT EXISTS warnings (
	id        INTEGER PRIMARY KEY AUTOINCREMENT,
	node_id   TEXT,                             -- nullable: warning may be workspace-scoped
	kind      TEXT NOT NULL,
	message   TEXT NOT NULL,
	since     INTEGER NOT NULL                  -- unix nanoseconds
);
`

// Open opens (and bootstraps if needed) the index at dbPath. The parent
// directory is created if missing.
func Open(dbPath string) (*Index, error) {
	if mkErr := os.MkdirAll(filepath.Dir(dbPath), 0o755); mkErr != nil {
		return nil, fmt.Errorf("index: ensure dir: %w", mkErr)
	}

	db, openErr := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(ON)")

	if openErr != nil {
		return nil, fmt.Errorf("index: open sqlite: %w", openErr)
	}

	if _, execErr := db.Exec(schema); execErr != nil {
		db.Close()
		return nil, fmt.Errorf("index: bootstrap schema: %w", execErr)
	}

	return &Index{db: db, path: dbPath}, nil
}

// Close releases the underlying database handle.
func (idx *Index) Close() error {
	return idx.db.Close()
}

// DB exposes the underlying *sql.DB for repository packages in the same module.
func (idx *Index) DB() *sql.DB {
	return idx.db
}

// ListTables returns the names of all user tables, sorted.
func (idx *Index) ListTables() ([]string, error) {
	rows, queryErr := idx.db.Query(`SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' ORDER BY name`)

	if queryErr != nil {
		return nil, fmt.Errorf("index: list tables: %w", queryErr)
	}

	defer rows.Close()

	var names []string

	for rows.Next() {
		var name string

		if scanErr := rows.Scan(&name); scanErr != nil {
			return nil, fmt.Errorf("index: scan table: %w", scanErr)
		}

		names = append(names, name)
	}

	return names, rows.Err()
}
