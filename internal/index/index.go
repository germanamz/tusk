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
	parent_id       TEXT NULL,                  -- parent file id for sub-units; NULL for files (P2)
	ordinal         INTEGER NULL,               -- position within parent; NULL for files (P2)
	embed_payload   TEXT NULL,                  -- synthesized embedding payload for sub-units (P2)
	kind            TEXT NOT NULL,              -- row-class: 'file' | 'subunit'
	source          TEXT NULL,                  -- namespace identifier; NULL = user
	CHECK (
		(kind = 'file'    AND source IS NULL     AND parent_id IS NULL) OR
		(kind = 'subunit' AND source IS NOT NULL AND parent_id IS NOT NULL)
	)
);

CREATE INDEX IF NOT EXISTS nodes_kind_type_idx ON nodes(kind, type);
-- The (parent_id, ordinal) composite index is created by
-- migrateAddSubUnitColumns after it ensures the columns exist. The
-- IF NOT EXISTS check there is safe because the index was either
-- created on a fresh DB by that migration or by a previous run.
-- The partial UNIQUE index on path (file rows only) is created by
-- migrateRelaxNodesPathUnique once the sub-units columns exist —
-- sub-unit rows inherit their parent file's path, so a table-level
-- UNIQUE(path) constraint would block them. The predicate is on
-- kind='file' (previously parent_id IS NULL — equivalent under the
-- CHECK, but matches the post-tighten discriminator).

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
	node_id      TEXT NOT NULL UNIQUE,
	chunk_idx    INTEGER NOT NULL DEFAULT 0,
	model        TEXT NOT NULL,
	content_hash TEXT NOT NULL,
	vector       BLOB NOT NULL,
	dim          INTEGER NOT NULL,
	body         TEXT NOT NULL DEFAULT '',
	FOREIGN KEY (node_id) REFERENCES nodes(id) ON DELETE CASCADE
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

	// Pin all bootstrap + migration work to a single physical connection.
	// PRAGMA foreign_keys is per-connection in SQLite, so the FK toggle
	// performed by withForeignKeysDisabled must execute on the same conn
	// as the table-rebuild statements it brackets. Using the *sql.DB pool
	// directly would risk those PRAGMA statements landing on a different
	// pooled connection than the rebuild itself if another goroutine ever
	// raced Open. Today nothing else touches the DB during Open, but the
	// dedicated conn makes the invariant local to this function instead
	// of a cross-package assumption.
	ctx := context.Background()
	conn, connErr := db.Conn(ctx)

	if connErr != nil {
		db.Close()
		return nil, fmt.Errorf("index: acquire bootstrap conn: %w", connErr)
	}

	if _, execErr := conn.ExecContext(ctx, schema); execErr != nil {
		conn.Close()
		db.Close()
		return nil, fmt.Errorf("index: bootstrap schema: %w", execErr)
	}

	if migrateErr := migrateDropOrdinalColumn(ctx, conn); migrateErr != nil {
		conn.Close()
		db.Close()
		return nil, migrateErr
	}

	if migrateErr := migrateAddSubUnitColumns(ctx, conn); migrateErr != nil {
		conn.Close()
		db.Close()
		return nil, migrateErr
	}

	if migrateErr := migrateEmbeddingsPrimaryKey(ctx, conn); migrateErr != nil {
		conn.Close()
		db.Close()
		return nil, migrateErr
	}

	if migrateErr := migrateAddEdgesSourceFK(ctx, conn); migrateErr != nil {
		conn.Close()
		db.Close()
		return nil, migrateErr
	}

	if migrateErr := migrateRelaxNodesPathUnique(ctx, conn); migrateErr != nil {
		conn.Close()
		db.Close()
		return nil, migrateErr
	}

	if closeErr := conn.Close(); closeErr != nil {
		db.Close()
		return nil, fmt.Errorf("index: release bootstrap conn: %w", closeErr)
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
		// Fresh DB (or one created by Task 1 which wrote a value
		// unconditionally — that value matches SchemaVersion). Persist
		// the constant.
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

// nodesColumnNames returns the set of column names currently on the nodes
// table, used by the P2 migrations to decide whether columns must be added.
func nodesColumnNames(ctx context.Context, conn *sql.Conn) (map[string]struct{}, error) {
	rows, queryErr := conn.QueryContext(ctx, `PRAGMA table_info(nodes)`)

	if queryErr != nil {
		return nil, fmt.Errorf("index: inspect nodes schema: %w", queryErr)
	}

	defer rows.Close()

	names := make(map[string]struct{})

	for rows.Next() {
		var cid int
		var name, columnType string
		var notNull, pk int
		var dfltValue sql.NullString

		if scanErr := rows.Scan(&cid, &name, &columnType, &notNull, &dfltValue, &pk); scanErr != nil {
			return nil, fmt.Errorf("index: scan column info: %w", scanErr)
		}

		names[name] = struct{}{}
	}

	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, fmt.Errorf("index: iterate column info: %w", rowsErr)
	}

	return names, nil
}

