package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/germanamz/tusk/internal/doctor"
	"github.com/germanamz/tusk/internal/filter"
	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/node"
	"github.com/germanamz/tusk/internal/reindex"
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
	registerNodeCreateTool(srv)
	registerNodeModifyTool(srv)
	registerNodeMoveTool(srv)
	registerNodeDeleteTool(srv)
	registerEdgeAddTool(srv)
	registerEdgeRemoveTool(srv)
	registerReindexTool(srv)
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

// normalizeProps coerces float64 values in props to int when they represent
// whole numbers. MCP clients encode JSON numbers as float64; the node service
// only accepts string/int/bool for frontmatter properties.
func normalizeProps(props map[string]any) map[string]any {
	if props == nil {
		return nil
	}

	out := make(map[string]any, len(props))

	for key, value := range props {
		if floatVal, isFloat := value.(float64); isFloat && floatVal == float64(int(floatVal)) {
			out[key] = int(floatVal)
		} else {
			out[key] = value
		}
	}

	return out
}

// mcpSourcePath is the synthetic source_path attributed to edges added via MCP
// tools. Mirrors cmd/tusk's cliSourcePath; both keep MCP/CLI-added edges
// distinguishable from edges discovered in node frontmatter.
const mcpSourcePath = "__mcp__"

func registerNodeCreateTool(srv *Server) {
	tool := mcpgo.NewTool("tusk_node_create",
		mcpgo.WithDescription("Create a new node file and index it. The path must be a workspace-relative path with extension."),
		mcpgo.WithString("path", mcpgo.Required(), mcpgo.Description("Workspace-relative target path (e.g. notes/hello.md)")),
		mcpgo.WithString("type", mcpgo.Required(), mcpgo.Description("Node type")),
		mcpgo.WithString("title", mcpgo.Description("Optional title")),
		mcpgo.WithString("body", mcpgo.Description("Optional markdown body")),
		mcpgo.WithObject("properties", mcpgo.Description("Additional frontmatter properties")),
	)

	handler := func(ctx context.Context, request mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		path, parseErr := argString(request, "path")

		if parseErr != nil {
			return toolError(parseErr), nil
		}

		nodeType, parseErr := argString(request, "type")

		if parseErr != nil {
			return toolError(parseErr), nil
		}

		title := argStringOptional(request, "title")
		body := argStringOptional(request, "body")
		properties := normalizeProps(argMap(request, "properties"))

		var created *node.Node

		lockErr := srv.runtime.WithWriteLock(func() error {
			out, createErr := srv.runtime.NodeService.Create(node.CreateInput{
				RelPath:    path,
				Type:       nodeType,
				Title:      title,
				Properties: properties,
				Body:       []byte(body),
			})

			if createErr != nil {
				return createErr
			}

			created = out

			return nil
		})

		if lockErr != nil {
			return toolError(lockErr), nil
		}

		return toolJSON(map[string]any{
			"id":    created.ID,
			"type":  created.Type,
			"path":  created.Path,
			"title": created.Title,
		})
	}

	srv.register(tool, handler)
}

func registerNodeModifyTool(srv *Server) {
	tool := mcpgo.NewTool("tusk_node_modify",
		mcpgo.WithDescription("Modify a node's frontmatter properties. Cannot change type."),
		mcpgo.WithString("id", mcpgo.Required(), mcpgo.Description("Node id")),
		mcpgo.WithObject("set", mcpgo.Description("Properties to upsert (key→value)")),
		mcpgo.WithArray("unset", mcpgo.Description("Property keys to remove"), mcpgo.Items(map[string]any{"type": "string"})),
		mcpgo.WithString("body", mcpgo.Description("Optional new markdown body")),
	)

	handler := func(ctx context.Context, request mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		nodeID, parseErr := argString(request, "id")

		if parseErr != nil {
			return toolError(parseErr), nil
		}

		setProps := normalizeProps(argMap(request, "set"))
		unsetKeys := argStringSlice(request, "unset")

		input := node.ModifyInput{
			ID:        nodeID,
			SetProps:  setProps,
			UnsetKeys: unsetKeys,
		}

		if rawBody, hasBody := request.GetArguments()["body"].(string); hasBody {
			body := []byte(rawBody)
			input.Body = &body
		}

		var modified *node.Node

		lockErr := srv.runtime.WithWriteLock(func() error {
			out, modifyErr := srv.runtime.NodeService.Modify(input)

			if modifyErr != nil {
				return modifyErr
			}

			modified = out

			return nil
		})

		if lockErr != nil {
			return toolError(lockErr), nil
		}

		return toolJSON(map[string]any{
			"id":         modified.ID,
			"type":       modified.Type,
			"path":       modified.Path,
			"title":      modified.Title,
			"properties": modified.Properties,
		})
	}

	srv.register(tool, handler)
}

