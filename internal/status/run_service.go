package status

// Request is a type alias for Config so the read-verb service surface stays
// uniform with the other verbs (each exposes a <Verb>Request). Using an alias
// rather than a distinct struct prevents the silent-zero trap where adding a
// field to one but not the other would compile but drop the value.
type Request = Config

// Result is the typed payload returned by Run. The shape mirrors SnapshotData
// so existing renderers and MCP handlers can consume it without translation.
type Result struct {
	NodesByType     map[string]int
	EdgeCount       int
	EmbedQueueDepth int
	LastReindexAt   string
}

// Run is the canonical entry point for the `status` / `tusk_status` verb.
// It wraps Snapshot so the CLI and MCP handlers share a single code path.
func Run(req Request) (*Result, error) {
	snap, snapErr := Snapshot(req)

	if snapErr != nil {
		return nil, snapErr
	}

	return &Result{
		NodesByType:     snap.NodesByType,
		EdgeCount:       snap.EdgeCount,
		EmbedQueueDepth: snap.EmbedQueueDepth,
		LastReindexAt:   snap.LastReindexAt,
	}, nil
}