// migrateAddSubUnitColumns is the P2 sub-units migration. It adds
// parent_id, ordinal, and embed_payload as nullable columns on the nodes
// table and creates the (parent_id, ordinal) composite index used for
// subtree queries. Idempotent: a second call is a no-op once the columns
// exist. Forward-only — rolling back to a pre-P2 binary still reads the
// table because the new columns are nullable.
//
// No transaction wraps the ALTER TABLE / CREATE INDEX statements because
// SQLite auto-commits DDL; if the process dies mid-migration the next
// Open re-runs nodesColumnNames, sees which columns are still missing,
// and adds only those. The migration is therefore self-healing across
// crashes without explicit transactional bracketing.
func migrateAddSubUnitColumns(ctx context.Context, conn *sql.Conn) error {
	cols, colsErr := nodesColumnNames(ctx, conn)

	if colsErr != nil {
		return colsErr
	}

	type addition struct {
		name string
		ddl  string
	}

	additions := []addition{
		{name: "parent_id", ddl: `ALTER TABLE nodes ADD COLUMN parent_id TEXT NULL`},
		{name: "ordinal", ddl: `ALTER TABLE nodes ADD COLUMN ordinal INTEGER NULL`},
		{name: "embed_payload", ddl: `ALTER TABLE nodes ADD COLUMN embed_payload TEXT NULL`},
	}

	for _, add := range additions {
		if _, present := cols[add.name]; present {
			continue
		}

		if _, execErr := conn.ExecContext(ctx, add.ddl); execErr != nil {
			return fmt.Errorf("index: add nodes column %q: %w", add.name, execErr)
		}
	}

	if _, execErr := conn.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS nodes_parent_id_ordinal ON nodes(parent_id, ordinal)`); execErr != nil {
		return fmt.Errorf("index: create nodes_parent_id_ordinal index: %w", execErr)
	}

	return nil
}

// migrateEmbeddingsPrimaryKey rebuilds the embeddings table so node_id is
// uniquely constrained (the spec's "each node has at most one embedding
// row" invariant) and so deleting a node cascades its embedding row via a
// foreign key. The autoincrement `id` column is retained as the row PK for
// back-compat with existing repository code. The chunk_idx column is kept
// for back-compat reads of legacy rows.
//
// Idempotent: the migration runs only when the embeddings table is in its
// pre-P2 shape (no FK to nodes, no UNIQUE(node_id)).
func migrateEmbeddingsPrimaryKey(ctx context.Context, conn *sql.Conn) error {
	needs, needsErr := embeddingsNeedsRebuild(ctx, conn)

	if needsErr != nil {
		return needsErr
	}

	if !needs {
		return nil
	}

	statements := []string{
		`CREATE TABLE embeddings_new (
			id           INTEGER PRIMARY KEY AUTOINCREMENT,
			node_id      TEXT NOT NULL UNIQUE,
			chunk_idx    INTEGER NOT NULL DEFAULT 0,
			model        TEXT NOT NULL,
			content_hash TEXT NOT NULL,
			vector       BLOB NOT NULL,
			dim          INTEGER NOT NULL,
			body         TEXT NOT NULL DEFAULT '',
			FOREIGN KEY (node_id) REFERENCES nodes(id) ON DELETE CASCADE
		)`,
		// Collapse legacy multi-chunk rows to the lowest chunk_idx per node
		// so the new UNIQUE(node_id) constraint holds. After the sub-units
		// migration, embeddings are one-per-node; legacy multi-chunk rows
		// are a back-compat artifact and the lowest chunk is the closest
		// approximation to a whole-document vector.
		`INSERT INTO embeddings_new (node_id, chunk_idx, model, content_hash, vector, dim, body)
			SELECT node_id, chunk_idx, model, content_hash, vector, dim, body
			FROM embeddings
			WHERE chunk_idx = (SELECT MIN(chunk_idx) FROM embeddings AS inner WHERE inner.node_id = embeddings.node_id)`,
		`DROP TABLE embeddings`,
		`ALTER TABLE embeddings_new RENAME TO embeddings`,
		`CREATE INDEX IF NOT EXISTS embeddings_node_idx ON embeddings(node_id)`,
	}

	return withForeignKeysDisabled(ctx, conn, func() error {
		tx, beginErr := conn.BeginTx(ctx, nil)

		if beginErr != nil {
			return fmt.Errorf("index: begin embeddings migration: %w", beginErr)
		}

		for _, statement := range statements {
			if _, execErr := tx.ExecContext(ctx, statement); execErr != nil {
				_ = tx.Rollback()
				return fmt.Errorf("index: migrate embeddings (statement %q): %w", statement, execErr)
			}
		}

		if commitErr := tx.Commit(); commitErr != nil {
			return fmt.Errorf("index: commit embeddings migration: %w", commitErr)
		}

		return nil
	})
}

// embeddingsNeedsRebuild returns true when the embeddings table predates
// the P2 PK + FK migration. The signal is the absence of a FK to nodes;
// once the rebuild has run, the FK is present and the function returns
// false on subsequent calls. A missing embeddings table (fresh DB created
// from the current schema) also returns false because the schema DDL
// already declares the FK.
func embeddingsNeedsRebuild(ctx context.Context, conn *sql.Conn) (bool, error) {
	rows, queryErr := conn.QueryContext(ctx, `PRAGMA foreign_key_list(embeddings)`)

	if queryErr != nil {
		return false, fmt.Errorf("index: inspect embeddings foreign keys: %w", queryErr)
	}

	defer rows.Close()

	for rows.Next() {
		var id, seq int
		var table, from, to, onUpdate, onDelete, match string

		if scanErr := rows.Scan(&id, &seq, &table, &from, &to, &onUpdate, &onDelete, &match); scanErr != nil {
			return false, fmt.Errorf("index: scan foreign key info: %w", scanErr)
		}

		if table == "nodes" && from == "node_id" {
			return false, nil
		}
	}

	if rowsErr := rows.Err(); rowsErr != nil {
		return false, fmt.Errorf("index: iterate foreign key info: %w", rowsErr)
	}

	// No matching FK observed. Check the table actually exists — a fresh
	// DB that already created the table via the schema DDL would have hit
	// the FK above; truly absent means there is nothing to rebuild yet
	// (CREATE TABLE IF NOT EXISTS in the schema DDL will run first).
	var tableExists int

	queryErr = conn.QueryRowContext(
		ctx,
		`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='embeddings'`,
	).Scan(&tableExists)

	if queryErr != nil {
		return false, fmt.Errorf("index: check embeddings existence: %w", queryErr)
	}

	return tableExists > 0, nil
}

// migrateAddEdgesSourceFK rebuilds the edges table to add a foreign key
// from source_id to nodes(id) with ON DELETE CASCADE. SQLite cannot add a
// foreign key in place, so the migration uses the same copy/drop/rename
// pattern as migrateDropOrdinalColumn. Idempotent: returns nil once the
// FK is present.
//
// Foreign-key enforcement is disabled for the duration of the rebuild
// (per the SQLite docs' table-rebuild recipe) so legacy rows whose
// source_id no longer resolves don't crash the migration. Such orphans
// drop on the way through `INSERT OR IGNORE` because the new table's
// FK with ON DELETE CASCADE rejects them, but only after the rebuild
// commits. The PRAGMA toggle is restored before returning.
func migrateAddEdgesSourceFK(ctx context.Context, conn *sql.Conn) error {
	hasFK, fkErr := edgesHasSourceFK(ctx, conn)

	if fkErr != nil {
		return fkErr
	}

	if hasFK {
		return nil
	}

	return withForeignKeysDisabled(ctx, conn, func() error {
		statements := []string{
			`CREATE TABLE edges_new (
				id          INTEGER PRIMARY KEY AUTOINCREMENT,
				type        TEXT NOT NULL,
				source_id   TEXT NOT NULL,
				target_id   TEXT NOT NULL,
				source_path TEXT NOT NULL,
				UNIQUE(type, source_id, target_id, source_path),
				FOREIGN KEY (source_id) REFERENCES nodes(id) ON DELETE CASCADE
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

		tx, beginErr := conn.BeginTx(ctx, nil)

		if beginErr != nil {
			return fmt.Errorf("index: begin edges FK migration: %w", beginErr)
		}

		for _, statement := range statements {
			if _, execErr := tx.ExecContext(ctx, statement); execErr != nil {
				_ = tx.Rollback()
				return fmt.Errorf("index: migrate edges FK (statement %q): %w", statement, execErr)
			}
		}

		if commitErr := tx.Commit(); commitErr != nil {
			return fmt.Errorf("index: commit edges FK migration: %w", commitErr)
		}

		return nil
	})
}

