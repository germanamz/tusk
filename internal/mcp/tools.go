package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/germanamz/tusk/internal/behavior/workflow"
	"github.com/germanamz/tusk/internal/doctor"
	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/node"
	"github.com/germanamz/tusk/internal/query"
	"github.com/germanamz/tusk/internal/reindex"
	"github.com/germanamz/tusk/internal/render"
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
		result, runErr := status.Run(status.Request{
			Nodes:      srv.runtime.Nodes,
			Edges:      srv.runtime.Edges,
			EmbedQueue: srv.runtime.EmbedQueue,
			Meta:       srv.runtime.Meta,
		})

		if runErr != nil {
			return toolError(runErr), nil
		}

		return toolJSON(map[string]any{
			"nodes_by_type":     result.NodesByType,
			"edge_count":        result.EdgeCount,
			"embed_queue_depth": result.EmbedQueueDepth,
			"last_reindex_at":   result.LastReindexAt,
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

// argBoolOptional extracts an optional boolean argument from the request.
// Returns defaultValue when the key is absent or not a bool.
func argBoolOptional(request mcpgo.CallToolRequest, key string, defaultValue bool) bool {
	args := request.GetArguments()
	value, ok := args[key].(bool)

	if !ok {
		return defaultValue
	}

	return value
}

// argFloatOptional extracts an optional numeric argument from the request as a
// float64. Returns defaultValue when the key is absent or not numeric.
func argFloatOptional(request mcpgo.CallToolRequest, key string, defaultValue float64) float64 {
	args := request.GetArguments()

	raw, ok := args[key]
	if !ok {
		return defaultValue
	}

	switch typed := raw.(type) {
	case float64:
		return typed
	case int:
		return float64(typed)
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
		mcpgo.WithDescription("Read a node by id (workspace-relative path without extension). Returns id, type, path, title, plus body / edges / properties when requested via include or fields."),
		mcpgo.WithString("id", mcpgo.Required(), mcpgo.Description("Node id (e.g. \"notes/hi\")")),
		mcpgo.WithArray("include", mcpgo.Description("Expand returned shape: body|edges|properties"), mcpgo.Items(map[string]any{"type": "string"})),
		mcpgo.WithArray("fields", mcpgo.Description("Project returned shape to these field names"), mcpgo.Items(map[string]any{"type": "string"})),
		mcpgo.WithString("format", mcpgo.Description("Output format: json (default) or compact")),
	)

	handler := func(ctx context.Context, request mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		nodeID, parseErr := argString(request, "id")

		if parseErr != nil {
			return toolError(parseErr), nil
		}

		includeRaw := argStringSlice(request, "include")
		fields := argStringSlice(request, "fields")
		format := argStringOptional(request, "format")

		result, runErr := node.GetRun(srv.runtime.NodeService, node.GetRequest{
			ID:      nodeID,
			Include: includeRaw,
			Fields:  fields,
		})

		if runErr != nil {
			return toolError(runErr), nil
		}

		loaded := result.Node
		payload := map[string]any{
			"id":    loaded.ID,
			"type":  loaded.Type,
			"path":  loaded.Path,
			"title": loaded.Title,
		}

		if result.IncludeProperties {
			payload["properties"] = loaded.Properties
		}

		if result.IncludeEdges {
			payload["edges"] = loaded.Edges
		}

		if result.IncludeBody {
			payload["body"] = string(loaded.Body)
		}

		if format == "compact" {
			var edgeRefs []query.EdgeRef

			for edgeType, targets := range loaded.Edges {
				for _, target := range targets {
					edgeRefs = append(edgeRefs, query.EdgeRef{
						Type:      edgeType,
						Direction: "out",
						TargetID:  target,
					})
				}
			}

			rows := []render.CompactRow{{
				ID:         loaded.ID,
				Type:       loaded.Type,
				Title:      loaded.Title,
				Body:       string(loaded.Body),
				Properties: loaded.Properties,
				Edges:      edgeRefs,
			}}

			var buf bytes.Buffer

			if renderErr := render.CompactNodeRows(&buf, rows, render.CompactOpts{Fields: fields}); renderErr != nil {
				return toolError(renderErr), nil
			}

			return mcpgo.NewToolResultText(buf.String()), nil
		}

		return toolJSON(payload)
	}

	srv.register(tool, handler)
}

func registerNodeListTool(srv *Server) {
	tool := mcpgo.NewTool("tusk_node_list",
		mcpgo.WithDescription("List nodes from the index. Optional type filter narrows the result. Use include / fields to expand rows with body / edges / properties in one round-trip. Results are sorted by id ascending by default."),
		mcpgo.WithString("type", mcpgo.Description("Optional node type filter (e.g. \"ticket\"). Empty = all.")),
		mcpgo.WithArray("include", mcpgo.Description("Expand rows: body|edges|properties"), mcpgo.Items(map[string]any{"type": "string"})),
		mcpgo.WithArray("fields", mcpgo.Description("Project rows to these field names"), mcpgo.Items(map[string]any{"type": "string"})),
		mcpgo.WithString("format", mcpgo.Description("Output format: json (default) or compact")),
	)

	handler := func(ctx context.Context, request mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		typeFilter := argStringOptional(request, "type")
		includeRaw := argStringSlice(request, "include")
		fields := argStringSlice(request, "fields")
		format := argStringOptional(request, "format")

		// Build a filter expression equivalent to ListFilter{Type:X} so the
		// CLI and MCP share node.ListRun. An empty type yields an empty
		// filter string, which filter.Parse treats as match-all.
		var filterExpr string

		if typeFilter != "" {
			filterExpr = fmt.Sprintf("type=%s", typeFilter)
		}

		// Default to "+id" so the API contract preserves the historical
		// NodeRepo.List "ORDER BY id ASC" ordering. The CLI does NOT inherit
		// this default — its `tusk node list` retains "no implicit order"
		// behavior unless the user passes --sort.
		result, runErr := query.ListRun(srv.runtime.Index.DB(), srv.runtime.Manifest, query.ListRequest{
			Filter:        filterExpr,
			Sort:          "+id",
			Include:       includeRaw,
			Fields:        fields,
			WorkspaceRoot: srv.runtime.Root,
		})

		if runErr != nil {
			return toolError(runErr), nil
		}

		if format == "compact" {
			compactRows := make([]render.CompactRow, 0, len(result.Rows))

			for _, row := range result.Rows {
				compactRows = append(compactRows, render.CompactRow{
					ID:         row.ID,
					Type:       row.Type,
					Title:      row.Title,
					Body:       row.Body,
					Properties: row.Properties,
					Edges:      row.Edges,
				})
			}

			var buf bytes.Buffer

			if renderErr := render.CompactNodeRows(&buf, compactRows, render.CompactOpts{Fields: fields}); renderErr != nil {
				return toolError(renderErr), nil
			}

			return mcpgo.NewToolResultText(buf.String()), nil
		}

		results := make([]map[string]any, 0, len(result.Rows))

		for _, row := range result.Rows {
			entry := map[string]any{
				"id":    row.ID,
				"type":  row.Type,
				"path":  row.Path,
				"title": row.Title,
			}

			if row.Body != "" {
				entry["body"] = row.Body
			}

			if row.Properties != nil {
				entry["properties"] = row.Properties
			}

			if row.Edges != nil {
				entry["edges"] = row.Edges
			}

			results = append(results, entry)
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
		mcpgo.WithDescription("List edges. Provide from, to, or type to narrow. Use format=compact for tab-aligned text output."),
		mcpgo.WithString("from", mcpgo.Description("Source node id")),
		mcpgo.WithString("to", mcpgo.Description("Target node id")),
		mcpgo.WithString("type", mcpgo.Description("Edge type")),
		mcpgo.WithString("format", mcpgo.Description("Output format: json (default) or compact")),
	)

	handler := func(ctx context.Context, request mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		format := argStringOptional(request, "format")

		result, runErr := index.EdgeListRun(srv.runtime.Edges, index.EdgeListRequest{
			From: argStringOptional(request, "from"),
			To:   argStringOptional(request, "to"),
			Type: argStringOptional(request, "type"),
		})

		if runErr != nil {
			return toolError(runErr), nil
		}

		if format == "compact" {
			entries := make([]render.EdgeListEntry, 0, len(result.Rows))

			for _, row := range result.Rows {
				entries = append(entries, render.EdgeListEntry{
					Type:       row.Type,
					SourceID:   row.SourceID,
					TargetID:   row.TargetID,
					SourcePath: row.SourcePath,
				})
			}

			var buf bytes.Buffer

			if renderErr := render.CompactEdgeRows(&buf, entries); renderErr != nil {
				return toolError(renderErr), nil
			}

			return mcpgo.NewToolResultText(buf.String()), nil
		}

		results := make([]map[string]any, 0, len(result.Rows))

		for _, row := range result.Rows {
			results = append(results, map[string]any{
				"type":        row.Type,
				"source_id":   row.SourceID,
				"target_id":   row.TargetID,
				"source_path": row.SourcePath,
			})
		}

		return toolJSON(map[string]any{"results": results, "count": len(results)})
	}

	srv.register(tool, handler)
}

func registerQueryTool(srv *Server) {
	tool := mcpgo.NewTool("tusk_query",
		mcpgo.WithDescription("Run a structural filter against the workspace, optionally ranked by semantic similarity. Use include / fields to expand rows with body / edges / properties in one round-trip."),
		mcpgo.WithString("filter", mcpgo.Required(), mcpgo.Description("Filter expression (e.g. 'type=ticket status=active')")),
		mcpgo.WithString("sort", mcpgo.Description("Sort spec (e.g. '+priority,-due')")),
		mcpgo.WithNumber("take", mcpgo.Description("Limit results to N rows")),
		mcpgo.WithNumber("skip", mcpgo.Description("Skip the first M rows (requires take)")),
		mcpgo.WithString("semantic", mcpgo.Description("Rank by cosine similarity to this query string")),
		mcpgo.WithNumber("min_score", mcpgo.Description("Minimum cosine similarity to include in semantic results (default 0.5). Lower this when an initial query misses.")),
		mcpgo.WithArray("include", mcpgo.Description("Expand rows: body|edges|properties (semantic body = best-matching chunk)"), mcpgo.Items(map[string]any{"type": "string"})),
		mcpgo.WithArray("fields", mcpgo.Description("Project rows to these field names"), mcpgo.Items(map[string]any{"type": "string"})),
		mcpgo.WithString("format", mcpgo.Description("Output format: json (default) or compact")),
	)

	handler := func(ctx context.Context, request mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		filterText, parseErr := argString(request, "filter")

		if parseErr != nil {
			return toolError(parseErr), nil
		}

		includeRaw := argStringSlice(request, "include")
		fields := argStringSlice(request, "fields")
		format := argStringOptional(request, "format")

		result, runErr := query.Run(ctx, query.Deps{
			Database:   srv.runtime.Index.DB(),
			Manifest:   srv.runtime.Manifest,
			Embedder:   srv.runtime.Embedder,
			Embeddings: srv.runtime.Embeddings,
		}, query.Request{
			Filter:   filterText,
			Sort:     argStringOptional(request, "sort"),
			Take:     argIntOptional(request, "take", 0),
			Skip:     argIntOptional(request, "skip", 0),
			Semantic: argStringOptional(request, "semantic"),
			MinScore: argFloatOptional(request, "min_score", 0.5),
			// MCP keeps tool responses bounded by defaulting semantic page
			// size to 10 when take is unset (CLI returns all ranked rows).
			SemanticDefaultTake: 10,
			Include:             includeRaw,
			Fields:              fields,
			WorkspaceRoot:       srv.runtime.Root,
		})

		if runErr != nil {
			return toolError(runErr), nil
		}

		if format == "compact" {
			var compactRows []render.CompactRow

			if result.Semantic == nil {
				compactRows = make([]render.CompactRow, 0, len(result.Rows))

				for _, row := range result.Rows {
					compactRows = append(compactRows, render.CompactRow{
						ID:         row.ID,
						Type:       row.Type,
						Title:      row.Title,
						Body:       row.Body,
						Properties: row.Properties,
						Edges:      row.Edges,
					})
				}
			} else {
				compactRows = make([]render.CompactRow, 0, len(result.Semantic.Ranked))

				for _, scored := range result.Semantic.Ranked {
					compactRows = append(compactRows, render.CompactRow{
						ID:         scored.ID,
						Type:       scored.Type,
						Title:      scored.Title,
						Body:       scored.Body,
						Properties: scored.Properties,
						Edges:      scored.Edges,
						Score:      scored.Score,
						HasScore:   true,
					})
				}
			}

			var buf bytes.Buffer

			if renderErr := render.CompactNodeRows(&buf, compactRows, render.CompactOpts{Fields: fields}); renderErr != nil {
				return toolError(renderErr), nil
			}

			return mcpgo.NewToolResultText(buf.String()), nil
		}

		if result.Semantic == nil {
			results := make([]map[string]any, 0, len(result.Rows))

			for _, row := range result.Rows {
				entry := map[string]any{
					"id":    row.ID,
					"type":  row.Type,
					"path":  row.Path,
					"title": row.Title,
				}

				if row.Body != "" {
					entry["body"] = row.Body
				}

				if row.Properties != nil {
					entry["properties"] = row.Properties
				}

				if row.Edges != nil {
					entry["edges"] = row.Edges
				}

				results = append(results, entry)
			}

			return toolJSON(map[string]any{"results": results, "count": len(results)})
		}

		semantic := result.Semantic
		ranking := make([]map[string]any, 0, len(semantic.Ranked))

		for _, scored := range semantic.Ranked {
			entry := map[string]any{
				"id":      scored.ID,
				"score":   scored.Score,
				"type":    scored.Type,
				"path":    scored.Path,
				"title":   scored.Title,
				"snippet": scored.Snippet,
			}

			if scored.Body != "" {
				entry["body"] = scored.Body
			}

			if scored.Properties != nil {
				entry["properties"] = scored.Properties
			}

			if scored.Edges != nil {
				entry["edges"] = scored.Edges
			}

			ranking = append(ranking, entry)
		}

		response := map[string]any{
			"results": ranking,
			"count":   len(ranking),
			"model":   semantic.Model,
		}

		if len(ranking) == 0 && semantic.FilteredBelowMinScore > 0 {
			response["filtered_below_min_score"] = semantic.FilteredBelowMinScore
		}

		return toolJSON(response)
	}

	srv.register(tool, handler)
}

func registerDoctorTool(srv *Server) {
	tool := mcpgo.NewTool("tusk_doctor",
		mcpgo.WithDescription("Surface validation warnings and index health issues (dangling edges, embed-queue retries). Auto-migrates legacy __cli__/__mcp__ edge rows back into source frontmatter unless no_migrate is true."),
		mcpgo.WithBoolean("no_migrate", mcpgo.Description("If true, skip the legacy CLI/MCP row migration pass (diagnostic-only run); legacy rows are surfaced as drift issues instead.")),
	)

	handler := func(ctx context.Context, request mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		noMigrate := argBoolOptional(request, "no_migrate", false)

		cfg := doctor.Config{
			Nodes:         srv.runtime.Nodes,
			Edges:         srv.runtime.Edges,
			EmbedQueue:    srv.runtime.EmbedQueue,
			WorkflowDrift: srv.runtime.WorkflowDrift,
			PropertyDrift: srv.runtime.PropertyDrift,
			Embeddings:    srv.runtime.Embeddings,
			Manifest:      srv.runtime.Manifest,
			Root:          srv.runtime.Root,
		}

		runResult, runErr := doctor.RunWithMigration(doctor.Request{Cfg: cfg, NoMigrate: noMigrate})

		if runErr != nil {
			return toolError(runErr), nil
		}

		report := runResult.Report
		migrationReport := runResult.Migration

		issues := make([]map[string]any, 0, len(report.Issues))

		for _, issue := range report.Issues {
			issues = append(issues, map[string]any{
				"kind":    issue.Kind,
				"node_id": issue.NodeID,
				"message": issue.Message,
			})
		}

		response := map[string]any{
			"issues":            issues,
			"embed_queue_depth": report.EmbedQueueDepth,
		}

		if migrationReport != nil {
			response["migrated"] = migrationReport.Migrated
			response["skipped"] = migrationReport.Skipped
			response["migrated_count"] = len(migrationReport.Migrated)
			response["skipped_count"] = len(migrationReport.Skipped)
		}

		if report.EmbedStats != nil {
			stats := report.EmbedStats
			topByChunks := make([]map[string]any, 0, len(stats.TopByChunks))

			for _, entry := range stats.TopByChunks {
				topByChunks = append(topByChunks, map[string]any{
					"node_id": entry.NodeID,
					"chunks":  entry.Chunks,
				})
			}

			response["embed_stats"] = map[string]any{
				"total_nodes":   stats.TotalNodes,
				"total_chunks":  stats.TotalChunks,
				"mean_chunks":   stats.MeanChunks,
				"median_chunks": stats.MedianChunks,
				"max_chunks":    stats.MaxChunks,
				"top_by_chunks": topByChunks,
			}
		}

		return toolJSON(response)
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

		created, createErr := srv.runtime.NodeService.Create(node.CreateInput{
			RelPath:    path,
			Type:       nodeType,
			Title:      title,
			Properties: properties,
			Body:       []byte(body),
		})

		if createErr != nil {
			var workflowErr *workflow.Error

			if errors.As(createErr, &workflowErr) {
				return toolJSONError(map[string]any{
					"error":         "workflow-rejection",
					"code":          string(workflowErr.Code),
					"message":       workflowErr.Error(),
					"property":      workflowErr.Property,
					"from":          workflowErr.From,
					"to":            workflowErr.To,
					"valid_targets": stringSliceOrNil(workflowErr.ValidTargets),
					"known_states":  stringSliceOrNil(workflowErr.KnownStates),
					"pack_instance": workflowErr.PackInstance,
				}), nil
			}

			var propErr *node.PropertyValidationError

			if errors.As(createErr, &propErr) {
				return toolJSONError(buildPropertyRejectionPayload(propErr)), nil
			}

			var refErr *node.RefValidationError

			if errors.As(createErr, &refErr) {
				return toolJSONError(buildRefRejectionPayload(refErr)), nil
			}

			return toolError(createErr), nil
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

		// Build a per-call Service so recovery warnings flow into our local
		// buffer rather than the runtime's shared os.Stderr.
		var warningsBuf bytes.Buffer

		perCallService := node.NewServiceWithBehaviors(
			srv.runtime.Root,
			srv.runtime.Nodes,
			srv.runtime.Edges,
			srv.runtime.Manifest.EdgeTypes,
			srv.runtime.EmbedQueue,
			srv.runtime.Manifest.NodeTypes,
			srv.runtime.PropertyDrift,
			srv.runtime.BehaviorEngine,
			srv.runtime.WorkflowDrift,
			&warningsBuf,
			node.NewIndexRefLookup(srv.runtime.Nodes),
		)

		modified, modifyErr := perCallService.Modify(input)

		if modifyErr != nil {
			var workflowErr *workflow.Error

			if errors.As(modifyErr, &workflowErr) {
				return toolJSONError(map[string]any{
					"error":         "workflow-rejection",
					"code":          string(workflowErr.Code),
					"message":       workflowErr.Error(),
					"property":      workflowErr.Property,
					"from":          workflowErr.From,
					"to":            workflowErr.To,
					"valid_targets": stringSliceOrNil(workflowErr.ValidTargets),
					"known_states":  stringSliceOrNil(workflowErr.KnownStates),
					"pack_instance": workflowErr.PackInstance,
				}), nil
			}

			var propErr *node.PropertyValidationError

			if errors.As(modifyErr, &propErr) {
				return toolJSONError(buildPropertyRejectionPayload(propErr)), nil
			}

			var refErr *node.RefValidationError

			if errors.As(modifyErr, &refErr) {
				return toolJSONError(buildRefRejectionPayload(refErr)), nil
			}

			return toolError(modifyErr), nil
		}

		result := map[string]any{
			"id":         modified.ID,
			"type":       modified.Type,
			"path":       modified.Path,
			"title":      modified.Title,
			"properties": modified.Properties,
		}

		if warningsBuf.Len() > 0 {
			allWarnings := parseRecoveryWarnings(warningsBuf.String(), modified.ID)
			allWarnings = append(allWarnings, parsePropertyDriftWarnings(warningsBuf.String())...)

			if len(allWarnings) > 0 {
				result["warnings"] = allWarnings
			}
		}

		return toolJSON(result)
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

		plan, renameErr := node.Rename(srv.runtime.Root, srv.runtime.Nodes, srv.runtime.Edges, srv.runtime.Manifest.EdgeTypes, nodeID, newPath)

		if renameErr != nil {
			return toolError(renameErr), nil
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

		if deleteErr := node.Delete(srv.runtime.Root, srv.runtime.Nodes, srv.runtime.Edges, nodeID); deleteErr != nil {
			return toolError(deleteErr), nil
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

		sourceRow, sourceErr := srv.runtime.Nodes.Get(sourceID)

		if sourceErr != nil {
			return toolError(fmt.Errorf("source: %w", sourceErr)), nil
		}

		if !edgeDef.AllowsSource(sourceRow.Type) {
			return toolError(fmt.Errorf("edge type %q does not allow source type %q", edgeType, sourceRow.Type)), nil
		}

		if targetRow, getErr := srv.runtime.Nodes.Get(targetID); getErr == nil {
			if !edgeDef.AllowsTarget(targetRow.Type) {
				return toolError(fmt.Errorf("edge type %q does not allow target type %q", edgeType, targetRow.Type)), nil
			}
		}

		if edgeDef.Acyclic {
			existing, listErr := srv.runtime.Edges.ListByType(edgeType)

			if listErr != nil {
				return toolError(listErr), nil
			}

			adjacency := map[string][]string{}

			for _, row := range existing {
				adjacency[row.SourceID] = append(adjacency[row.SourceID], row.TargetID)
			}

			if cycleErr := node.DetectCycle(node.CycleProbe{EdgeType: edgeType, Source: sourceID, Target: targetID}, adjacency); cycleErr != nil {
				return toolError(cycleErr), nil
			}
		}

		if writeErr := node.AddEdgeToFrontmatter(srv.runtime.Root, sourceID, edgeType, targetID, srv.runtime.Manifest.EdgeTypes); writeErr != nil {
			return toolError(writeErr), nil
		}

		if reindexErr := node.ReindexSource(srv.runtime.Root, srv.runtime.Edges, srv.runtime.Manifest.EdgeTypes, sourceID); reindexErr != nil {
			return toolError(reindexErr), nil
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
		mcpgo.WithDescription("Remove an edge from the source node's frontmatter; the index is updated to match."),
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

		if _, declared := srv.runtime.Manifest.EdgeTypes[edgeType]; !declared {
			return toolError(fmt.Errorf("edge type %q not declared in manifest", edgeType)), nil
		}

		if writeErr := node.RemoveEdgeFromFrontmatter(srv.runtime.Root, sourceID, edgeType, targetID, srv.runtime.Manifest.EdgeTypes); writeErr != nil {
			return toolError(writeErr), nil
		}

		if reindexErr := node.ReindexSource(srv.runtime.Root, srv.runtime.Edges, srv.runtime.Manifest.EdgeTypes, sourceID); reindexErr != nil {
			return toolError(reindexErr), nil
		}

		// Back-compat: also clear any legacy __cli__/__mcp__ row for this triple.
		legacy, listErr := srv.runtime.Edges.ListBySource(sourceID)

		if listErr != nil {
			return toolError(fmt.Errorf("edge remove: list legacy rows: %w", listErr)), nil
		}

		var keptLegacyCLI, keptLegacyMCP []index.EdgeRow

		for _, row := range legacy {
			matchesTriple := row.Type == edgeType && row.TargetID == targetID

			switch row.SourcePath {
			case index.CLISourcePath:
				if !matchesTriple {
					keptLegacyCLI = append(keptLegacyCLI, row)
				}
			case index.MCPSourcePath:
				if !matchesTriple {
					keptLegacyMCP = append(keptLegacyMCP, row)
				}
			}
		}

		if upsertErr := srv.runtime.Edges.UpsertAll(sourceID, index.CLISourcePath, keptLegacyCLI); upsertErr != nil {
			return toolError(upsertErr), nil
		}

		if upsertErr := srv.runtime.Edges.UpsertAll(sourceID, index.MCPSourcePath, keptLegacyMCP); upsertErr != nil {
			return toolError(upsertErr), nil
		}

		return toolJSON(map[string]any{
			"type":      edgeType,
			"source_id": sourceID,
			"target_id": targetID,
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

		report, runErr := reindex.Run(config)

		if runErr != nil {
			return toolError(runErr), nil
		}

		return toolJSON(map[string]any{
			"indexed": report.Indexed,
			"removed": report.Removed,
			"skipped": report.Skipped,
		})
	}

	srv.register(tool, handler)
}

// toolJSONError builds a CallToolResult with IsError=true and a JSON-encoded
// body. Mirrors toolJSON's success path.
func toolJSONError(payload map[string]any) *mcpgo.CallToolResult {
	body, marshalErr := json.Marshal(payload)

	if marshalErr != nil {
		return mcpgo.NewToolResultError(fmt.Sprintf("toolJSONError: %v", marshalErr))
	}

	return &mcpgo.CallToolResult{
		IsError: true,
		Content: []mcpgo.Content{mcpgo.NewTextContent(string(body))},
	}
}

// buildPropertyRejectionPayload constructs the structured node-types-rejection
// envelope from a PropertyValidationError. Per spec §7.2.
func buildPropertyRejectionPayload(propErr *node.PropertyValidationError) map[string]any {
	errItems := make([]map[string]any, 0, len(propErr.Errors))

	for _, pe := range propErr.Errors {
		item := map[string]any{
			"kind":     propertyErrorKindString(pe.Kind),
			"property": pe.Property,
			"type":     pe.Type,
			"reason":   pe.Reason,
		}

		if pe.Value != nil {
			item["value"] = pe.Value
		}

		errItems = append(errItems, item)
	}

	return map[string]any{
		"error":     "node-types-rejection",
		"node_id":   propErr.NodeID,
		"node_type": propErr.NodeType,
		"op":        propErr.Op,
		"errors":    errItems,
	}
}

// propertyErrorKindString maps a PropertyErrorKind to its JSON string.
func propertyErrorKindString(kind node.PropertyErrorKind) string {
	switch kind {
	case node.ErrTypeMismatch:
		return "type-mismatch"
	case node.ErrRequiredMissing:
		return "required-missing"
	case node.ErrEnumViolation:
		return "enum-violation"
	case node.ErrCannotUnsetRequired:
		return "cannot-unset-required"
	default:
		return "unknown"
	}
}

// parsePropertyDriftWarnings extracts property-drift warning entries from the
// Service's stderr output. Format:
//
//	warning: node-types: property "<P>" is not declared on type "<T>"; ...
func parsePropertyDriftWarnings(buf string) []map[string]any {
	var warnings []map[string]any

	for _, line := range strings.Split(strings.TrimSpace(buf), "\n") {
		if !strings.HasPrefix(line, "warning: node-types: property ") {
			continue
		}

		// Extract the property name (first quoted string after "property ").
		prop := extractQuoted(line, 0)

		if prop == "" {
			continue
		}

		warnings = append(warnings, map[string]any{
			"kind":     "property-drift",
			"property": prop,
			"message":  line,
		})
	}

	return warnings
}

// parseRecoveryWarnings turns the Service's stderr warning lines into a
// structured slice. Format produced by node.Service:
//
//	warning: workflow "tickets" recovered from unknown status "blocked" → "active" on tickets/foo; transition not validated
func parseRecoveryWarnings(buf, nodeID string) []map[string]any {
	var warnings []map[string]any

	for _, line := range strings.Split(strings.TrimSpace(buf), "\n") {
		if !strings.HasPrefix(line, "warning: workflow ") {
			continue
		}

		// Extract instance name between the first pair of quotes.
		instance := extractQuoted(line, 0)
		from := extractQuoted(line, 1)
		to := extractQuoted(line, 2)

		warnings = append(warnings, map[string]any{
			"kind":          "workflow-recovered",
			"pack_instance": instance,
			"from":          from,
			"to":            to,
			"property":      "status",
			"message":       line,
		})
	}

	return warnings
}

// extractQuoted extracts the occurrence-th quoted string from line.
// occurrence=0 means the first quoted string.
func extractQuoted(line string, occurrence int) string {
	count := 0
	start := -1

	for idx, ch := range line {
		if ch != '"' {
			continue
		}

		if start == -1 {
			start = idx + 1

			continue
		}

		if count == occurrence {
			return line[start:idx]
		}

		count++
		start = -1
	}

	return ""
}

// stringSliceOrNil returns nil for an empty slice and the slice as any otherwise.
// Used to omit empty arrays from the JSON payload.
func stringSliceOrNil(values []string) any {
	if len(values) == 0 {
		return nil
	}

	return values
}

// buildRefRejectionPayload constructs the structured ref-rejection envelope from
// a RefValidationError. Returns ok:false with a per-error list carrying kind,
// property, value, to, and optional candidates/actual_type/reason fields.
func buildRefRejectionPayload(refErr *node.RefValidationError) map[string]any {
	rendered := make([]map[string]any, 0, len(refErr.Errors))

	for _, refError := range refErr.Errors {
		item := map[string]any{
			"kind":     string(refError.Kind),
			"property": refError.Property,
			"value":    refError.Value,
			"to":       refError.To,
			"reason":   refError.Reason,
		}

		if len(refError.Candidates) > 0 {
			item["candidates"] = refError.Candidates
		}

		if refError.ActualType != "" {
			item["actual_type"] = refError.ActualType
		}

		rendered = append(rendered, item)
	}

	return map[string]any{
		"ok":     false,
		"errors": rendered,
	}
}
