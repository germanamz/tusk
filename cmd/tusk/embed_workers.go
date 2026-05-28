package main

import (
	"github.com/germanamz/tusk/internal/embedconfig"
	"github.com/germanamz/tusk/internal/manifest"
)

// resolveEmbedWorkers wraps embedconfig.ResolveWorkers with the manifest
// pointer. Used by every CLI factory that builds a reindex.Config so the
// rebuild-on-Open path runs the worker pool with the same resolved size as
// `tusk reindex` and the MCP runtime. A return of 0 means the operator
// opted out of the worker pool in this instance.
func resolveEmbedWorkers(loaded *manifest.Manifest) int {
	if loaded == nil {
		return embedconfig.ResolveWorkers(nil)
	}

	return embedconfig.ResolveWorkers(loaded.Embeddings.Workers)
}