// migrateRelaxNodesPathUnique drops the table-level UNIQUE constraint on
// nodes.path (added by the pre-P2 schema) and replaces it with a partial
// UNIQUE index over file rows only (`WHERE parent_id IS NULL`). Sub-unit
// rows added by the Task 3 sync pipeline inherit their parent file's path,
// so a table-level UNIQUE(path) would prevent the second-and-subsequent
// sub-units of a file from being inserted. The partial index keeps the
// "one file per workspace path" invariant intact while letting many
// sub-units share that path.
//
// Idempotent: returns nil once the partial index exists and the rebuilt
// nodes table no longer carries the table-level UNIQUE(path).
//
// SQLite cannot drop a column-level UNIQUE constraint in place, so the
// migration uses the same copy/drop/rename recipe as
// migrateAddEdgesSourceFK. Foreign-key enforcement is disabled for the
// rebuild because the rebuilt nodes table is briefly renamed, which
// would otherwise break edges.source_id and embeddings.node_id FKs that
// point at it.
func migrateRelaxNodesPathUnique(ctx context.Context, conn *sql.Conn) error {
	needs, needsErr := nodesPathNeedsRelax(ctx, conn)

	if needsErr != nil {
		return needsErr
	}

	if !needs {
		// Ensure the partial unique index exists even when the table
		// is already in its post-migration shape (covers fresh DBs
		// where the schema constant created the table without the
		// UNIQUE constraint and only the index is missing).
		if _, execErr := conn.ExecContext(ctx,
			`CREATE UNIQUE INDEX IF NOT EXISTS nodes_file_path_uidx ON nodes(path) WHERE kind = 'file'`,
		); execErr != nil {
			return fmt.Errorf("index: create nodes_file_path_uidx: %w", execErr)
		}

		return nil
	}

	return withForeignKeysDisabled(ctx, conn, func() error {
		statements := []string{
			`CREATE TABLE nodes_new (
				id              TEXT PRIMARY KEY,
				type            TEXT NOT NULL,
				path            TEXT NOT NULL,
				title           TEXT,
				properties_json TEXT NOT NULL DEFAULT '{}',
				last_mtime      INTEGER NOT NULL,
				last_size       INTEGER NOT NULL,
				last_checksum   TEXT NOT NULL,
				parent_id       TEXT NULL,
				ordinal         INTEGER NULL,
				embed_payload   TEXT NULL
			)`,
			`INSERT INTO nodes_new (
				id, type, path, title, properties_json,
				last_mtime, last_size, last_checksum,
				parent_id, ordinal, embed_payload
			)
			SELECT id, type, path, title, properties_json,
				last_mtime, last_size, last_checksum,
				parent_id, ordinal, embed_payload
			FROM nodes`,
			`DROP TABLE nodes`,
			`ALTER TABLE nodes_new RENAME TO nodes`,
			`CREATE INDEX IF NOT EXISTS nodes_type_idx ON nodes(type)`,
			`CREATE INDEX IF NOT EXISTS nodes_parent_id_ordinal ON nodes(parent_id, ordinal)`,
			`CREATE UNIQUE INDEX IF NOT EXISTS nodes_file_path_uidx ON nodes(path) WHERE parent_id IS NULL`,
		}

		tx, beginErr := conn.BeginTx(ctx, nil)

		if beginErr != nil {
			return fmt.Errorf("index: begin nodes path-unique migration: %w", beginErr)
		}

		for _, statement := range statements {
			if _, execErr := tx.ExecContext(ctx, statement); execErr != nil {
				_ = tx.Rollback()
				return fmt.Errorf("index: migrate nodes path-unique (statement %q): %w", statement, execErr)
			}
		}

		if commitErr := tx.Commit(); commitErr != nil {
			return fmt.Errorf("index: commit nodes path-unique migration: %w", commitErr)
		}

		return nil
	})
}

