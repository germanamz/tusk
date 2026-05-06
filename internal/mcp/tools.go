package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/germanamz/tusk/internal/doctor"
	"github.com/germanamz/tusk/internal/filter"
	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/node"
	"github.com/germanamz/tusk/internal/status"
	mcpgo "github.com/mark3labs/mcp-go/mcp"
)

func registerTools(srv *Server) {
	registerStatusTool(srv)
	registerNodeGetTool(srv)
	registerNodeListTool(srv)
	registerEdgeListTool(srv)
	registerQueryTool(srv)
	registerDoctorTool(srv)
}

func registerStatusTool(srv *Server) {
	tool := mcpgo.NewTool("tusk_status",
		mcpgo.WithDescription("Quick workspace summary: node counts by type, edge count, embed queue depth, last reindex time."),
	)

	handler := func(ctx context.Context, request mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		snap, snapErr := status.Snapshot(status.Config{
			Nodes:      srv.runtime.Nodes,
			Edges:      srv.runtime.Edges,
			EmbedQueue: srv.runtime.EmbedQueue,
			Meta:       srv.runtime.Meta,
		})

		if snapErr != nil {
			return toolError(snapErr), nil
		}

		return toolJSON(map[string]any{
			"nodes_by_type":     snap.NodesByType,
			"edge_count":        snap.EdgeCount,
			"embed_queue_depth": snap.EmbedQueueDepth,
			"last_reindex_at":   snap.LastReindexAt,
		})
	}

	srv.register(tool, handler)
}

// argString extracts a required string argument from the request. Returns an
// error when the key is absent or its value is not a string.
func argString(request mcpgo.CallToolRequest, key string) (string, error) {
	args := request.GetArguments()
	value, ok := args[key].(string)

	if !ok {
		return "", fmt.Errorf("missing or non-string argument %q", key)
	}

	return value, nil
}

// argStringOptional extracts an optional string argument from the request.
// Returns an empty string when the key is absent or its value is not a string.
func argStringOptional(request mcpgo.CallToolRequest, key string) string {
	args := request.GetArguments()
	value, _ := args[key].(string)

	return value
}

// argInt extracts a required numeric argument from the request as an int.
func argInt(request mcpgo.CallToolRequest, key string) (int, error) {
	args := request.GetArguments()
	raw, ok := args[key]

	if !ok {
		return 0, fmt.Errorf("missing argument %q", key)
	}

	switch typed := raw.(type) {
	case float64:
		return int(typed), nil
	case int:
		return typed, nil
	}

	return 0, fmt.Errorf("argument %q is not a number", key)
}

// argIntOptional extracts an optional numeric argument from the request as an
// int. Returns defaultValue when the key is absent or not numeric.
func argIntOptional(request mcpgo.CallToolRequest, key string, defaultValue int) int {
	if value, parseErr := argInt(request, key); parseErr == nil {
		return value
	}

	return defaultValue
}

// argMap extracts an optional map argument from the request.
func argMap(request mcpgo.CallToolRequest, key string) map[string]any {
	args := request.GetArguments()
	value, _ := args[key].(map[string]any)

	return value
}

// argStringSlice extracts an optional string-slice argument from the request.
func argStringSlice(request mcpgo.CallToolRequest, key string) []string {
	args := request.GetArguments()
	raw, ok := args[key].([]any)

	if !ok {
		return nil
	}

	out := make([]string, 0, len(raw))

	for _, item := range raw {
		if str, isString := item.(string); isString {
			out = append(out, str)
		}
	}

	return out
}

// toolError wraps an error as a CallToolResult error.
func toolError(err error) *mcpgo.CallToolResult {
	return mcpgo.NewToolResultError(err.Error())
}

// toolJSON marshals payload to JSON and returns it as a text CallToolResult.
func toolJSON(payload any) (*mcpgo.CallToolResult, error) {
	body, marshalErr := json.Marshal(payload)

	if marshalErr != nil {
		return nil, marshalErr
	}

	return mcpgo.NewToolResultText(string(body)), nil
}

