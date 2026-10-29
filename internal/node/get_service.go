package node

import (
	"fmt"
	"slices"
	"strings"
)

// GetRequest configures GetRun.
//
// Include selects which expandable fields to populate on the result. The
// service always loads body + edges + properties from disk (Service.Get reads
// the file), so Include in GetRequest is a *filter* over the returned shape
// rather than a load directive — the MCP handler omits unrequested fields
// from its JSON envelope and the CLI does the same in structured mode.
// Fields, when set, names the columns the renderer should project (rendering
// concern); fields that match an expandable name also imply their include
// flag for shape parity with the other read verbs.
//
// EdgeType, EdgeDirection and SubUnitEdges shape the edges the caller hydrates
// (query.EdgeOptions): keep one edge type, keep one direction ("in", "out",
// or "both", the same as ""), and list sub-unit edges as stored instead of
// folded onto their file. Setting any of them asks for edges the way
// include=edges does.
type GetRequest struct {
	ID      string
	Include []string
	Fields  []string

	EdgeType      string
	EdgeDirection string
	SubUnitEdges  bool
}

// GetResult is the typed payload returned by GetRun. Node carries id, type,
// path, title, properties, edges, and body — everything both renderers need.
// IncludeBody / IncludeEdges / IncludeProperties mirror the filtered shape
// the caller asked for; they are convenience flags on top of the always-full
// Node so renderers do not have to re-parse the request. IncludePaths asks for
// the workspace paths the page names, which the node package can't load (the
// caller hydrates them with query.LoadPathsForNode); unlike the others it is
// never implied by an empty filter, since it checks the disk.
type GetResult struct {
	Node              *Node
	IncludeBody       bool
	IncludeEdges      bool
	IncludeProperties bool
	IncludePaths      bool
	// HasIncludeFilter reports whether the caller passed any explicit
	// Include, Fields or edge option. False means "render everything" (back-compat:
	// matches the historical raw-file behavior of `tusk node get`).
	HasIncludeFilter bool
}

// GetRun is the canonical entry point for the `node get` / `tusk_node_get`
// verb. It delegates to Service.Get; this wrapper exists so the CLI and MCP
// handler share the same request/result types as the other read verbs.
func GetRun(service *Service, req GetRequest) (*GetResult, error) {
	switch req.EdgeDirection {
	case "", "in", "out", "both":
	default:
		return nil, fmt.Errorf("node get: direction must be in, out or both (got %q)", req.EdgeDirection)
	}

	loaded, getErr := service.Get(req.ID)

	if getErr != nil {
		return nil, getErr
	}

	shapesEdges := req.EdgeType != "" || req.EdgeDirection != "" || req.SubUnitEdges
	hasFilter := len(req.Include) > 0 || len(req.Fields) > 0 || shapesEdges
	result := &GetResult{Node: loaded, HasIncludeFilter: hasFilter, IncludeEdges: shapesEdges}

	if !hasFilter {
		result.IncludeBody = true
		result.IncludeEdges = true
		result.IncludeProperties = true

		return result, nil
	}

	for _, name := range slices.Concat(req.Include, req.Fields) {
		switch {
		case name == "body":
			result.IncludeBody = true
		case name == "edges":
			result.IncludeEdges = true
		case name == "properties", strings.HasPrefix(name, "properties."):
			// properties.<name> is a fields projection of one property
			// (query.ParseFields, which the adapters validate fields with).
			result.IncludeProperties = true
		case name == "paths":
			result.IncludePaths = true
		}
	}

	return result, nil
}