// nodesPathNeedsRelax reports whether the nodes table still carries the
// pre-P2 table-level UNIQUE(path) constraint. The constraint surfaces in
// PRAGMA index_list as an auto-generated `sqlite_autoindex_nodes_<n>`
// unique index that does NOT carry a partial-index predicate. The
// post-migration `nodes_file_path_uidx` is partial (and lives under a
// stable name) so the two are easily distinguished.
func nodesPathNeedsRelax(ctx context.Context, conn *sql.Conn) (bool, error) {
	rows, queryErr := conn.QueryContext(ctx, `PRAGMA index_list(nodes)`)

	if queryErr != nil {
		return false, fmt.Errorf("index: inspect nodes indexes: %w", queryErr)
	}

	defer rows.Close()

	var hasAutoUnique bool

	for rows.Next() {
		var seq int
		var name, origin string
		var unique, partial int

		if scanErr := rows.Scan(&seq, &name, &unique, &origin, &partial); scanErr != nil {
			return false, fmt.Errorf("index: scan nodes index info: %w", scanErr)
		}

		// `origin = "u"` marks a unique constraint declared inline on
		// a column (the only way an automatic UNIQUE index shows up
		// for nodes.path). The partial flag is 0 for the legacy
		// constraint; the post-migration index is partial.
		if unique == 1 && origin == "u" && partial == 0 {
			hasAutoUnique = true
		}
	}

	if rowsErr := rows.Err(); rowsErr != nil {
		return false, fmt.Errorf("index: iterate nodes index info: %w", rowsErr)
	}

	return hasAutoUnique, nil
}