func registerNodeGetTool(srv *Server) {
	tool := mcpgo.NewTool("tusk_node_get",
		mcpgo.WithDescription("Read a node by id (workspace-relative path without extension). Returns id, type, path, title, properties, edges, body."),
		mcpgo.WithString("id", mcpgo.Required(), mcpgo.Description("Node id (e.g. \"notes/hi\")")),
	)

	handler := func(ctx context.Context, request mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		nodeID, parseErr := argString(request, "id")

		if parseErr != nil {
			return toolError(parseErr), nil
		}

		loaded, getErr := srv.runtime.NodeService.Get(nodeID)

		if getErr != nil {
			return toolError(getErr), nil
		}

		return toolJSON(map[string]any{
			"id":         loaded.ID,
			"type":       loaded.Type,
			"path":       loaded.Path,
			"title":      loaded.Title,
			"properties": loaded.Properties,
			"edges":      loaded.Edges,
			"body":       string(loaded.Body),
		})
	}

	srv.register(tool, handler)
}

func registerNodeListTool(srv *Server) {
	tool := mcpgo.NewTool("tusk_node_list",
		mcpgo.WithDescription("List nodes from the index. Optional type filter narrows the result."),
		mcpgo.WithString("type", mcpgo.Description("Optional node type filter (e.g. \"ticket\"). Empty = all.")),
	)

	handler := func(ctx context.Context, request mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		typeFilter := argStringOptional(request, "type")

		nodes, listErr := srv.runtime.NodeService.List(node.ListFilter{Type: typeFilter})

		if listErr != nil {
			return toolError(listErr), nil
		}

		results := make([]map[string]any, 0, len(nodes))

		for _, item := range nodes {
			results = append(results, map[string]any{
				"id":    item.ID,
				"type":  item.Type,
				"path":  item.Path,
				"title": item.Title,
			})
		}

		return toolJSON(map[string]any{
			"results": results,
			"count":   len(results),
		})
	}

	srv.register(tool, handler)
}

func registerEdgeListTool(srv *Server) {
	tool := mcpgo.NewTool("tusk_edge_list",
		mcpgo.WithDescription("List edges. Provide from, to, or type to narrow."),
		mcpgo.WithString("from", mcpgo.Description("Source node id")),
		mcpgo.WithString("to", mcpgo.Description("Target node id")),
		mcpgo.WithString("type", mcpgo.Description("Edge type")),
	)

	handler := func(ctx context.Context, request mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		from := argStringOptional(request, "from")
		to := argStringOptional(request, "to")
		edgeType := argStringOptional(request, "type")

		var rows []index.EdgeRow
		var listErr error

		switch {
		case from != "":
			rows, listErr = srv.runtime.Edges.ListBySource(from)
		case to != "":
			rows, listErr = srv.runtime.Edges.ListByTarget(to)
		case edgeType != "":
			rows, listErr = srv.runtime.Edges.ListByType(edgeType)
		default:
			rows, listErr = srv.runtime.Edges.ListAll()
		}

		if listErr != nil {
			return toolError(listErr), nil
		}

		results := make([]map[string]any, 0, len(rows))

		for _, row := range rows {
			results = append(results, map[string]any{
				"type":        row.Type,
				"source_id":   row.SourceID,
				"target_id":   row.TargetID,
				"ordinal":     row.Ordinal,
				"source_path": row.SourcePath,
			})
		}

		return toolJSON(map[string]any{"results": results, "count": len(results)})
	}

	srv.register(tool, handler)
}

