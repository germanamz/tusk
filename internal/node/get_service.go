package node

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
type GetRequest struct {
	ID      string
	Include []string
	Fields  []string
}

// GetResult is the typed payload returned by GetRun. Node carries id, type,
// path, title, properties, edges, and body — everything both renderers need.
// IncludeBody / IncludeEdges / IncludeProperties mirror the filtered shape
// the caller asked for; they are convenience flags on top of the always-full
// Node so renderers do not have to re-parse the request.
type GetResult struct {
	Node              *Node
	IncludeBody       bool
	IncludeEdges      bool
	IncludeProperties bool
	// HasIncludeFilter reports whether the caller passed any explicit
	// Include or Fields. False means "render everything" (back-compat:
	// matches the historical raw-file behavior of `tusk node get`).
	HasIncludeFilter bool
}

// GetRun is the canonical entry point for the `node get` / `tusk_node_get`
// verb. It delegates to Service.Get; this wrapper exists so the CLI and MCP
// handler share the same request/result types as the other read verbs.
func GetRun(service *Service, req GetRequest) (*GetResult, error) {
	loaded, getErr := service.Get(req.ID)

	if getErr != nil {
		return nil, getErr
	}

	hasFilter := len(req.Include) > 0 || len(req.Fields) > 0
	result := &GetResult{Node: loaded, HasIncludeFilter: hasFilter}

	if !hasFilter {
		result.IncludeBody = true
		result.IncludeEdges = true
		result.IncludeProperties = true

		return result, nil
	}

	for _, token := range req.Include {
		switch token {
		case "body":
			result.IncludeBody = true
		case "edges":
			result.IncludeEdges = true
		case "properties":
			result.IncludeProperties = true
		}
	}

	for _, name := range req.Fields {
		switch name {
		case "body":
			result.IncludeBody = true
		case "edges":
			result.IncludeEdges = true
		case "properties":
			result.IncludeProperties = true
		}
	}

	return result, nil
}
