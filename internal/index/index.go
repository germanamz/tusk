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
	content_hash    TEXT NULL,                  -- sha256 of embed payload (leaves) / heading (sections); NULL for files
	kind            TEXT NOT NULL,              -- row-class: 'file' | 'subunit'
	source          TEXT NULL,                  -- namespace identifier; NULL = user
	CHECK (
		(kind = 'file'    AND source IS NULL     AND parent_id IS NULL) OR
		(kind = 'subunit' AND source IS NOT NULL AND parent_id IS NOT NULL)
	)
);

CREATE INDEX IF NOT EXISTS nodes_kind_type_idx ON nodes(kind, type);
-- type-leading index for bare "type = ?" filters: nodes_kind_type_idx leads
-- with the 2-value kind column, so it cannot serve a type-only predicate
-- (the most common structural filter), which otherwise scans the table.
CREATE INDEX IF NOT EXISTS nodes_type_idx ON nodes(type);
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

-- embeddings is the content-addressed vector store: one row per unique
-- (content_hash, model). Many node-chunks can reference the same vector via
-- node_embeddings, so identical content is embedded once and shared. There is
-- no node_id here; vector lifetime is content-scoped and orphans are GC'd when
-- no mapping references a content_hash (see EmbeddingRepo.GCOrphanVectors).
CREATE TABLE IF NOT EXISTS embeddings (
	content_hash TEXT NOT NULL,
	model        TEXT NOT NULL,
	vector       BLOB NOT NULL,
	dim          INTEGER NOT NULL,
	body         TEXT NOT NULL DEFAULT '',
	PRIMARY KEY (content_hash, model)
);

-- node_embeddings maps each embedded node-chunk to its shared vector. Sub-unit
-- rows have exactly one mapping (chunk_idx 0); file rows have one per chunk.
-- The FK cascades mappings away when a node is deleted.
CREATE TABLE IF NOT EXISTS node_embeddings (
	node_id      TEXT NOT NULL,
	chunk_idx    INTEGER NOT NULL DEFAULT 0,
	content_hash TEXT NOT NULL,
	model        TEXT NOT NULL,
	PRIMARY KEY (node_id, chunk_idx),
	FOREIGN KEY (node_id) REFERENCES nodes(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS node_embeddings_hash_idx ON node_embeddings(content_hash);

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

CREATE INDEX IF NOT EXISTS idx_file_state_seen
	ON file_state(last_seen_gen);

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
	error_code       TEXT NOT NULL DEFAULT '',
	detail           TEXT NOT NULL DEFAULT '',
	observed_at      INTEGER NOT NULL,
	PRIMARY KEY (node_id, pack_instance, observed_status)
);

CREATE INDEX IF NOT EXISTS workflow_drift_node_idx ON workflow_drift(node_id);

CREATE TABLE IF NOT EXISTS property_drift (
	node_id      TEXT NOT NULL,
	node_type    TEXT NOT NULL,
	kind         TEXT NOT NULL,
	property     TEXT NOT NULL,
	value        TEXT NOT NULL DEFAULT '', -- the offending property value; '' for per-property (non-ref) kinds
	details      TEXT,
	observed_at  INTEGER NOT NULL,
	PRIMARY KEY (node_id, kind, property, value)
);

CREATE INDEX IF NOT EXISTS property_drift_node_idx ON property_drift(node_id);
`

// migrations holds idempotent schema fixups applied on every Open, after the
// bootstrap schema. Each statement must be safe to run repeatedly and on a
// fresh database. Used to retire tables/columns without forcing a full
// rebuild + re-embed (which a SchemaVersion bump would).
const migrations = `
-- Drop the never-read manifest_snapshot and warnings tables (validation
-- warnings and embed errors are surfaced live by doctor, not persisted).
DROP TABLE IF EXISTS manifest_snapshot;
DROP TABLE IF EXISTS warnings;
-- Drop the never-read idx_file_state_lease partial index. It was added for a
-- file_state lease-sweeper that was never built; no query filters file_state on
-- leased_until_ns WHERE leased_by IS NOT NULL (Claim uses leased_by IS NULL),
-- so the index only cost write maintenance. Its bootstrap CREATE is gone too;
-- this drop reclaims it from databases created before that removal.
DROP INDEX IF EXISTS idx_file_state_lease;
`

// Open opens (and bootstraps if needed) the index at dbPath. The parent
// directory is created if missing. Incompatible on-disk schemas are
// surfaced as *SchemaVersionError (wrapping ErrSchemaIncompatible) for
// OpenOrRebuild to handle by dropping and rebuilding.
func Open(dbPath string) (*Index, error) {
	if mkErr := os.MkdirAll(filepath.Dir(dbPath), 0o755); mkErr != nil {
		return nil, fmt.Errorf("index: ensure dir: %w", mkErr)
	}

	// synchronous=NORMAL: the index is a rebuildable cache (the markdown files
	// are the source of truth), so fsyncing every commit (the FULL default)
	// buys nothing the pipelines' thousands of tiny commits can't regenerate.
	// In WAL mode NORMAL stays corruption-safe and fsyncs only at checkpoint;
	// at worst the last few commits are lost on power loss, which reindex
	// recomputes from disk.
	db, openErr := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(ON)&_pragma=synchronous(NORMAL)")

	if openErr != nil {
		return nil, fmt.Errorf("index: open sqlite: %w", openErr)
	}

	ctx := context.Background()

	if _, execErr := db.ExecContext(ctx, schema); execErr != nil {
		db.Close()
		return nil, fmt.Errorf("index: bootstrap schema: %w", execErr)
	}

	if _, execErr := db.ExecContext(ctx, migrations); execErr != nil {
		db.Close()
		return nil, fmt.Errorf("index: run migrations: %w", execErr)
	}

	if migrateErr := migratePropertyDriftValue(ctx, db); migrateErr != nil {
		db.Close()
		return nil, migrateErr
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

// migratePropertyDriftValue recreates a pre-#689 property_drift table that
// lacks the `value` column. The old primary key (node_id, kind, property)
// collapsed every broken value of one list-of(ref) property into a single row
// (last write won), under-representing the drift and mis-counting heals. The
// new key includes `value`, so each broken value gets its own row.
//
// property_drift is a rebuildable cache — every reindex re-derives it — so the
// migration simply drops the stale-shaped table and lets the bootstrap schema
// (already run above, with the new definition) recreate it. No re-embed, no
// SchemaVersion bump. Idempotent: once the column exists it is a no-op.
func migratePropertyDriftValue(ctx context.Context, db *sql.DB) error {
	var hasValue int

	if scanErr := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM pragma_table_info('property_drift') WHERE name = 'value'`,
	).Scan(&hasValue); scanErr != nil {
		return fmt.Errorf("index: inspect property_drift schema: %w", scanErr)
	}

	if hasValue > 0 {
		return nil
	}

	if _, execErr := db.ExecContext(ctx, `DROP TABLE property_drift`); execErr != nil {
		return fmt.Errorf("index: migrate property_drift: drop legacy table: %w", execErr)
	}

	// Re-running the whole bootstrap schema recreates property_drift with the
	// new shape; every other CREATE is IF NOT EXISTS, so this is a no-op for
	// the tables that already exist.
	if _, execErr := db.ExecContext(ctx, schema); execErr != nil {
		return fmt.Errorf("index: migrate property_drift: recreate table: %w", execErr)
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
