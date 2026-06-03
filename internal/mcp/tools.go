package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/germanamz/tusk/internal/aliasdispatch"
	"github.com/germanamz/tusk/internal/behavior/workflow"
	"github.com/germanamz/tusk/internal/contextcompose"
	"github.com/germanamz/tusk/internal/doctor"
	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/node"
	"github.com/germanamz/tusk/internal/query"
	"github.com/germanamz/tusk/internal/reindex"
	"github.com/germanamz/tusk/internal/render"
	"github.com/germanamz/tusk/internal/status"
	"github.com/germanamz/tusk/internal/typeref"
	mcpgo "github.com/mark3labs/mcp-go/mcp"
)

func registerTools(srv *Server) {
	registerHelpTool(srv)
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
	registerRunTool(srv)
	registerContextTool(srv)
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
			"nodes_by_type":       result.NodesByType,
			"edge_count":          result.EdgeCount,
			"embed_queue_depth":   result.EmbedQueueDepth,
			"reindex_queue_depth": result.ReindexQueueDepth,
			"last_reindex_at":     result.LastReindexAt,
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

// requireStrings reads several required string arguments in order, returning
// them positionally or the first argString error (preserving its exact text and
// key precedence). For handlers whose required-string reads are consecutive.
func requireStrings(request mcpgo.CallToolRequest, keys ...string) ([]string, error) {
	values := make([]string, len(keys))

	for index, key := range keys {
		value, parseErr := argString(request, key)

		if parseErr != nil {
			return nil, parseErr
		}

		values[index] = value
	}

	return values, nil
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

// argBoolTriState extracts an optional boolean argument and discriminates
// between "absent" and "present". Returns nil when the key is absent or not
// a bool; otherwise returns a pointer to the supplied value. Used by tri-state
// arguments (e.g. graph_expand) where the caller's intent of "false" must
// override a workspace-default "true".
func argBoolTriState(request mcpgo.CallToolRequest, key string) *bool {
	args := request.GetArguments()
	value, ok := args[key].(bool)

	if !ok {
		return nil
	}

	return &value
}

// mergeGraphExpansionFromRequest folds the workspace manifest's
// GraphExpansion with per-call MCP overrides. Precedence (high to low):
// graph_expand=false → graph_expand=true → workspace enabled flag. Returns
// nil only on invalid input (e.g. weight outside [0,1]).
func mergeGraphExpansionFromRequest(request mcpgo.CallToolRequest, base manifest.GraphExpansion) (*manifest.GraphExpansion, error) {
	resolved := base

	// Struct copy aliases the EdgeTypes slice header. The MCP server fans
	// requests out to multiple goroutines that share the runtime manifest;
	// clone the backing array so a future mutation cannot race with another
	// in-flight request reading the same slice.
	if len(base.EdgeTypes) > 0 {
		cloned := make([]string, len(base.EdgeTypes))
		copy(cloned, base.EdgeTypes)
		resolved.EdgeTypes = cloned
	}

	if expand := argBoolTriState(request, "graph_expand"); expand != nil {
		resolved.Enabled = *expand
	}

	args := request.GetArguments()

	if raw, present := args["hops"]; present {
		hops, ok := coerceMCPInt(raw)

		if !ok {
			return nil, fmt.Errorf("hops: must be a number (got %T)", raw)
		}

		if hops != 1 && hops != 2 {
			return nil, fmt.Errorf("hops: must be 1 or 2 (got %d)", hops)
		}

		resolved.Hops = hops
	}

	if raw, present := args["graph_weight"]; present {
		weight, ok := coerceMCPFloat(raw)

		if !ok {
			return nil, fmt.Errorf("graph_weight: must be a number (got %T)", raw)
		}

		if weight < 0 || weight > 1 {
			return nil, fmt.Errorf("graph_weight: must be in [0.0, 1.0] (got %v)", weight)
		}

		resolved.Weight = weight
	}

	if _, present := args["graph_edge_types"]; present {
		edges := argStringSlice(request, "graph_edge_types")
		// Materialise a copy so we don't share storage with caller-provided
		// state through the JSON decoder.
		copied := make([]string, len(edges))
		copy(copied, edges)
		resolved.EdgeTypes = copied
	}

	return &resolved, nil
}

// coerceMCPInt converts a JSON-decoded number argument to int.
func coerceMCPInt(raw any) (int, bool) {
	switch typed := raw.(type) {
	case float64:
		return int(typed), true
	case int:
		return typed, true
	}

	return 0, false
}

// coerceMCPFloat converts a JSON-decoded number argument to float64.
func coerceMCPFloat(raw any) (float64, bool) {
	switch typed := raw.(type) {
	case float64:
		return typed, true
	case int:
		return float64(typed), true
	}

	return 0, false
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

			if result.IncludeEdges {
				for edgeType, targets := range loaded.Edges {
					for _, target := range targets {
						edgeRefs = append(edgeRefs, query.EdgeRef{
							Type:      edgeType,
							Direction: "out",
							TargetID:  target,
						})
					}
				}
			}

			row := render.CompactRow{
				ID:    loaded.ID,
				Type:  loaded.Type,
				Title: loaded.Title,
				Edges: edgeRefs,
			}

			if result.IncludeBody {
				row.Body = string(loaded.Body)
			}

			if result.IncludeProperties {
				row.Properties = loaded.Properties
			}

			rows := []render.CompactRow{row}

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
				compactRows = append(compactRows, listRowToCompact(row))
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

		req := index.EdgeListRequest{
			From: argStringOptional(request, "from"),
			To:   argStringOptional(request, "to"),
		}

		if typeArg := argStringOptional(request, "type"); typeArg != "" {
			ref, parseErr := typeref.Parse(typeArg)

			if parseErr != nil {
				return toolError(fmt.Errorf("tusk_edge_list: parse type: %w", parseErr)), nil
			}

			req.TypeRef = &ref
		}

		result, runErr := index.EdgeListRun(srv.runtime.Edges, req)

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
		mcpgo.WithNumber("min_score", mcpgo.Description("Minimum similarity score to include in semantic results (default 0.5). Lower this when an initial query misses. When graph expansion is active, this filters the blended final score, not the bare cosine.")),
		mcpgo.WithArray("include", mcpgo.Description("Expand rows: body|edges|properties|units (units = matched sub-units per file; semantic body = best-matching chunk)"), mcpgo.Items(map[string]any{"type": "string"})),
		mcpgo.WithArray("fields", mcpgo.Description("Project rows to these field names"), mcpgo.Items(map[string]any{"type": "string"})),
		mcpgo.WithString("format", mcpgo.Description("Output format: json (default) or compact")),
		mcpgo.WithBoolean("graph_expand", mcpgo.Description("Override the workspace's graph-expansion enabled flag for this call (tri-state: omit to inherit, true to force-on, false to force-off).")),
		mcpgo.WithNumber("hops", mcpgo.Description("Graph-expansion BFS depth (1 or 2). Omit to inherit the manifest's [query.graph-expansion] hops.")),
		mcpgo.WithNumber("graph_weight", mcpgo.Description("Per-hop weight applied to expanded candidates ([0,1]). Omit to inherit the manifest setting.")),
		mcpgo.WithArray("graph_edge_types", mcpgo.Description("Edge-type names used by the graph expander. Omit to inherit the manifest setting."), mcpgo.Items(map[string]any{"type": "string"})),
		mcpgo.WithBoolean("explain", mcpgo.Description("Include a per-row score-contribution trace (cosine_score/graph_score/final_score/distance) in the response when graph expansion is active.")),
	)

	handler := func(ctx context.Context, request mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		filterText, parseErr := argString(request, "filter")

		if parseErr != nil {
			return toolError(parseErr), nil
		}

		includeRaw := argStringSlice(request, "include")
		fields := argStringSlice(request, "fields")
		format := argStringOptional(request, "format")

		graphExpansion, mergeErr := mergeGraphExpansionFromRequest(request, srv.runtime.Manifest.GraphExpansion)

		if mergeErr != nil {
			return toolError(mergeErr), nil
		}

		explain := argBoolOptional(request, "explain", false)

		result, runErr := query.Run(ctx, query.Deps{
			Database:   srv.runtime.Index.DB(),
			Manifest:   srv.runtime.Manifest,
			Embedder:   srv.runtime.Embedder,
			Embeddings: srv.runtime.Embeddings,
			Nodes:      srv.runtime.Nodes,
			Edges:      srv.runtime.Edges,
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
			GraphExpansion:      graphExpansion,
			Explain:             explain,
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
						ID:           row.ID,
						Type:         row.Type,
						Title:        row.Title,
						Body:         row.Body,
						Properties:   row.Properties,
						Edges:        row.Edges,
						MatchedUnits: row.MatchedUnits,
					})
				}
			} else {
				compactRows = make([]render.CompactRow, 0, len(result.Semantic.Ranked))

				for _, scored := range result.Semantic.Ranked {
					compactRows = append(compactRows, render.CompactRow{
						ID:           scored.ID,
						Type:         scored.Type,
						Title:        scored.Title,
						Body:         scored.Body,
						Properties:   scored.Properties,
						Edges:        scored.Edges,
						Score:        scored.Score,
						HasScore:     true,
						MatchedUnits: scored.MatchedUnits,
						CosineScore:  scored.CosineScore,
						GraphScore:   scored.GraphScore,
						FinalScore:   scored.FinalScore,
						Distance:     scored.Distance,
						HasExplain:   explain,
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

				if row.ParentID != "" {
					entry["parent_id"] = row.ParentID
				}

				if row.MatchedUnits != nil {
					entry["matched_units"] = row.MatchedUnits
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
				"id":    scored.ID,
				"score": scored.Score,
				"type":  scored.Type,
				"path":  scored.Path,
				"title": scored.Title,
			}

			if scored.Snippet != "" {
				entry["snippet"] = scored.Snippet
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

			if scored.MatchedUnits != nil {
				entry["matched_units"] = scored.MatchedUnits
			}

			// Explain-trace fields are surfaced only when the caller
			// asked for them (Request.Explain) AND graph expansion
			// actually populated a breakdown. The query service zeroes
			// the four fields when Explain is off, so the inner
			// non-zero guard suppresses the all-zero breakdown that
			// would otherwise appear when Explain is on without graph
			// expansion — emitting it would serve no purpose.
			if explain && (scored.FinalScore != 0 || scored.CosineScore != 0 || scored.GraphScore != 0) {
				entry["cosine_score"] = scored.CosineScore
				entry["graph_score"] = scored.GraphScore
				entry["final_score"] = scored.FinalScore
				entry["distance"] = scored.Distance
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
			"issues":              issues,
			"embed_queue_depth":   report.EmbedQueueDepth,
			"reindex_queue_depth": report.ReindexQueueDepth,
		}

		if len(report.AliasErrors) > 0 {
			response["alias_errors"] = aliasErrorsPayload(report.AliasErrors)
		}

		if len(report.ContextErrors) > 0 {
			contextErrors := make([]map[string]any, 0, len(report.ContextErrors))

			for _, contextErr := range report.ContextErrors {
				contextErrors = append(contextErrors, map[string]any{
					"message": contextErr.Message,
				})
			}

			response["context_errors"] = contextErrors
		}

		if len(report.MissingPinnedIDs) > 0 {
			response["missing_pinned_ids"] = report.MissingPinnedIDs
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

		if report.SubUnitPane != nil {
			pane := report.SubUnitPane
			byKind := make(map[string]any, len(pane.CountByKind))

			for kind, count := range pane.CountByKind {
				byKind[kind] = count
			}

			response["sub_units"] = map[string]any{
				"total":                   pane.Total,
				"count_by_kind":           byKind,
				"deduped_sub_units":       pane.DedupedSubUnits,
				"orphaned_sub_units":      pane.OrphanedSubUnits,
				"embed_queue_files":       pane.EmbedQueueFiles,
				"embed_queue_sub_units":   pane.EmbedQueueSubUnits,
				"oversize_embed_payloads": pane.OversizeEmbedPayloads,
				"reserved_name_conflicts": pane.ReservedNameConflicts,
			}
		}

		if report.GraphExpansion != nil {
			pane := report.GraphExpansion

			response["graph_expansion"] = map[string]any{
				"enabled":              pane.Enabled,
				"hops":                 pane.Hops,
				"weight":               pane.Weight,
				"candidate_multiplier": pane.CandidateMultiplier,
				"edge_types":           pane.EdgeTypes,
				"unknown_edge_types":   pane.UnknownEdgeTypes,
				"weight_zero_no_op":    pane.WeightZeroNoOp,
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
		required, parseErr := requireStrings(request, "path", "type")

		if parseErr != nil {
			return toolError(parseErr), nil
		}

		path, nodeType := required[0], required[1]

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
			return classifyNodeWriteError(createErr), nil
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
		mcpgo.WithDescription("Modify a node's frontmatter properties (set or unset). Cannot change type. Body changes are made by writing to the file directly; the watcher reindexes."),
		mcpgo.WithString("id", mcpgo.Required(), mcpgo.Description("Node id")),
		mcpgo.WithObject("set", mcpgo.Description("Properties to upsert (key→value)")),
		mcpgo.WithArray("unset", mcpgo.Description("Property keys to remove"), mcpgo.Items(map[string]any{"type": "string"})),
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
			srv.runtime.FileState,
			srv.runtime.WorkerID,
			srv.runtime.LeaseTTL,
		)

		modified, modifyErr := perCallService.Modify(input)

		if modifyErr != nil {
			return classifyNodeWriteError(modifyErr), nil
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
		required, parseErr := requireStrings(request, "id", "new_path")

		if parseErr != nil {
			return toolError(parseErr), nil
		}

		nodeID, newPath := required[0], required[1]

		plan, renameErr := node.Rename(
			srv.runtime.Root,
			srv.runtime.Nodes,
			srv.runtime.Edges,
			srv.runtime.FileState,
			srv.runtime.WorkerID,
			srv.runtime.LeaseTTL,
			srv.runtime.Manifest.EdgeTypes,
			srv.runtime.Manifest.NodeTypes,
			nodeID,
			newPath,
		)

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

		if deleteErr := node.Delete(
			srv.runtime.Root,
			srv.runtime.Nodes,
			srv.runtime.Edges,
			srv.runtime.FileState,
			srv.runtime.WorkerID,
			srv.runtime.LeaseTTL,
			nodeID,
		); deleteErr != nil {
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
		required, parseErr := requireStrings(request, "type", "source_id", "target_id")

		if parseErr != nil {
			return toolError(parseErr), nil
		}

		edgeType, sourceID, targetID := required[0], required[1], required[2]

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

		if reindexErr := node.ReindexSource(srv.runtime.Root, srv.runtime.Edges, srv.runtime.Manifest.EdgeTypes, srv.runtime.Manifest.NodeTypes, sourceID); reindexErr != nil {
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
		required, parseErr := requireStrings(request, "type", "source_id", "target_id")

		if parseErr != nil {
			return toolError(parseErr), nil
		}

		edgeType, sourceID, targetID := required[0], required[1], required[2]

		if _, declared := srv.runtime.Manifest.EdgeTypes[edgeType]; !declared {
			return toolError(fmt.Errorf("edge type %q not declared in manifest", edgeType)), nil
		}

		if writeErr := node.RemoveEdgeFromFrontmatter(srv.runtime.Root, sourceID, edgeType, targetID, srv.runtime.Manifest.EdgeTypes); writeErr != nil {
			return toolError(writeErr), nil
		}

		if reindexErr := node.ReindexSource(srv.runtime.Root, srv.runtime.Edges, srv.runtime.Manifest.EdgeTypes, srv.runtime.Manifest.NodeTypes, sourceID); reindexErr != nil {
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
			EmbedQueue:      srv.runtime.EmbedQueue,
			Meta:            srv.runtime.Meta,
			FileStates:      srv.runtime.FileState,
			Workers:         srv.runtime.Workers,
		}

		if !noEmbed && srv.runtime.Embedder != nil {
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

// registerRunTool exposes the manifest-declared alias mechanism over MCP.
// The handler builds an aliasdispatch.Dispatcher off the runtime, runs the
// requested alias by name, and returns a {alias, command, kind, result}
// envelope. format=compact uses the underlying verb's compact renderer.
func registerRunTool(srv *Server) {
	tool := mcpgo.NewTool("tusk_run",
		mcpgo.WithDescription("Invoke a manifest-declared alias by name. Aliases must be declared under [alias.<name>] in tusk.toml and target one of the read-only verbs (node list, node get, query, edge list, doctor, status)."),
		mcpgo.WithString("alias", mcpgo.Required(), mcpgo.Description("Alias name as declared in tusk.toml [alias.<name>]")),
		mcpgo.WithString("format", mcpgo.Description("Output format: json (default) or compact")),
	)

	handler := func(ctx context.Context, request mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		aliasName, parseErr := argString(request, "alias")

		if parseErr != nil {
			return toolError(parseErr), nil
		}

		format := argStringOptional(request, "format")

		alias, ok := srv.runtime.Manifest.Aliases[aliasName]

		if !ok {
			for _, aliasErr := range srv.runtime.Manifest.AliasErrors {
				if aliasErr.Name == aliasName {
					return toolError(fmt.Errorf("alias %q is invalid: %s", aliasName, aliasErr.Message)), nil
				}
			}

			return toolError(fmt.Errorf("alias %q not declared in tusk.toml", aliasName)), nil
		}

		deps := aliasdispatch.Deps{
			Database:            srv.runtime.Index.DB(),
			Manifest:            srv.runtime.Manifest,
			WorkspaceRoot:       srv.runtime.Root,
			NodeService:         srv.runtime.NodeService,
			Nodes:               srv.runtime.Nodes,
			Edges:               srv.runtime.Edges,
			EmbedQueue:          srv.runtime.EmbedQueue,
			WorkflowDrift:       srv.runtime.WorkflowDrift,
			PropertyDrift:       srv.runtime.PropertyDrift,
			Embeddings:          srv.runtime.Embeddings,
			Meta:                srv.runtime.Meta,
			Embedder:            srv.runtime.Embedder,
			SemanticDefaultTake: 10,
		}

		dispatcher := aliasdispatch.NewDispatcher(deps)
		result, dispatchErr := dispatcher.Run(ctx, alias)

		if dispatchErr != nil {
			return toolError(dispatchErr), nil
		}

		if format == "compact" {
			return aliasCompactResult(result)
		}

		return toolJSON(map[string]any{
			"alias":   result.Alias,
			"command": result.Command,
			"kind":    result.Kind,
			"result":  aliasResultJSON(result),
		})
	}

	srv.register(tool, handler)
}

// registerContextTool exposes the manifest-declared [context] block as a
// composable MCP tool. The handler builds a contextcompose.Compose call
// off the runtime, returning the {pinned, recent, aliases} envelope.
func registerContextTool(srv *Server) {
	tool := mcpgo.NewTool("tusk_context",
		mcpgo.WithDescription("Composed warm-context digest: pinned nodes, recent activity, and named aliases per [context] in tusk.toml."),
		mcpgo.WithArray("include", mcpgo.Description("Override the per-node include set (default [body, edges])."), mcpgo.Items(map[string]any{"type": "string"})),
		mcpgo.WithString("format", mcpgo.Description("Output format: json (default) or compact.")),
	)

	handler := func(ctx context.Context, request mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		includeOverride := argStringSlice(request, "include")
		format := argStringOptional(request, "format")

		aliasDeps := aliasdispatch.Deps{
			Database:            srv.runtime.Index.DB(),
			Manifest:            srv.runtime.Manifest,
			WorkspaceRoot:       srv.runtime.Root,
			NodeService:         srv.runtime.NodeService,
			Nodes:               srv.runtime.Nodes,
			Edges:               srv.runtime.Edges,
			EmbedQueue:          srv.runtime.EmbedQueue,
			WorkflowDrift:       srv.runtime.WorkflowDrift,
			PropertyDrift:       srv.runtime.PropertyDrift,
			Embeddings:          srv.runtime.Embeddings,
			Meta:                srv.runtime.Meta,
			Embedder:            srv.runtime.Embedder,
			SemanticDefaultTake: 10,
		}

		dispatcher := aliasdispatch.NewDispatcher(aliasDeps)

		composeDeps := contextcompose.Deps{
			Manifest:      srv.runtime.Manifest,
			Dispatcher:    dispatcher,
			WorkspaceRoot: srv.runtime.Root,
			Database:      srv.runtime.Index.DB(),
		}

		composed, composeErr := contextcompose.Compose(ctx, composeDeps, contextcompose.Request{
			Include: includeOverride,
		})

		if composeErr != nil {
			return toolError(composeErr), nil
		}

		if format == "compact" {
			return contextCompactResult(composed)
		}

		return toolJSON(contextJSONPayload(composed))
	}

	srv.register(tool, handler)
}

// contextJSONPayload builds the JSON envelope tusk_context returns. Mirrors
// cmd/tusk's buildContextJSONPayload.
func contextJSONPayload(result *contextcompose.Result) map[string]any {
	envelope := map[string]any{}

	if len(result.Pinned) > 0 {
		envelope["pinned"] = result.Pinned
	}

	if len(result.Recent) > 0 {
		envelope["recent"] = result.Recent
	}

	if len(result.Aliases) > 0 {
		aliasEnv := make(map[string]any, len(result.Aliases))

		for _, name := range contextcompose.SortedIncludeNames(result) {
			dispatched := result.Aliases[name]

			aliasEnv[name] = map[string]any{
				"kind":   dispatched.Kind,
				"result": aliasResultJSON(dispatched),
			}
		}

		envelope["aliases"] = aliasEnv
	}

	if len(result.MissingPinned) > 0 {
		envelope["missing_pinned"] = result.MissingPinned
	}

	return envelope
}

// contextCompactResult renders the digest as compact text wrapped in a
// CallToolResult. Sections are headed with Markdown-style `# Name` lines so
// agents can locate them with simple regexes.
func contextCompactResult(result *contextcompose.Result) (*mcpgo.CallToolResult, error) {
	var buf bytes.Buffer

	if len(result.Pinned) > 0 {
		buf.WriteString("# Pinned\n")

		if renderErr := writeNodeRowsCompact(&buf, result.Pinned); renderErr != nil {
			return toolError(renderErr), nil
		}
	}

	if len(result.Recent) > 0 {
		if buf.Len() > 0 {
			buf.WriteString("\n")
		}

		buf.WriteString("# Recent\n")

		if renderErr := writeNodeRowsCompact(&buf, result.Recent); renderErr != nil {
			return toolError(renderErr), nil
		}
	}

	for _, name := range contextcompose.SortedIncludeNames(result) {
		if buf.Len() > 0 {
			buf.WriteString("\n")
		}

		fmt.Fprintf(&buf, "# Aliases / %s\n", name)

		aliasBuf, renderErr := aliasCompactResult(result.Aliases[name])

		if renderErr != nil {
			return toolError(renderErr), nil
		}

		for _, content := range aliasBuf.Content {
			if text, ok := content.(mcpgo.TextContent); ok {
				buf.WriteString(text.Text)

				if !strings.HasSuffix(text.Text, "\n") {
					buf.WriteString("\n")
				}
			}
		}
	}

	if len(result.MissingPinned) > 0 {
		if buf.Len() > 0 {
			buf.WriteString("\n")
		}

		buf.WriteString("# Missing pinned\n")

		for _, id := range result.MissingPinned {
			fmt.Fprintf(&buf, "  %s\n", id)
		}
	}

	if buf.Len() == 0 {
		buf.WriteString("tusk context: no [context] block declared in tusk.toml\n")
	}

	return mcpgo.NewToolResultText(buf.String()), nil
}

// listRowToCompact maps a structural list row to the compact render row,
// copying only the structural fields — never Score/MatchedUnits/explain, which
// the structural-compact paths deliberately omit. Shared by the node-list,
// context, and alias-list compact renderers.
func listRowToCompact(row query.ListRow) render.CompactRow {
	return render.CompactRow{
		ID:         row.ID,
		Type:       row.Type,
		Title:      row.Title,
		Body:       row.Body,
		Properties: row.Properties,
		Edges:      row.Edges,
	}
}

// writeNodeRowsCompact is the contextcompose-specific helper that re-uses
// render.CompactNodeRows but accepts the contextcompose row shape directly.
func writeNodeRowsCompact(out *bytes.Buffer, rows []query.ListRow) error {
	compactRows := make([]render.CompactRow, 0, len(rows))

	for _, row := range rows {
		compactRows = append(compactRows, listRowToCompact(row))
	}

	return render.CompactNodeRows(out, compactRows, render.CompactOpts{})
}

// aliasResultJSON converts a DispatchResult into the JSON-friendly payload
// embedded under "result" in the envelope. Mirrors cmd/tusk's
// aliasResultPayload but lives here to keep the MCP package free of a
// dependency on cmd/tusk.
func aliasResultJSON(result *aliasdispatch.DispatchResult) any {
	switch typed := result.Result.(type) {
	case *query.ListResult:
		return map[string]any{
			"rows":  typed.Rows,
			"count": len(typed.Rows),
		}

	case *query.Result:
		if typed.Semantic != nil {
			return map[string]any{
				"results": typed.Semantic.Ranked,
				"count":   len(typed.Semantic.Ranked),
				"model":   typed.Semantic.Model,
			}
		}

		return map[string]any{
			"rows":  typed.Rows,
			"count": len(typed.Rows),
		}

	case *node.GetResult:
		envelope := map[string]any{
			"id":    typed.Node.ID,
			"type":  typed.Node.Type,
			"path":  typed.Node.Path,
			"title": typed.Node.Title,
		}

		if typed.IncludeProperties {
			envelope["properties"] = typed.Node.Properties
		}

		if typed.IncludeEdges {
			envelope["edges"] = typed.Node.Edges
		}

		if typed.IncludeBody {
			envelope["body"] = string(typed.Node.Body)
		}

		return envelope

	case *index.EdgeListResult:
		return map[string]any{
			"rows":  typed.Rows,
			"count": len(typed.Rows),
		}

	case *doctor.Result:
		envelope := map[string]any{
			"issues":              typed.Report.Issues,
			"embed_queue_depth":   typed.Report.EmbedQueueDepth,
			"reindex_queue_depth": typed.Report.ReindexQueueDepth,
		}

		if len(typed.Report.AliasErrors) > 0 {
			envelope["alias_errors"] = aliasErrorsPayload(typed.Report.AliasErrors)
		}

		if typed.Migration != nil {
			envelope["migrated"] = typed.Migration.Migrated
			envelope["skipped"] = typed.Migration.Skipped
		}

		return envelope

	case *status.Result:
		return map[string]any{
			"nodes_by_type":       typed.NodesByType,
			"edge_count":          typed.EdgeCount,
			"embed_queue_depth":   typed.EmbedQueueDepth,
			"reindex_queue_depth": typed.ReindexQueueDepth,
			"last_reindex_at":     typed.LastReindexAt,
		}
	}

	return result.Result
}

// aliasCompactResult emits the alias result through the matching compact
// renderer. Used by tusk_run when the caller passes format=compact.
func aliasCompactResult(result *aliasdispatch.DispatchResult) (*mcpgo.CallToolResult, error) {
	var buf bytes.Buffer

	switch typed := result.Result.(type) {
	case *query.ListResult:
		compactRows := make([]render.CompactRow, 0, len(typed.Rows))

		for _, row := range typed.Rows {
			compactRows = append(compactRows, listRowToCompact(row))
		}

		if renderErr := render.CompactNodeRows(&buf, compactRows, render.CompactOpts{}); renderErr != nil {
			return toolError(renderErr), nil
		}

	case *query.Result:
		if typed.Semantic != nil {
			compactRows := make([]render.CompactRow, 0, len(typed.Semantic.Ranked))

			for _, scored := range typed.Semantic.Ranked {
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

			if renderErr := render.CompactNodeRows(&buf, compactRows, render.CompactOpts{}); renderErr != nil {
				return toolError(renderErr), nil
			}
		} else {
			compactRows := make([]render.CompactRow, 0, len(typed.Rows))

			for _, row := range typed.Rows {
				compactRows = append(compactRows, render.CompactRow{
					ID:         row.ID,
					Type:       row.Type,
					Title:      row.Title,
					Body:       row.Body,
					Properties: row.Properties,
					Edges:      row.Edges,
				})
			}

			if renderErr := render.CompactNodeRows(&buf, compactRows, render.CompactOpts{}); renderErr != nil {
				return toolError(renderErr), nil
			}
		}

	case *index.EdgeListResult:
		entries := make([]render.EdgeListEntry, 0, len(typed.Rows))

		for _, row := range typed.Rows {
			entries = append(entries, render.EdgeListEntry{
				Type:       row.Type,
				SourceID:   row.SourceID,
				TargetID:   row.TargetID,
				SourcePath: row.SourcePath,
			})
		}

		if renderErr := render.CompactEdgeRows(&buf, entries); renderErr != nil {
			return toolError(renderErr), nil
		}

	default:
		// For node get / doctor / status, fall back to JSON inside the
		// compact envelope so the caller still gets a useful payload.
		body, marshalErr := json.Marshal(aliasResultJSON(result))

		if marshalErr != nil {
			return toolError(marshalErr), nil
		}

		buf.Write(body)
	}

	return mcpgo.NewToolResultText(buf.String()), nil
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

// classifyNodeWriteError maps a node create/modify failure to the structured
// MCP result the tools return: a typed JSON rejection for workflow / property /
// ref validation errors (in that precedence order), or a plain-text toolError
// for anything else. Shared by tusk_node_create and tusk_node_modify.
func classifyNodeWriteError(err error) *mcpgo.CallToolResult {
	var workflowErr *workflow.Error

	if errors.As(err, &workflowErr) {
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
		})
	}

	var propErr *node.PropertyValidationError

	if errors.As(err, &propErr) {
		return toolJSONError(buildPropertyRejectionPayload(propErr))
	}

	var refErr *node.RefValidationError

	if errors.As(err, &refErr) {
		return toolJSONError(buildRefRejectionPayload(refErr))
	}

	return toolError(err)
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

// aliasErrorsPayload renders manifest alias-validation errors as the
// {name, message} maps the doctor tool and the tusk_run / tusk_context
// envelopes embed under "alias_errors". Callers keep their own len()>0 guard so
// the empty-omits-the-key behavior stays at the call site.
func aliasErrorsPayload(errs []manifest.AliasError) []map[string]any {
	aliasErrors := make([]map[string]any, 0, len(errs))

	for _, aliasErr := range errs {
		aliasErrors = append(aliasErrors, map[string]any{
			"name":    aliasErr.Name,
			"message": aliasErr.Message,
		})
	}

	return aliasErrors
}
