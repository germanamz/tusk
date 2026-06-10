package reindex

import (
	"path/filepath"

	"github.com/germanamz/tusk/internal/node"
)

// parseContentFile parses a workspace file into a *node.Node, dispatching by
// extension: HTML kinds go through node.ParseHTMLFile (id retains the
// extension), everything else through the markdown node.ParseFile (id strips
// the extension). This is a thin internal switch on the content kind, not a
// registry — the only structural seam HTML indexing adds to the pipeline.
func parseContentFile(relPath string, content []byte) (*node.Node, error) {
	// keep in sync — import cycle (reindex imports embed) prevents sharing
	switch filepath.Ext(relPath) {
	case ".html", ".htm":
		return node.ParseHTMLFile(relPath, content)
	default:
		return node.ParseFile(relPath, content)
	}
}

// isHTMLPath reports whether a workspace path is an HTML content kind.
//
// BRIDGE (removal target: Phase 5). Only the sub-unit-skip guard at
// worker.go:455 uses this; Phase 5 deletes both the guard and this helper when
// the real HTML sub-unit branch lands.
func isHTMLPath(path string) bool {
	switch filepath.Ext(path) {
	case ".html", ".htm":
		return true
	default:
		return false
	}
}

// htmlReservedDrift augments a behavior-engine reserved-property map with the
// HTML signals key (node.HTMLSignalsKey == "data") for the given node type, so
// node.FilterReservedDrift exempts the data signal map from undeclared-property
// drift. HTML node types are user-declared (arbitrary), so the exemption is
// keyed on the parsed node's own type. The base map is treated as read-only;
// a shallow copy is returned.
func htmlReservedDrift(base map[string]map[string]struct{}, nodeType string) map[string]map[string]struct{} {
	merged := make(map[string]map[string]struct{}, len(base)+1)

	for typeName, props := range base {
		merged[typeName] = props
	}

	exempt := make(map[string]struct{}, len(merged[nodeType])+1)

	for prop := range merged[nodeType] {
		exempt[prop] = struct{}{}
	}

	exempt[node.HTMLSignalsKey] = struct{}{}
	merged[nodeType] = exempt

	return merged
}