// withForeignKeysDisabled temporarily toggles PRAGMA foreign_keys = OFF,
// runs the supplied function, and restores the previous setting. Used by
// the P2 migrations that rebuild tables — SQLite's "12-step" table
// rebuild recipe requires FKs to be disabled to avoid spurious
// constraint failures on legacy rows during the copy phase.
//
// SQLite scopes PRAGMA foreign_keys to the connection it executes on,
// so this helper takes a *sql.Conn rather than a *sql.DB to guarantee
// the OFF/ON pair and the supplied fn all run on the same physical
// connection. Passing a *sql.DB would risk the PRAGMA landing on a
// different pooled conn than the table-rebuild statements that depend
// on it, silently re-enabling FK enforcement mid-rebuild.
func withForeignKeysDisabled(ctx context.Context, conn *sql.Conn, fn func() error) error {
	var prior int

	if scanErr := conn.QueryRowContext(ctx, `PRAGMA foreign_keys`).Scan(&prior); scanErr != nil {
		return fmt.Errorf("index: read foreign_keys pragma: %w", scanErr)
	}

	if prior == 1 {
		if _, offErr := conn.ExecContext(ctx, `PRAGMA foreign_keys = OFF`); offErr != nil {
			return fmt.Errorf("index: disable foreign_keys pragma: %w", offErr)
		}
	}

	runErr := fn()

	if prior == 1 {
		if _, onErr := conn.ExecContext(ctx, `PRAGMA foreign_keys = ON`); onErr != nil {
			// Surface the original error (if any) first; mention the
			// restoration failure so the caller knows the connection
			// state is now off-spec.
			if runErr != nil {
				return fmt.Errorf("%w (also: failed to restore foreign_keys pragma: %v)", runErr, onErr)
			}

			return fmt.Errorf("index: restore foreign_keys pragma: %w", onErr)
		}
	}

	return runErr
}