func registerQueryTool(srv *Server) {
	tool := mcpgo.NewTool("tusk_query",
		mcpgo.WithDescription("Run a structural filter against the workspace, optionally ranked by semantic similarity."),
		mcpgo.WithString("filter", mcpgo.Required(), mcpgo.Description("Filter expression (e.g. 'type=ticket status=active')")),
		mcpgo.WithString("sort", mcpgo.Description("Sort spec (e.g. '+priority,-due')")),
		mcpgo.WithNumber("take", mcpgo.Description("Limit results to N rows")),
		mcpgo.WithNumber("skip", mcpgo.Description("Skip the first M rows (requires take)")),
		mcpgo.WithString("semantic", mcpgo.Description("Rank by cosine similarity to this query string")),
	)

	handler := func(ctx context.Context, request mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		filterText, parseErr := argString(request, "filter")

		if parseErr != nil {
			return toolError(parseErr), nil
		}

		sortSpec := argStringOptional(request, "sort")
		take := argIntOptional(request, "take", 0)
		skip := argIntOptional(request, "skip", 0)
		semanticQuery := argStringOptional(request, "semantic")

		expr, parseErrs := filter.NewParser(filterText).Parse()

		if len(parseErrs) > 0 {
			return toolError(&parseErrs[0]), nil
		}

		if validateErrs := filter.Validate(expr, *srv.runtime.Manifest); len(validateErrs) > 0 {
			return toolError(&validateErrs[0]), nil
		}

		sortKeys, sortErr := filter.ParseSort(sortSpec)

		if sortErr != nil {
			return toolError(sortErr), nil
		}

		sqlQuery, params, compileErr := filter.Compile(expr, filter.CompileOptions{
			SortKeys: sortKeys,
			Take:     take,
			Skip:     skip,
		})

		if compileErr != nil {
			return toolError(compileErr), nil
		}

		rows, queryErr := srv.runtime.Index.DB().Query(sqlQuery, params...)

		if queryErr != nil {
			return toolError(queryErr), nil
		}

		defer rows.Close()

		type queryResult struct {
			ID    string `json:"id"`
			Type  string `json:"type"`
			Path  string `json:"path"`
			Title string `json:"title"`
		}

		var results []queryResult
		var ids []string

		for rows.Next() {
			var (
				rowID, rowType, rowPath, rowTitle, propertiesRaw, lastChecksum string
				lastMtime, lastSize                                            int64
			)

			if scanErr := rows.Scan(&rowID, &rowType, &rowPath, &rowTitle, &propertiesRaw, &lastMtime, &lastSize, &lastChecksum); scanErr != nil {
				return toolError(scanErr), nil
			}

			results = append(results, queryResult{ID: rowID, Type: rowType, Path: rowPath, Title: rowTitle})
			ids = append(ids, rowID)
		}

		if semanticQuery == "" {
			return toolJSON(map[string]any{"results": results, "count": len(results)})
		}

		if srv.runtime.Embedder == nil {
			return toolError(fmt.Errorf("semantic ranking requires [embeddings] in tusk.toml")), nil
		}

		queryVector, embedErr := srv.runtime.Embedder.Embed(ctx, []byte(semanticQuery))

		if embedErr != nil {
			return toolError(embedErr), nil
		}

		loaded, loadErr := srv.runtime.Embeddings.ListByNodeIDs(ids)

		if loadErr != nil {
			return toolError(loadErr), nil
		}

		candidates := make([]filter.SemanticCandidate, 0, len(loaded))

		for _, embeddingRow := range loaded {
			candidates = append(candidates, filter.SemanticCandidate{NodeID: embeddingRow.NodeID, Vector: embeddingRow.Vector})
		}

		ranked := filter.SemanticRank(candidates, queryVector)

		if take > 0 {
			startIdx := skip

			if startIdx > len(ranked) {
				startIdx = len(ranked)
			}

			endIdx := startIdx + take

			if endIdx > len(ranked) {
				endIdx = len(ranked)
			}

			ranked = ranked[startIdx:endIdx]
		}

		ranking := make([]map[string]any, 0, len(ranked))
		byID := map[string]queryResult{}

		for _, item := range results {
			byID[item.ID] = item
		}

		for _, scored := range ranked {
			ranking = append(ranking, map[string]any{
				"id":    scored.NodeID,
				"score": scored.Score,
				"type":  byID[scored.NodeID].Type,
				"path":  byID[scored.NodeID].Path,
				"title": byID[scored.NodeID].Title,
			})
		}

		return toolJSON(map[string]any{
			"results": ranking,
			"count":   len(ranking),
			"model":   srv.runtime.Embedder.Model(),
		})
	}

	srv.register(tool, handler)
}

func registerDoctorTool(srv *Server) {
	tool := mcpgo.NewTool("tusk_doctor",
		mcpgo.WithDescription("Surface validation warnings and index health issues (dangling edges, embed-queue retries)."),
	)

	handler := func(ctx context.Context, request mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		report, runErr := doctor.Run(doctor.Config{
			Nodes:      srv.runtime.Nodes,
			Edges:      srv.runtime.Edges,
			EmbedQueue: srv.runtime.EmbedQueue,
		})

		if runErr != nil {
			return toolError(runErr), nil
		}

		issues := make([]map[string]any, 0, len(report.Issues))

		for _, issue := range report.Issues {
			issues = append(issues, map[string]any{
				"kind":    issue.Kind,
				"node_id": issue.NodeID,
				"message": issue.Message,
			})
		}

		return toolJSON(map[string]any{
			"issues":            issues,
			"embed_queue_depth": report.EmbedQueueDepth,
		})
	}

	srv.register(tool, handler)
}

// Keep helper imports and functions alive until later tasks (Bundle 5) consume them.
var _ = argMap
var _ = argStringSlice
