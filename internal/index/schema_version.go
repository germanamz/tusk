package index

// SchemaVersion is the on-disk schema generation this binary writes.
// Open compares the value stored in the meta table against this
// constant; a mismatch (or a missing key) means the on-disk index
// was written by a different binary and must be rebuilt from source
// files. The string is opaque — bump it whenever the schema shape
// (DDL, indexes, CHECK constraints) changes in a way that the in-place
// migration code cannot bridge.
//
// 2026-05-27-embed-queue-leases: adds leased_by, leased_until_ns,
// lease_started_at_ns, and kind columns to embed_queue (plus a
// (kind, leased_until_ns) index) in preparation for lease-based draining.
//
// 2026-05-31-workflow-drift-error-code: adds error_code and detail
// columns to workflow_drift so drift rows carry the real rejection code
// and rendered message instead of doctor reconstructing a fixed string.
//
// 2026-06-01-structural-subunit-addressing: adds the nodes.content_hash
// column (current content fingerprint of a sub-unit). Sub-unit ids become
// structural addresses; a full rebuild from source recomputes them.
//
// 2026-06-02-content-addressed-embeddings: replaces the node_id-keyed
// embeddings table with a content-addressed store (PK content_hash, model)
// plus a node_embeddings junction (node_id, chunk_idx -> content_hash). A full
// rebuild re-embeds; identical content is now stored once and shared.
const SchemaVersion = "2026-06-02-content-addressed-embeddings"

// MetaSchemaVersionKey is the key under which SchemaVersion is stored
// in the meta table. The value lives next to other workspace-scoped
// key/value pairs (see meta_repo.go).
const MetaSchemaVersionKey = "schema_version"