// edgesHasSourceFK reports whether the edges table already carries a
// foreign key from source_id to nodes(id). Returns false when the table
// is absent (the schema bootstrap will create it).
func edgesHasSourceFK(ctx context.Context, conn *sql.Conn) (bool, error) {
	rows, queryErr := conn.QueryContext(ctx, `PRAGMA foreign_key_list(edges)`)

	if queryErr != nil {
		return false, fmt.Errorf("index: inspect edges foreign keys: %w", queryErr)
	}

	defer rows.Close()

	for rows.Next() {
		var id, seq int
		var table, from, to, onUpdate, onDelete, match string

		if scanErr := rows.Scan(&id, &seq, &table, &from, &to, &onUpdate, &onDelete, &match); scanErr != nil {
			return false, fmt.Errorf("index: scan foreign key info: %w", scanErr)
		}

		if table == "nodes" && from == "source_id" {
			return true, nil
		}
	}

	if rowsErr := rows.Err(); rowsErr != nil {
		return false, fmt.Errorf("index: iterate foreign key info: %w", rowsErr)
	}

	return false, nil
}

// migrateDropOrdinalColumn drops the `ordinal` column from the `edges` table
// on databases created before the column was removed. Idempotent: if the
// column is absent the function returns nil.
func migrateDropOrdinalColumn(ctx context.Context, conn *sql.Conn) error {
	rows, queryErr := conn.QueryContext(ctx, `PRAGMA table_info(edges)`)

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

	return withForeignKeysDisabled(ctx, conn, func() error {
		tx, beginErr := conn.BeginTx(ctx, nil)

		if beginErr != nil {
			return fmt.Errorf("index: begin migration: %w", beginErr)
		}

		for _, statement := range statements {
			if _, execErr := tx.ExecContext(ctx, statement); execErr != nil {
				_ = tx.Rollback()
				return fmt.Errorf("index: migrate edges (statement %q): %w", statement, execErr)
			}
		}

		if commitErr := tx.Commit(); commitErr != nil {
			return fmt.Errorf("index: commit migration: %w", commitErr)
		}

		return nil
	})
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
