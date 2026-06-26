package graphview

import (
	"encoding/json"
	"net/http"

	"github.com/germanamz/tusk/internal/graphcluster"
	"github.com/germanamz/tusk/internal/manifest"
)

// snapshot builds the file-level graph: every file node, plus only the edges
// whose BOTH endpoints are file nodes (sub-unit "contains" edges are excluded
// from the top-level view; they surface on drill-down). Degree counts those
// kept edges.
func (srv *Server) snapshot() (Graph, error) {
	// Resolve the active cluster config once, before any per-node work.
	// A nil Manifest is tolerated (tests that omit it) and resolves to the
	// default by = "type" behavior.
	cfg := manifest.DefaultGraphCluster()

	if srv.deps.Manifest != nil {
		cfg = srv.deps.Manifest.GraphCluster
	}

	// Resolve the change signal before the producer logic so that later
	// producers (Phase 6 community) which key a memo on sig.Generation
	// have it available without restructuring.
	sig, sigErr := srv.signal()

	if sigErr != nil {
		return Graph{}, sigErr
	}

	fileRows, listErr := srv.deps.Nodes.ListFileNodes()

	if listErr != nil {
		return Graph{}, listErr
	}

	edgeRows, edgeErr := srv.deps.Edges.ListAll()

	if edgeErr != nil {
		return Graph{}, edgeErr
	}

	fileSet := make(map[string]struct{}, len(fileRows))
	for _, row := range fileRows {
		fileSet[row.ID] = struct{}{}
	}

	degree := make(map[string]int, len(fileRows))
	inDegree := make(map[string]int, len(fileRows))

	edges := make([]GraphEdge, 0, len(edgeRows))
	for _, row := range edgeRows {
		_, srcOK := fileSet[row.SourceID]
		_, dstOK := fileSet[row.TargetID]

		if !srcOK || !dstOK {
			continue
		}

		edges = append(edges, GraphEdge{Source: row.SourceID, Target: row.TargetID, Type: row.Type, Kind: row.Kind})
		degree[row.SourceID]++
		degree[row.TargetID]++
		inDegree[row.TargetID]++
	}

	// For the ancestor producer, pre-compute the group map once from the kept
	// file-level edges so the per-node loop is a simple map lookup.
	var ancestorGroupMap map[string]string

	if cfg.By == "ancestor" {
		ancestorGroupMap = ancestorGroups(edges, cfg.Edge, cfg.Depth, cfg.ParentIsSource)
	}

	// For the community producer, pre-compute stable labels via the memo so
	// Detect runs at most once per reindex generation even when concurrent
	// requests race snapshot().
	var communityLabelMap map[string]string

	if cfg.By == "community" {
		// Build the node-id slice in snapshot (deterministic) order; stableLabels
		// relies on this ordering for tie-breaks.
		nodeIDs := make([]string, len(fileRows))

		for idx, row := range fileRows {
			nodeIDs[idx] = row.ID
		}

		// Build the filtered edge slice for the detector.
		clusterEdges := buildCommunityEdges(edges, cfg.CommunityEdges)

		communityLabelMap = srv.communityLabelsFor(sig.Generation, func(prev map[string]string) map[string]string {
			part := srv.detect(nodeIDs, clusterEdges, graphcluster.Options{Resolution: cfg.Resolution, Seed: communitySeed})

			return stableLabels(prev, part, nodeIDs)
		})
	}

	nodes := make([]GraphNode, 0, len(fileRows))
	for _, row := range fileRows {
		nodes = append(nodes, GraphNode{
			ID:       row.ID,
			Type:     row.Type,
			Group:    groupKey(cfg, ancestorGroupMap, communityLabelMap, row.ID, row.Type, row.PropertiesJSON),
			Title:    row.Title,
			Path:     row.Path,
			Tags:     tagsFromProperties(row.PropertiesJSON),
			Degree:   degree[row.ID],
			InDegree: inDegree[row.ID],
		})
	}

	// Build Graph.Cluster exactly once at the single Graph{} construction
	// site. Phase 4 flips Huddle and Phase 7 adds Hull at this one site.
	return Graph{
		Generation: sig.Generation,
		Epoch:      sig.Epoch,
		Nodes:      nodes,
		Edges:      edges,
		Cluster:    ClusterMeta{By: cfg.By, Property: cfg.Property, Huddle: cfg.Huddle, Hull: cfg.Hull},
	}, nil
}

