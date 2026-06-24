package status

// Request is a type alias for Config so the read-verb service surface stays
// uniform with the other verbs (each exposes a <Verb>Request). Using an alias
// rather than a distinct struct prevents the silent-zero trap where adding a
// field to one but not the other would compile but drop the value.
type Request = Config

// Result is the typed payload returned by Run. It is a type alias for
// SnapshotData (the two are field-identical) so existing renderers and MCP
// handlers consume Snapshot output without translation, and so adding a field
// to one cannot silently drop it from the other.
type Result = SnapshotData

// Run is the canonical entry point for the `status` / `tusk_status` verb.
// It wraps Snapshot so the CLI and MCP handlers share a single code path.
func Run(req Request) (*Result, error) {
	return Snapshot(req)
}
