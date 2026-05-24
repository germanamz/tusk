package node

// GetRequest configures GetRun.
type GetRequest struct {
	ID string
}

// GetResult is the typed payload returned by GetRun. Node carries id, type,
// path, title, properties, edges, and body — everything both renderers need.
type GetResult struct {
	Node *Node
}

// GetRun is the canonical entry point for the `node get` / `tusk_node_get`
// verb. It delegates to Service.Get; this wrapper exists so the CLI and MCP
// handler share the same request/result types as the other read verbs.
func GetRun(service *Service, req GetRequest) (*GetResult, error) {
	loaded, getErr := service.Get(req.ID)

	if getErr != nil {
		return nil, getErr
	}

	return &GetResult{Node: loaded}, nil
}