// groupKey resolves the group key for a single node according to the active
// cluster config. All producer branches live here so future phases add a
// branch rather than restructure the function.
//
// ancestorMap is the pre-computed id→ancestor-id map for the "ancestor"
// producer (nil for other producers). communityMap is the pre-computed
// id→stable-label map for the "community" producer (nil for other producers).
// nodeID is needed by the ancestor and community branches.
func groupKey(cfg manifest.GraphCluster, ancestorMap, communityMap map[string]string, nodeID, nodeType, propsJSON string) string {
	switch cfg.By {
	case "property":
		return propertyString(propsJSON, cfg.Property)
	case "ancestor":
		// Use the pre-computed ancestor map; fall back to own id for nodes
		// with no matching hierarchy edge so they each get a stable,
		// distinct singleton group rather than the neutral empty-group bucket.
		if grp, ok := ancestorMap[nodeID]; ok {
			return grp
		}

		return nodeID
	case "community":
		// Use the pre-computed stable label map. A node not present in the map
		// (degenerate: empty graph) falls back to its own id so the group is
		// always non-empty.
		if grp, ok := communityMap[nodeID]; ok {
			return grp
		}

		return nodeID
	default:
		// "type" and any unrecognised value fall back to node type,
		// reproducing today's behavior.
		return nodeType
	}
}

// buildCommunityEdges filters the kept file-level edges for the community
// detector. If filter is empty, all edges are included. Otherwise an edge is
// included when its Type OR its Kind appears in the filter set. Self-loops are
// dropped defensively, though they cannot occur in the kept file-level graph.
func buildCommunityEdges(edges []GraphEdge, filter []string) []graphcluster.Edge {
	clusterEdges := make([]graphcluster.Edge, 0, len(edges))

	if len(filter) == 0 {
		for _, ge := range edges {
			if ge.Source == ge.Target {
				continue
			}

			clusterEdges = append(clusterEdges, graphcluster.Edge{Source: ge.Source, Target: ge.Target, Weight: 1})
		}

		return clusterEdges
	}

	filterSet := make(map[string]struct{}, len(filter))

	for _, name := range filter {
		filterSet[name] = struct{}{}
	}

	for _, ge := range edges {
		if ge.Source == ge.Target {
			continue
		}

		_, matchType := filterSet[ge.Type]
		_, matchKind := filterSet[ge.Kind]

		if matchType || matchKind {
			clusterEdges = append(clusterEdges, graphcluster.Edge{Source: ge.Source, Target: ge.Target, Weight: 1})
		}
	}

	return clusterEdges
}

// signal reads the current change signal, tolerating a nil ChangeSource (tests
// that don't care pass none).
func (srv *Server) signal() (Signal, error) {
	if srv.deps.Changes == nil {
		return Signal{}, nil
	}

	return srv.deps.Changes.Signal()
}

// tagsFromProperties extracts a string "tags" array from a node's raw
// properties JSON. Returns nil when absent or malformed.
func tagsFromProperties(propsJSON string) []string {
	if propsJSON == "" {
		return nil
	}

	var parsed struct {
		Tags []string `json:"tags"`
	}

	if err := json.Unmarshal([]byte(propsJSON), &parsed); err != nil {
		return nil
	}

	return parsed.Tags
}

// propertyString extracts a single string value from a node's raw properties
// JSON by key. Returns "" when the key is absent, the value is not a string,
// or the JSON is malformed. Generalizes tagsFromProperties for arbitrary keys.
func propertyString(propsJSON, key string) string {
	if propsJSON == "" {
		return ""
	}

	var props map[string]json.RawMessage

	if err := json.Unmarshal([]byte(propsJSON), &props); err != nil {
		return ""
	}

	raw, ok := props[key]
	if !ok {
		return ""
	}

	var value string

	if err := json.Unmarshal(raw, &value); err != nil {
		return ""
	}

	return value
}

func (srv *Server) handleGraph(writer http.ResponseWriter, _ *http.Request) {
	graph, err := srv.snapshot()
	if err != nil {
		http.Error(writer, "index unavailable: "+err.Error(), http.StatusServiceUnavailable)

		return
	}

	writeJSON(writer, graph)
}

// writeJSON writes value as application/json. An encode error after the header
// is sent is unrecoverable, so it is intentionally ignored.
func writeJSON(writer http.ResponseWriter, value any) {
	writer.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(writer).Encode(value)
}
