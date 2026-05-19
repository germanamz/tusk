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
	source_path TEXT NOT NULL,
	UNIQUE(type, source_id, target_id, source_path)
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
	body         TEXT NOT NULL DEFAULT '',
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

CREATE TABLE IF NOT EXISTS meta (
	key   TEXT PRIMARY KEY,
	value TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS workflow_drift (
	node_id          TEXT NOT NULL,
	pack_instance    TEXT NOT NULL,
	pack_kind        TEXT NOT NULL,
	observed_status  TEXT NOT NULL,
	property         TEXT NOT NULL,
	observed_at      INTEGER NOT NULL,
	PRIMARY KEY (node_id, pack_instance, observed_status)
);

CREATE INDEX IF NOT EXISTS workflow_drift_node_idx ON workflow_drift(node_id);

CREATE TABLE IF NOT EXISTS property_drift (
	node_id      TEXT NOT NULL,
	node_type    TEXT NOT NULL,
	kind         TEXT NOT NULL,
	property     TEXT NOT NULL,
	details      TEXT,
	observed_at  INTEGER NOT NULL,
	PRIMARY KEY (node_id, kind, property)
);

CREATE INDEX IF NOT EXISTS property_drift_node_idx ON property_drift(node_id);
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

	if migrateErr := migrateDropOrdinalColumn(db); migrateErr != nil {
		db.Close()
		return nil, migrateErr
	}

	return &Index{db: db, path: dbPath}, nil
}

// migrateDropOrdinalColumn drops the `ordinal` column from the `edges` table
// on databases created before the column was removed. Idempotent: if the
// column is absent the function returns nil.
func migrateDropOrdinalColumn(db *sql.DB) error {
	rows, queryErr := db.Query(`PRAGMA table_info(edges)`)

	if queryErr != nil {
		return fmt.Errorf("index: inspect edges schema: %w", queryErr)
	}

	hasOrdinal := false

	for rows.Next() {
		var cid int
		var name, columnType string
		var notNull, pk int
		var dfltValue sql.NullString

		if scanErr := rows.Scan(&cid, &name, &columnType, &notNull, &dfltValue, &pk); scanErr != nil {
			rows.Close()
			return fmt.Errorf("index: scan column info: %w", scanErr)
		}

		if name == "ordinal" {
			hasOrdinal = true
		}
	}

	rows.Close()

	if rowsErr := rows.Err(); rowsErr != nil {
		return fmt.Errorf("index: iterate column info: %w", rowsErr)
	}

	if !hasOrdinal {
		return nil
	}

	statements := []string{
		`CREATE TABLE edges_new (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			type        TEXT NOT NULL,
			source_id   TEXT NOT NULL,
			target_id   TEXT NOT NULL,
			source_path TEXT NOT NULL,
			UNIQUE(type, source_id, target_id, source_path)
		)`,
		`INSERT OR IGNORE INTO edges_new (type, source_id, target_id, source_path)
			SELECT type, source_id, target_id, source_path FROM edges`,
		`DROP TABLE edges`,
		`ALTER TABLE edges_new RENAME TO edges`,
		`CREATE INDEX IF NOT EXISTS edges_source_idx      ON edges(source_id)`,
		`CREATE INDEX IF NOT EXISTS edges_target_idx      ON edges(target_id)`,
		`CREATE INDEX IF NOT EXISTS edges_type_idx        ON edges(type)`,
		`CREATE INDEX IF NOT EXISTS edges_source_path_idx ON edges(source_path)`,
	}

	tx, beginErr := db.Begin()

	if beginErr != nil {
		return fmt.Errorf("index: begin migration: %w", beginErr)
	}

	for _, statement := range statements {
		if _, execErr := tx.Exec(statement); execErr != nil {
			_ = tx.Rollback()
			return fmt.Errorf("index: migrate edges (statement %q): %w", statement, execErr)
		}
	}

	if commitErr := tx.Commit(); commitErr != nil {
		return fmt.Errorf("index: commit migration: %w", commitErr)
	}

	return nil
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
