package reindex

import "github.com/germanamz/tusk/internal/node"

// isHTMLPath reports whether a workspace path is an HTML content kind. It
// delegates to node.IsHTMLPath so the content-kind set has a single source of
// truth. Used by the drift-exemption path in processReindexJob, which exempts
// the HTML data-* signals key from undeclared-property drift.
func isHTMLPath(path string) bool {
	return node.IsHTMLPath(path)
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
