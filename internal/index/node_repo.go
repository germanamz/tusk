package index

import (
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
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

	// ParentID is the id of the file node this row is a sub-unit of. NULL
	// (sql.NullString{Valid:false}) for file-level rows. Populated by the
	// sub-unit sync pipeline. Schema column is `parent_id` (P2 migration).
	ParentID sql.NullString

	// Ordinal is the depth-first position of a sub-unit within its parent
	// file, 0-based. NULL for file-level rows. Schema column is `ordinal`
	// (P2 migration). Used by query-time ordering of `contains` traversals.
	Ordinal sql.NullInt64

	// EmbedPayload is the synthesized text the embedder should send to the
	// model for this row. NULL for file-level rows (the embedder builds its
	// own payload from the parsed file). Schema column is `embed_payload`
	// (P2 migration).
	EmbedPayload sql.NullString

	// ContentHash is the current content fingerprint of a sub-unit row:
	// sha256(embed payload) for embeddable leaves, sha256 of the heading for
	// sections. NULL for file-level rows. Drives the sync re-embed decision
	// and doctor coverage. Schema column is `content_hash`.
	ContentHash sql.NullString
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

// nodeUpsertFileSQL writes a file-class row: literal `kind='file'` and
// `source=NULL`. Used by Upsert (the file-row writer). Sub-unit rows go
// through BulkUpsert / nodeUpsertSubUnitSQL, which writes
// `kind='subunit', source='markdown'`.
const nodeUpsertFileSQL = `
	INSERT INTO nodes (
		id, type, path, title, properties_json,
		last_mtime, last_size, last_checksum,
		parent_id, ordinal, embed_payload, content_hash,
		kind, source
	)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'file', NULL)
	ON CONFLICT(id) DO UPDATE SET
		type            = excluded.type,
		path            = excluded.path,
		title           = excluded.title,
		properties_json = excluded.properties_json,
		last_mtime      = excluded.last_mtime,
		last_size       = excluded.last_size,
		last_checksum   = excluded.last_checksum,
		parent_id       = excluded.parent_id,
		ordinal         = excluded.ordinal,
		embed_payload   = excluded.embed_payload,
		content_hash    = excluded.content_hash,
		kind            = 'file',
		source          = NULL
`

// nodeUpsertSubUnitSQL writes a sub-unit-class row: literal
// `kind='subunit'` and a caller-supplied `source` (bound as the final
// `?`). Used by BulkUpsert (the sub-unit row writer). The columns are
// listed in a fixed order so the bind arguments line up; `kind` is a SQL
// literal to mirror nodeUpsertFileSQL, while `source` is bound so the
// sub-unit pipeline can stamp the namespace ("markdown" or "html").
const nodeUpsertSubUnitSQL = `
	INSERT INTO nodes (
		id, type, path, title, properties_json,
		last_mtime, last_size, last_checksum,
		parent_id, ordinal, embed_payload, content_hash,
		kind, source
	)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'subunit', ?)
	ON CONFLICT(id) DO UPDATE SET
		type            = excluded.type,
		path            = excluded.path,
		title           = excluded.title,
		properties_json = excluded.properties_json,
		last_mtime      = excluded.last_mtime,
		last_size       = excluded.last_size,
		last_checksum   = excluded.last_checksum,
		parent_id       = excluded.parent_id,
		ordinal         = excluded.ordinal,
		embed_payload   = excluded.embed_payload,
		content_hash    = excluded.content_hash,
		kind            = 'subunit',
		source          = excluded.source
`

// nodeUpsertArgs returns the positional bind arguments shared by
// nodeUpsertFileSQL and nodeUpsertSubUnitSQL (both bind the same eleven
// columns; the two writers append `kind`/`source` as SQL literals).
func nodeUpsertArgs(row NodeRow) []any {
	return []any{
		row.ID, row.Type, row.Path, row.Title, row.PropertiesJSON,
		row.LastMtime, row.LastSize, row.LastChecksum,
		row.ParentID, row.Ordinal, row.EmbedPayload, row.ContentHash,
	}
}

// Upsert inserts or replaces a file-class node row, writing
// `kind='file'` and `source=NULL`.
func (repo *NodeRepo) Upsert(row NodeRow) error {
	if _, execErr := repo.db.Exec(nodeUpsertFileSQL, nodeUpsertArgs(row)...); execErr != nil {
		return fmt.Errorf("nodeRepo: upsert %s: %w", row.ID, execErr)
	}

	return nil
}

// BulkUpsert inserts or replaces every sub-unit row in a single
// transaction, stamping the supplied source on the nodes.source column.
// The transactional wrap means a partial failure rolls back all rows so
// callers observe an all-or-nothing outcome per call. Used by the
// sub-unit sync pipeline; source is the namespace ("markdown" or "html").
func (repo *NodeRepo) BulkUpsert(rows []NodeRow, source string) error {
	if len(rows) == 0 {
		return nil
	}

	tx, beginErr := repo.db.Begin()

	if beginErr != nil {
		return fmt.Errorf("nodeRepo: bulk upsert begin: %w", beginErr)
	}

	stmt, prepErr := tx.Prepare(nodeUpsertSubUnitSQL)

	if prepErr != nil {
		_ = tx.Rollback()
		return fmt.Errorf("nodeRepo: bulk upsert prepare: %w", prepErr)
	}

	for _, row := range rows {
		if _, execErr := stmt.Exec(append(nodeUpsertArgs(row), source)...); execErr != nil {
			stmt.Close()
			_ = tx.Rollback()
			return fmt.Errorf("nodeRepo: bulk upsert %s: %w", row.ID, execErr)
		}
	}

	stmt.Close()

	if commitErr := tx.Commit(); commitErr != nil {
		return fmt.Errorf("nodeRepo: bulk upsert commit: %w", commitErr)
	}

	return nil
}

// BulkDelete removes every row whose id is in ids in a single transaction.
// FK cascades drop the matching `edges.source_id` and `embeddings.node_id`
// rows automatically (P2 schema). A partial failure rolls back all deletes.
func (repo *NodeRepo) BulkDelete(ids []string) error {
	if len(ids) == 0 {
		return nil
	}

	tx, beginErr := repo.db.Begin()

	if beginErr != nil {
		return fmt.Errorf("nodeRepo: bulk delete begin: %w", beginErr)
	}

	stmt, prepErr := tx.Prepare(`DELETE FROM nodes WHERE id = ?`)

	if prepErr != nil {
		_ = tx.Rollback()
		return fmt.Errorf("nodeRepo: bulk delete prepare: %w", prepErr)
	}

	for _, id := range ids {
		if _, execErr := stmt.Exec(id); execErr != nil {
			stmt.Close()
			_ = tx.Rollback()
			return fmt.Errorf("nodeRepo: bulk delete %s: %w", id, execErr)
		}
	}

	stmt.Close()

	if commitErr := tx.Commit(); commitErr != nil {
		return fmt.Errorf("nodeRepo: bulk delete commit: %w", commitErr)
	}

	return nil
}

// Get returns the row with the given id, or ErrNodeNotFound.
func (repo *NodeRepo) Get(nodeID string) (*NodeRow, error) {
	row := repo.db.QueryRow(nodeSelectColumns+` FROM nodes WHERE id = ?`, nodeID)

	loaded, scanErr := scanNodeRow(row)

	if errors.Is(scanErr, sql.ErrNoRows) {
		return nil, ErrNodeNotFound
	}

	if scanErr != nil {
		return nil, fmt.Errorf("nodeRepo: get %s: %w", nodeID, scanErr)
	}

	return loaded, nil
}

// List returns rows matching filter, ordered by id ASC.
func (repo *NodeRepo) List(filter ListFilter) ([]NodeRow, error) {
	query := nodeSelectColumns + ` FROM nodes`
	args := []any{}

	if filter.Type != "" {
		query += ` WHERE type = ?`
		args = append(args, filter.Type)
	}

	query += ` ORDER BY id ASC`

	return repo.queryNodes(query, args...)
}

// ListFileNodes returns every file-level node (parent_id IS NULL), ordered by
// id ASC. Sub-unit rows are excluded. This is the file-level snapshot source
// for the graph view; List() with an empty ListFilter would also return
// sub-units, which the file-level graph does not render.
func (repo *NodeRepo) ListFileNodes() ([]NodeRow, error) {
	return repo.queryNodes(nodeSelectColumns + ` FROM nodes WHERE parent_id IS NULL ORDER BY id ASC`)
}

// CountFileNodes returns the number of file-level nodes (parent_id IS NULL).
// Used by the graph snapshot to report total size and drive the scale
// guardrail. EdgeRepo has no count helper; edge totals come from len(ListAll()).
func (repo *NodeRepo) CountFileNodes() (int, error) {
	var count int

	if scanErr := repo.db.QueryRow(`SELECT COUNT(*) FROM nodes WHERE parent_id IS NULL`).Scan(&count); scanErr != nil {
		return 0, fmt.Errorf("nodeRepo: count file nodes: %w", scanErr)
	}

	return count, nil
}

// ListByParent returns every sub-unit row whose `parent_id` equals parentID,
// ordered by ordinal ASC. Returns an empty slice (nil) when the parent has
// no sub-unit rows. Used by the sub-unit sync pipeline (Task 3) to diff the
// existing rows against the parser's freshly produced units.
func (repo *NodeRepo) ListByParent(parentID string) ([]NodeRow, error) {
	return repo.queryNodes(
		nodeSelectColumns+` FROM nodes WHERE parent_id = ? ORDER BY ordinal ASC`,
		parentID,
	)
}

// ListSubUnitsForFile returns every sub-unit row belonging to a file,
// regardless of how deeply nested in the document's section tree. The
// match is on the row id prefix `fileID#`, which mirrors the id format
// produced by the Task 3 sync pipeline (`<fileID>#<hash>`). Rows are
// ordered by ordinal ASC so callers walking the slice see depth-first
// document order.
//
// We use GLOB rather than LIKE because LIKE treats `_` as a wildcard,
// which would silently alias sibling files (e.g. `notes/foo_a` matching
// `notes/foo b`'s sub-units). GLOB's wildcards are `*` and `?`, neither
// of which can appear in a workspace file id.
//
// Use this for whole-file sub-unit diffs (sync, doctor); use ListByParent
// when you only need the immediate children of a single parent.
func (repo *NodeRepo) ListSubUnitsForFile(fileID string) ([]NodeRow, error) {
	pattern := fileID + "#*"

	return repo.queryNodes(
		nodeSelectColumns+` FROM nodes WHERE id GLOB ? ORDER BY ordinal ASC`,
		pattern,
	)
}

// maxGlobConditions caps how many `col GLOB ?` predicates are OR'd into a
// single WHERE clause. SQLite parses an OR-chain into a binary tree whose
// depth grows with the term count; past SQLITE_MAX_EXPR_DEPTH (default 1000)
// the parser aborts with "Expression tree is too large". Batching ids in
// chunks well under that ceiling keeps the per-query tree shallow regardless
// of how many files a hybrid filter matches (#564). Shared by the node and
// embedding sub-unit-for-files queries.
const maxGlobConditions = 500

// chunkStrings splits items into consecutive slices of at most size elements;
// the final chunk may be shorter. A non-positive size yields a single chunk.
func chunkStrings(items []string, size int) [][]string {
	if size <= 0 {
		return [][]string{items}
	}

	chunks := make([][]string, 0, (len(items)+size-1)/size)

	for start := 0; start < len(items); start += size {
		end := min(start+size, len(items))

		chunks = append(chunks, items[start:end])
	}

	return chunks
}

// ListSubUnitsForFiles is the batched form of ListSubUnitsForFile. It OR's one
// GLOB predicate per file id, splitting the ids into chunks of
// maxGlobConditions so the OR-chain never exceeds SQLite's expression-depth
// ceiling (#564). Returns rows ordered by id, then ordinal ASC; callers that
// need per-file grouping should bucket by `fileIDFromSubUnit` (the query
// layer's helper). Used by the sub-unit-aware semantic ranker to avoid an N+1
// lookup over candidate files.
//
// Empty input yields an empty slice with no query executed.
func (repo *NodeRepo) ListSubUnitsForFiles(fileIDs []string) ([]NodeRow, error) {
	if len(fileIDs) == 0 {
		return nil, nil
	}

	var results []NodeRow

	for _, chunk := range chunkStrings(fileIDs, maxGlobConditions) {
		conditions := make([]string, 0, len(chunk))
		args := make([]any, 0, len(chunk))

		for _, fileID := range chunk {
			conditions = append(conditions, "id GLOB ?")
			args = append(args, fileID+"#*")
		}

		query := nodeSelectColumns + ` FROM nodes WHERE ` + strings.Join(conditions, " OR ") + ` ORDER BY id ASC, ordinal ASC`

		chunkRows, queryErr := repo.queryNodes(query, args...)

		if queryErr != nil {
			return nil, queryErr
		}

		results = append(results, chunkRows...)
	}

	// Chunking splits the id set across queries, so the per-chunk ORDER BY no
	// longer yields a globally ordered result. Re-sort to preserve the
	// documented (id, ordinal) contract callers rely on.
	sort.SliceStable(results, func(left, right int) bool {
		if results[left].ID != results[right].ID {
			return results[left].ID < results[right].ID
		}

		return results[left].Ordinal.Int64 < results[right].Ordinal.Int64
	})

	return results, nil
}

// ListByIDs returns the rows whose id is in ids, ordered by id ASC. Missing
// ids are silently absent from the result (no error), so callers can diff the
// returned set against the requested set to detect non-existent ids. Empty
// input yields an empty slice with no query executed. Used to batch-resolve a
// set of node ids in one round trip instead of an N+1 sweep of Get calls.
func (repo *NodeRepo) ListByIDs(ids []string) ([]NodeRow, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	args := make([]any, len(ids))

	for idx, id := range ids {
		args[idx] = id
	}

	query := nodeSelectColumns + ` FROM nodes WHERE id IN (` + inPlaceholders(len(ids)) + `) ORDER BY id ASC`

	return repo.queryNodes(query, args...)
}

// FindByTitle returns the IDs of all nodes whose title matches title.
// When targetType is "*", the type filter is skipped and all matching titles
// are returned. Results are ordered by id ASC for stable doctor candidate lists.
func (repo *NodeRepo) FindByTitle(targetType, title string) ([]string, error) {
	var (
		rows     *sql.Rows
		queryErr error
	)

	if targetType == "*" {
		rows, queryErr = repo.db.Query(
			`SELECT id FROM nodes WHERE title = ? ORDER BY id ASC`,
			title,
		)
	} else {
		rows, queryErr = repo.db.Query(
			`SELECT id FROM nodes WHERE type = ? AND title = ? ORDER BY id ASC`,
			targetType, title,
		)
	}

	if queryErr != nil {
		return nil, fmt.Errorf("nodeRepo: findByTitle: %w", queryErr)
	}

	defer rows.Close()

	var ids []string

	for rows.Next() {
		var id string

		if scanErr := rows.Scan(&id); scanErr != nil {
			return nil, fmt.Errorf("nodeRepo: findByTitle scan: %w", scanErr)
		}

		ids = append(ids, id)
	}

	return ids, rows.Err()
}

// CountSubUnits returns the total number of sub-unit rows in the index
// (rows with kind='subunit'). Used by tusk doctor's sub-unit pane
// (spec §5.9).
func (repo *NodeRepo) CountSubUnits() (int, error) {
	var count int

	if scanErr := repo.db.QueryRow(`SELECT COUNT(*) FROM nodes WHERE kind = 'subunit'`).Scan(&count); scanErr != nil {
		return 0, fmt.Errorf("nodeRepo: count sub-units: %w", scanErr)
	}

	return count, nil
}

// CountSubUnitsByKind returns the sub-unit row counts grouped by the
// `type` column (which carries the subunit.Kind string for sub-unit
// rows: "section", "paragraph", "list-item", ...). Used by tusk
// doctor's sub-unit pane.
func (repo *NodeRepo) CountSubUnitsByKind() (map[string]int, error) {
	rows, queryErr := repo.db.Query(`SELECT type, COUNT(*) FROM nodes WHERE kind = 'subunit' GROUP BY type`)

	if queryErr != nil {
		return nil, fmt.Errorf("nodeRepo: count sub-units by kind: %w", queryErr)
	}

	defer rows.Close()

	out := map[string]int{}

	for rows.Next() {
		var (
			kind  string
			count int
		)

		if scanErr := rows.Scan(&kind, &count); scanErr != nil {
			return nil, fmt.Errorf("nodeRepo: scan sub-unit kind: %w", scanErr)
		}

		out[kind] = count
	}

	return out, rows.Err()
}

// CountDedupedSubUnits returns the number of content_hash values shared by two
// or more sub-unit rows — i.e. groups of duplicate-content leaves that the
// content-addressed embedding store collapses to a single shared vector. A
// high count is informational (lots of repeated content), not a problem.
func (repo *NodeRepo) CountDedupedSubUnits() (int, error) {
	var count int

	if scanErr := repo.db.QueryRow(`
		SELECT COUNT(*) FROM (
			SELECT content_hash
			FROM nodes
			WHERE kind = 'subunit' AND content_hash IS NOT NULL
			GROUP BY content_hash
			HAVING COUNT(*) > 1
		)
	`).Scan(&count); scanErr != nil {
		return 0, fmt.Errorf("nodeRepo: count deduped sub-units: %w", scanErr)
	}

	return count, nil
}

// CountOrphanedSubUnits returns the number of sub-unit rows whose
// parent_id does not resolve to any nodes.id. FK cascades should keep
// this at zero; a non-zero count indicates an indexer bug.
func (repo *NodeRepo) CountOrphanedSubUnits() (int, error) {
	var count int

	if scanErr := repo.db.QueryRow(`
		SELECT COUNT(*)
		FROM nodes child
		WHERE child.kind = 'subunit'
		  AND NOT EXISTS (
			SELECT 1 FROM nodes parent WHERE parent.id = child.parent_id
		  )
	`).Scan(&count); scanErr != nil {
		return 0, fmt.Errorf("nodeRepo: count orphaned sub-units: %w", scanErr)
	}

	return count, nil
}

// CountOversizeSubUnitPayloads returns the number of sub-unit rows
// whose embed_payload byte length exceeds maxBytes. Used by tusk
// doctor's sub-unit pane to surface AST leaves that bypass the
// chunker's normal bound.
//
// SQLite's `length()` on a TEXT column returns the codepoint count,
// which undercounts multi-byte UTF-8 (Cyrillic, CJK, emoji, etc.).
// embed.DefaultMaxBytes is a *byte* threshold, so we use
// `octet_length()` to compare bytes against bytes.
func (repo *NodeRepo) CountOversizeSubUnitPayloads(maxBytes int) (int, error) {
	var count int

	if scanErr := repo.db.QueryRow(`
		SELECT COUNT(*)
		FROM nodes
		WHERE kind = 'subunit'
		  AND embed_payload IS NOT NULL
		  AND octet_length(embed_payload) > ?
	`, maxBytes).Scan(&count); scanErr != nil {
		return 0, fmt.Errorf("nodeRepo: count oversize sub-unit payloads: %w", scanErr)
	}

	return count, nil
}

// DeleteByPath removes the node row whose path equals filePath.
func (repo *NodeRepo) DeleteByPath(filePath string) error {
	_, execErr := repo.db.Exec(`DELETE FROM nodes WHERE path = ?`, filePath)

	if execErr != nil {
		return fmt.Errorf("nodeRepo: delete %s: %w", filePath, execErr)
	}

	return nil
}

// nodeSelectColumns is the fixed column list used by Get, List, and
// ListByParent so every scan path uses the same struct shape.
const nodeSelectColumns = `SELECT id, type, path, title, properties_json,
	last_mtime, last_size, last_checksum,
	parent_id, ordinal, embed_payload, content_hash`

// queryNodes runs a SELECT that returns the standard NodeRow column set and
// materializes the result slice.
func (repo *NodeRepo) queryNodes(query string, args ...any) ([]NodeRow, error) {
	rows, queryErr := repo.db.Query(query, args...)

	if queryErr != nil {
		return nil, fmt.Errorf("nodeRepo: %s: %w", firstWord(query), queryErr)
	}

	defer rows.Close()

	var results []NodeRow

	for rows.Next() {
		loaded, scanErr := scanNodeRow(rows)

		if scanErr != nil {
			return nil, fmt.Errorf("nodeRepo: scan: %w", scanErr)
		}

		results = append(results, *loaded)
	}

	return results, rows.Err()
}

// rowScanner abstracts *sql.Row and *sql.Rows so scanNodeRow can serve both
// single-row Get and multi-row List paths without duplication.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanNodeRow(row rowScanner) (*NodeRow, error) {
	loaded := &NodeRow{}

	scanErr := row.Scan(
		&loaded.ID, &loaded.Type, &loaded.Path, &loaded.Title, &loaded.PropertiesJSON,
		&loaded.LastMtime, &loaded.LastSize, &loaded.LastChecksum,
		&loaded.ParentID, &loaded.Ordinal, &loaded.EmbedPayload, &loaded.ContentHash,
	)

	if scanErr != nil {
		return nil, scanErr
	}

	return loaded, nil
}

// firstWord returns the first whitespace-delimited token of a SQL string —
// used solely to give queryNodes errors a readable verb prefix.
func firstWord(query string) string {
	trimmed := strings.TrimSpace(query)

	if idx := strings.IndexAny(trimmed, " \t\n"); idx > 0 {
		return strings.ToLower(trimmed[:idx])
	}

	return strings.ToLower(trimmed)
}
