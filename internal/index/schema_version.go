package index

// SchemaVersion is the on-disk schema generation this binary writes.
// Open compares the value stored in the meta table against this
// constant; a mismatch (or a missing key) means the on-disk index
// was written by a different binary and must be rebuilt from source
// files. The string is opaque — bump it whenever the schema shape
// (DDL, indexes, CHECK constraints) changes in a way that the in-place
// migration code cannot bridge.
const SchemaVersion = "2026-05-25-edges-tightened"

// MetaSchemaVersionKey is the key under which SchemaVersion is stored
// in the meta table. The value lives next to other workspace-scoped
// key/value pairs (see meta_repo.go).
const MetaSchemaVersionKey = "schema_version"
