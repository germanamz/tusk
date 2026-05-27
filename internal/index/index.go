// Package index manages the local SQLite index that mirrors the workspace.
package index

import (
	"context"
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
	path            TEXT NOT NULL,              -- workspace-relative file path with extension; unique among file rows only
	title           TEXT,
	properties_json TEXT NOT NULL DEFAULT '{}', -- JSON object of all non-edge frontmatter properties
	last_mtime      INTEGER NOT NULL,           -- unix nanoseconds
	last_size       INTEGER NOT NULL,
	last_checksum   TEXT NOT NULL,              -- sha256 hex
	parent_id       TEXT NULL,                  -- parent file id for sub-units; NULL for files
	ordinal         INTEGER NULL,               -- position within parent; NULL for files
	embed_payload   TEXT NULL,                  -- synthesized embedding payload for sub-units
	kind            TEXT NOT NULL,              -- row-class: 'file' | 'subunit'
	source          TEXT NULL,                  -- namespace identifier; NULL = user
	CHECK (
		(kind = 'file'    AND source IS NULL     AND parent_id IS NULL) OR
		(kind = 'subunit' AND source IS NOT NULL AND parent_id IS NOT NULL)
	)
);

CREATE INDEX IF NOT EXISTS nodes_kind_type_idx ON nodes(kind, type);
CREATE INDEX IF NOT EXISTS nodes_parent_id_ordinal ON nodes(parent_id, ordinal);
-- Partial UNIQUE index on path (file rows only): sub-units inherit their
-- parent file's path, so a table-level UNIQUE(path) would block them.
CREATE UNIQUE INDEX IF NOT EXISTS nodes_file_path_uidx ON nodes(path) WHERE kind = 'file';

CREATE TABLE IF NOT EXISTS edges (
	id          INTEGER PRIMARY KEY AUTOINCREMENT,
	type        TEXT NOT NULL,
	source_id   TEXT NOT NULL,
	target_id   TEXT NOT NULL,
	source_path TEXT NOT NULL,
	kind        TEXT NOT NULL,              -- 'direct' | 'derived' | 'structural'
	source      TEXT NULL,                  -- namespace identifier; NULL = user
	UNIQUE(source, type, source_id, target_id, source_path),
	FOREIGN KEY (source_id) REFERENCES nodes(id) ON DELETE CASCADE,
	CHECK (
		(kind IN ('direct', 'derived') AND source IS NULL) OR
		(kind = 'structural'           AND source IS NOT NULL)
	)
);

CREATE INDEX IF NOT EXISTS edges_source_idx      ON edges(source_id);
CREATE INDEX IF NOT EXISTS edges_target_idx      ON edges(target_id);
CREATE INDEX IF NOT EXISTS edges_type_idx        ON edges(type);
CREATE INDEX IF NOT EXISTS edges_source_path_idx ON edges(source_path);
CREATE INDEX IF NOT EXISTS edges_source_type_idx ON edges(source, type);
CREATE INDEX IF NOT EXISTS edges_kind_idx        ON edges(kind);

CREATE TABLE IF NOT EXISTS embeddings (
	id           INTEGER PRIMARY KEY AUTOINCREMENT,
	node_id      TEXT NOT NULL,
	chunk_idx    INTEGER NOT NULL DEFAULT 0,
	model        TEXT NOT NULL,
	content_hash TEXT NOT NULL,
	vector       BLOB NOT NULL,
	dim          INTEGER NOT NULL,
	body         TEXT NOT NULL DEFAULT '',
	FOREIGN KEY (node_id) REFERENCES nodes(id) ON DELETE CASCADE,
	UNIQUE(node_id, chunk_idx)
);

CREATE INDEX IF NOT EXISTS embeddings_node_idx ON embeddings(node_id);

CREATE TABLE IF NOT EXISTS embed_queue (
	node_id              TEXT PRIMARY KEY,
	enqueued_at          INTEGER NOT NULL,
	attempts             INTEGER NOT NULL DEFAULT 0,
	last_error           TEXT,
	leased_by            TEXT,                          -- worker id, NULL = unleased
	leased_until_ns      INTEGER,                       -- absolute expiry, NULL = unleased
	lease_started_at_ns  INTEGER,                       -- when lease was taken, NULL = unleased
	kind                 TEXT NOT NULL DEFAULT 'embed'  -- 'embed' | 'reindex'
);

CREATE INDEX IF NOT EXISTS idx_embed_queue_kind_lease
	ON embed_queue(kind, leased_until_ns);

CREATE TABLE IF NOT EXISTS file_state (
	path              TEXT PRIMARY KEY,
	content_hash      TEXT NOT NULL,
	mtime_ns          INTEGER NOT NULL,
	size              INTEGER NOT NULL,
	state             TEXT NOT NULL,              -- 'live' | 'tombstone'
	leased_by         TEXT,                       -- worker id, NULL = unleased
	leased_until_ns   INTEGER,                    -- absolute expiry, NULL = unleased
	pending_temp_path TEXT,                       -- in-flight write target, NULL = none
	pending_hash      TEXT,                       -- hash of content being staged, NULL = none
	last_seen_gen     INTEGER NOT NULL DEFAULT 0, -- reindex generation
	updated_at_ns     INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_file_state_lease
	ON file_state(leased_until_ns)
	WHERE leased_by IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_file_state_seen
	ON file_state(last_seen_gen);

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
// directory is created if missing. Incompatible on-disk schemas are
// surfaced as *SchemaVersionError (wrapping ErrSchemaIncompatible) for
// OpenOrRebuild to handle by dropping and rebuilding.
func Open(dbPath string) (*Index, error) {
	if mkErr := os.MkdirAll(filepath.Dir(dbPath), 0o755); mkErr != nil {
		return nil, fmt.Errorf("index: ensure dir: %w", mkErr)
	}

	db, openErr := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(ON)")

	if openErr != nil {
		return nil, fmt.Errorf("index: open sqlite: %w", openErr)
	}

	ctx := context.Background()

	if _, execErr := db.ExecContext(ctx, schema); execErr != nil {
		db.Close()
		return nil, fmt.Errorf("index: bootstrap schema: %w", execErr)
	}

	idx := &Index{db: db, path: dbPath}

	metaRepo := NewMetaRepo(idx)

	observed, getErr := metaRepo.Get(MetaSchemaVersionKey)
	if getErr != nil {
		idx.Close()
		return nil, fmt.Errorf("index: read schema_version: %w", getErr)
	}

	switch {
	case observed == "":
		// Fresh DB. Persist the schema version constant.
		if setErr := metaRepo.Set(MetaSchemaVersionKey, SchemaVersion); setErr != nil {
			idx.Close()
			return nil, fmt.Errorf("index: persist schema_version: %w", setErr)
		}
	case observed != SchemaVersion:
		idx.Close()
		return nil, &SchemaVersionError{
			Observed: observed,
			Expected: SchemaVersion,
		}
	}

	return idx, nil
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