func registerNodeMoveTool(srv *Server) {
	tool := mcpgo.NewTool("tusk_node_move",
		mcpgo.WithDescription("Atomically rename a node and rewrite incoming edges."),
		mcpgo.WithString("id", mcpgo.Required(), mcpgo.Description("Current node id")),
		mcpgo.WithString("new_path", mcpgo.Required(), mcpgo.Description("New workspace-relative path with extension")),
	)

	handler := func(ctx context.Context, request mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		nodeID, parseErr := argString(request, "id")

		if parseErr != nil {
			return toolError(parseErr), nil
		}

		newPath, parseErr := argString(request, "new_path")

		if parseErr != nil {
			return toolError(parseErr), nil
		}

		var plan *node.RenamePlan

		lockErr := srv.runtime.WithWriteLock(func() error {
			out, renameErr := node.Rename(srv.runtime.Root, srv.runtime.Nodes, srv.runtime.Edges, srv.runtime.Manifest.EdgeTypes, nodeID, newPath)

			if renameErr != nil {
				return renameErr
			}

			plan = out

			return nil
		})

		if lockErr != nil {
			return toolError(lockErr), nil
		}

		return toolJSON(map[string]any{
			"old_id":         plan.OldID,
			"new_id":         plan.NewID,
			"old_path":       plan.OldPath,
			"new_path":       plan.NewPath,
			"affected_files": plan.AffectedFiles,
		})
	}

	srv.register(tool, handler)
}

func registerNodeDeleteTool(srv *Server) {
	tool := mcpgo.NewTool("tusk_node_delete",
		mcpgo.WithDescription("Remove a node file and its outgoing edges; incoming edges become dangling."),
		mcpgo.WithString("id", mcpgo.Required(), mcpgo.Description("Node id to delete")),
	)

	handler := func(ctx context.Context, request mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		nodeID, parseErr := argString(request, "id")

		if parseErr != nil {
			return toolError(parseErr), nil
		}

		lockErr := srv.runtime.WithWriteLock(func() error {
			return node.Delete(srv.runtime.Root, srv.runtime.Nodes, srv.runtime.Edges, nodeID)
		})

		if lockErr != nil {
			return toolError(lockErr), nil
		}

		return toolJSON(map[string]any{"deleted_id": nodeID})
	}

	srv.register(tool, handler)
}

func registerEdgeAddTool(srv *Server) {
	tool := mcpgo.NewTool("tusk_edge_add",
		mcpgo.WithDescription("Add a typed edge from source_id to target_id."),
		mcpgo.WithString("type", mcpgo.Required()),
		mcpgo.WithString("source_id", mcpgo.Required()),
		mcpgo.WithString("target_id", mcpgo.Required()),
		mcpgo.WithNumber("ordinal", mcpgo.Description("Optional ordinal (default appends)")),
	)

	handler := func(ctx context.Context, request mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		edgeType, parseErr := argString(request, "type")

		if parseErr != nil {
			return toolError(parseErr), nil
		}

		sourceID, parseErr := argString(request, "source_id")

		if parseErr != nil {
			return toolError(parseErr), nil
		}

		targetID, parseErr := argString(request, "target_id")

		if parseErr != nil {
			return toolError(parseErr), nil
		}

		edgeDef, declared := srv.runtime.Manifest.EdgeTypes[edgeType]

		if !declared {
			return toolError(fmt.Errorf("edge type %q not declared in manifest", edgeType)), nil
		}

		lockErr := srv.runtime.WithWriteLock(func() error {
			sourceRow, sourceErr := srv.runtime.Nodes.Get(sourceID)

			if sourceErr != nil {
				return fmt.Errorf("source: %w", sourceErr)
			}

			if !edgeDef.AllowsSource(sourceRow.Type) {
				return fmt.Errorf("edge type %q does not allow source type %q", edgeType, sourceRow.Type)
			}

			if targetRow, getErr := srv.runtime.Nodes.Get(targetID); getErr == nil {
				if !edgeDef.AllowsTarget(targetRow.Type) {
					return fmt.Errorf("edge type %q does not allow target type %q", edgeType, targetRow.Type)
				}
			}

			if edgeDef.Acyclic {
				existing, listErr := srv.runtime.Edges.ListByType(edgeType)

				if listErr != nil {
					return listErr
				}

				adjacency := map[string][]string{}

				for _, row := range existing {
					adjacency[row.SourceID] = append(adjacency[row.SourceID], row.TargetID)
				}

				if cycleErr := node.DetectCycle(node.CycleProbe{EdgeType: edgeType, Source: sourceID, Target: targetID}, adjacency); cycleErr != nil {
					return cycleErr
				}
			}

			existingForSource, listErr := srv.runtime.Edges.ListBySource(sourceID)

			if listErr != nil {
				return listErr
			}

			var mcpEdges []index.EdgeRow

			for _, row := range existingForSource {
				if row.SourcePath == mcpSourcePath {
					mcpEdges = append(mcpEdges, row)
				}
			}

			ordinal := -1

			for _, row := range mcpEdges {
				if row.Type == edgeType && row.Ordinal > ordinal {
					ordinal = row.Ordinal
				}
			}

			ordinal++

			mcpEdges = append(mcpEdges, index.EdgeRow{
				Type:       edgeType,
				SourceID:   sourceID,
				TargetID:   targetID,
				Ordinal:    ordinal,
				SourcePath: mcpSourcePath,
			})

			return srv.runtime.Edges.UpsertAll(sourceID, mcpSourcePath, mcpEdges)
		})

		if lockErr != nil {
			return toolError(lockErr), nil
		}

		return toolJSON(map[string]any{
			"type":      edgeType,
			"source_id": sourceID,
			"target_id": targetID,
		})
	}

	srv.register(tool, handler)
}

func registerEdgeRemoveTool(srv *Server) {
	tool := mcpgo.NewTool("tusk_edge_remove",
		mcpgo.WithDescription("Remove a typed edge from source_id to target_id."),
		mcpgo.WithString("type", mcpgo.Required()),
		mcpgo.WithString("source_id", mcpgo.Required()),
		mcpgo.WithString("target_id", mcpgo.Required()),
	)

	handler := func(ctx context.Context, request mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		edgeType, parseErr := argString(request, "type")

		if parseErr != nil {
			return toolError(parseErr), nil
		}

		sourceID, parseErr := argString(request, "source_id")

		if parseErr != nil {
			return toolError(parseErr), nil
		}

		targetID, parseErr := argString(request, "target_id")

		if parseErr != nil {
			return toolError(parseErr), nil
		}

		lockErr := srv.runtime.WithWriteLock(func() error {
			rows, listErr := srv.runtime.Edges.ListBySource(sourceID)

			if listErr != nil {
				return listErr
			}

			var kept []index.EdgeRow
			removed := 0

			for _, row := range rows {
				if row.SourcePath != mcpSourcePath {
					continue
				}

				if row.Type == edgeType && row.TargetID == targetID {
					removed++

					continue
				}

				kept = append(kept, row)
			}

			if removed == 0 {
				return fmt.Errorf("no MCP-added edge matches type=%q source=%q target=%q", edgeType, sourceID, targetID)
			}

			counters := map[string]int{}

			for idx := range kept {
				kept[idx].Ordinal = counters[kept[idx].Type]
				counters[kept[idx].Type]++
			}

			return srv.runtime.Edges.UpsertAll(sourceID, mcpSourcePath, kept)
		})

		if lockErr != nil {
			return toolError(lockErr), nil
		}

		return toolJSON(map[string]any{
			"type":      edgeType,
			"source_id": sourceID,
			"target_id": targetID,
			"removed":   true,
		})
	}

	srv.register(tool, handler)
}

func registerReindexTool(srv *Server) {
	tool := mcpgo.NewTool("tusk_reindex",
		mcpgo.WithDescription("Walk the workspace and bring the index up to date with disk."),
		mcpgo.WithBoolean("no_embed", mcpgo.Description("Skip the embedding pass")),
	)

	handler := func(ctx context.Context, request mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		noEmbed, _ := request.GetArguments()["no_embed"].(bool)

		var report *reindex.Report

		lockErr := srv.runtime.WithWriteLock(func() error {
			config := reindex.Config{
				Root:            srv.runtime.Root,
				Repo:            srv.runtime.Nodes,
				Edges:           srv.runtime.Edges,
				EdgeTypes:       srv.runtime.Manifest.EdgeTypes,
				WorkspaceIgnore: srv.runtime.Manifest.Workspace.Ignore,
				Meta:            srv.runtime.Meta,
			}

			if !noEmbed && srv.runtime.Embedder != nil {
				config.EmbedQueue = srv.runtime.EmbedQueue
				config.EmbeddingRepo = srv.runtime.Embeddings
				config.Embedder = srv.runtime.Embedder
				config.Chunker = srv.runtime.Chunker
			}

			out, runErr := reindex.Run(config)

			if runErr != nil {
				return runErr
			}

			report = out

			return nil
		})

		if lockErr != nil {
			return toolError(lockErr), nil
		}

		return toolJSON(map[string]any{
			"indexed": report.Indexed,
			"removed": report.Removed,
			"skipped": report.Skipped,
		})
	}

	srv.register(tool, handler)
}
