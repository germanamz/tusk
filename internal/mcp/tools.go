package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/germanamz/tusk/internal/aliasdispatch"
	"github.com/germanamz/tusk/internal/behavior/workflow"
	"github.com/germanamz/tusk/internal/contextcompose"
	"github.com/germanamz/tusk/internal/doctor"
	"github.com/germanamz/tusk/internal/epoch"
	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/lock"
	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/node"
	"github.com/germanamz/tusk/internal/query"
	"github.com/germanamz/tusk/internal/reindex"
	"github.com/germanamz/tusk/internal/render"
	"github.com/germanamz/tusk/internal/reset"
	"github.com/germanamz/tusk/internal/status"
	"github.com/germanamz/tusk/internal/typepacks"
	"github.com/germanamz/tusk/internal/typeref"
	mcpgo "github.com/mark3labs/mcp-go/mcp"
)

func registerTools(srv *Server) {
	registerHelpTool(srv)
	registerStatusTool(srv)
	registerNodeGetTool(srv)
	registerNodeRenderTool(srv)
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
	registerResetTool(srv)
	registerReloadTool(srv)
	registerPackAddTool(srv)
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

	value, coerced := coerceMCPInt(raw)

	if !coerced {
		return 0, fmt.Errorf("argument %q is not a number", key)
	}

	return value, nil
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

	if value, coerced := coerceMCPFloat(raw); coerced {
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

func registerNodeRenderTool(srv *Server) {
	tool := mcpgo.NewTool("tusk_node_render",
		mcpgo.WithDescription("Render a node's content as plain text. HTML nodes have tags stripped and entities decoded; markdown nodes have markup removed. Read-only — touches no files or index state. The id is the workspace-relative path (markdown drops the extension, HTML retains it)."),
		mcpgo.WithString("id", mcpgo.Required(), mcpgo.Description("Node id (e.g. \"notes/hi\" or \"page.html\")")),
	)

	handler := func(ctx context.Context, request mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		nodeID, parseErr := argString(request, "id")

		if parseErr != nil {
			return toolError(parseErr), nil
		}

		row, getErr := srv.runtime.Nodes.Get(nodeID)

		if getErr != nil {
			return toolError(getErr), nil
		}

		body, readErr := os.ReadFile(filepath.Join(srv.runtime.Root, row.Path))

		if readErr != nil {
			return toolError(readErr), nil
		}

		return mcpgo.NewToolResultText(render.NodeText(row.Path, body)), nil
	}

	srv.register(tool, handler)
}

func registerNodeListTool(srv *Server) {
	tool := mcpgo.NewTool("tusk_node_list",
		mcpgo.WithDescription("List nodes by type from the index — a convenience wrapper. For property / edge / hierarchy / recency filters, sorting, or semantic ranking, use tusk_query instead (it does everything `tusk node list` does and more). Use include / fields to expand rows with body / edges / properties in one round-trip. Sorted by id ascending. Returns up to 50 rows by default — raise take (with skip) to page through more."),
		mcpgo.WithString("type", mcpgo.Description("Optional node type filter (e.g. \"ticket\"). Empty = all.")),
		mcpgo.WithArray("include", mcpgo.Description("Expand rows: body|edges|properties"), mcpgo.Items(map[string]any{"type": "string"})),
		mcpgo.WithArray("fields", mcpgo.Description("Project rows to these field names"), mcpgo.Items(map[string]any{"type": "string"})),
		mcpgo.WithNumber("take", mcpgo.Description("Limit results to N rows (default 50)")),
		mcpgo.WithNumber("skip", mcpgo.Description("Skip the first M rows (requires take)")),
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
			Take:          argIntOptional(request, "take", 0),
			Skip:          argIntOptional(request, "skip", 0),
			Include:       includeRaw,
			Fields:        fields,
			WorkspaceRoot: srv.runtime.Root,
			// Bound tool output by default; the CLI `tusk node list` stays
			// uncapped. Raising take past 50 pages through more.
			StructuralDefaultTake: 50,
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
		mcpgo.WithDescription("Run a structural, semantic, or hybrid query — the MCP equivalent of `tusk query` (no shell needed). Filter grammar: property predicates key=value / key:value / key!=value / key<|<=|>|>=value and ranges key=lo..hi; compose with AND / OR / NOT and parentheses; edge traversal edge-type-> (outgoing) and edge-type<- (incoming), chainable multi-hop (mentions-> tagged-> type=tag); hierarchy shortcuts tree=id / parent=id / root=id; recency modified-since:7d (or an ISO date). Add semantic=\"...\" to rank by cosine similarity. Results default to 50 rows (structural) or 10 (semantic) — raise take for more. Use include / fields to expand or project rows in one round-trip. Full grammar: tusk_help(topic: \"filter\")."),
		mcpgo.WithString("filter", mcpgo.Required(), mcpgo.Description("Filter expression, e.g. 'type=ticket AND priority>=2 AND modified-since:7d'. Empty string matches everything (useful as a semantic pre-filter). See the tool description for the grammar.")),
		mcpgo.WithString("sort", mcpgo.Description("Sort spec (e.g. '+priority,-due')")),
		mcpgo.WithNumber("take", mcpgo.Description("Limit results to N rows (default 50 structural, 10 semantic)")),
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
			// size to 10 and structural reads to 50 when take is unset (the CLI
			// leaves both uncapped and returns every matching row).
			SemanticDefaultTake:   10,
			StructuralDefaultTake: 50,
			Include:               includeRaw,
			Fields:                fields,
			WorkspaceRoot:         srv.runtime.Root,
			GraphExpansion:        graphExpansion,
			Explain:               explain,
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
		mcpgo.WithDescription("Modify a node's frontmatter properties (set/unset) and/or replace its body — no file edit needed. Cannot change type. Pass body to overwrite the markdown after the frontmatter (body wikilinks materialize into edges, as in tusk_node_create); omit body to leave it untouched."),
		mcpgo.WithString("id", mcpgo.Required(), mcpgo.Description("Node id")),
		mcpgo.WithObject("set", mcpgo.Description("Properties to upsert (key→value)")),
		mcpgo.WithArray("unset", mcpgo.Description("Property keys to remove"), mcpgo.Items(map[string]any{"type": "string"})),
		mcpgo.WithString("body", mcpgo.Description("Replace the markdown body (everything after the frontmatter). Omit to leave the body unchanged; pass an empty string to clear it.")),
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

		// Distinguish an absent body (leave it untouched) from a present-but-empty
		// body (clear it): only set input.Body when the caller supplied the key.
		if rawBody, present := request.GetArguments()["body"]; present {
			if bodyStr, ok := rawBody.(string); ok {
				input.Body = []byte(bodyStr)
			}
		}

		// Derive a per-call Service so recovery warnings flow into our local
		// buffer rather than the runtime's shared os.Stderr. Every other
		// dependency is shared with the runtime's NodeService.
		var warningsBuf bytes.Buffer

		perCallService := srv.runtime.NodeService.WithWarningWriter(&warningsBuf)

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
		mcpgo.WithDescription("Add a typed edge from source_id to target_id — the MCP equivalent of `tusk edge add` (no shell needed). The edge type must be declared in tusk.toml under [edge-types.<name>]; the source and target node types must satisfy its from/to lists; acyclic edge types reject cycles at write time; re-adding the same edge is a no-op. The edge is written to the source node's frontmatter (the file stays the source of truth) and the index is brought into line. See tusk_help(topic: \"edge-types\")."),
		mcpgo.WithString("type", mcpgo.Required(), mcpgo.Description("Edge type — must be declared in tusk.toml under [edge-types.<name>].")),
		mcpgo.WithString("source_id", mcpgo.Required(), mcpgo.Description("Source node id: a workspace-relative path without extension (e.g. tickets/T-001).")),
		mcpgo.WithString("target_id", mcpgo.Required(), mcpgo.Description("Target node id: a workspace-relative path without extension (e.g. tickets/auth-epic).")),
	)

	handler := func(ctx context.Context, request mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		required, parseErr := requireStrings(request, "type", "source_id", "target_id")

		if parseErr != nil {
			return toolError(parseErr), nil
		}

		edgeType, sourceID, targetID := required[0], required[1], required[2]

		if addErr := srv.runtime.NodeService.AddEdge(edgeType, sourceID, targetID); addErr != nil {
			return toolError(edgeStaleSchemaHint(addErr)), nil
		}

		return toolJSON(map[string]any{
			"type":      edgeType,
			"source_id": sourceID,
			"target_id": targetID,
		})
	}

	srv.register(tool, handler)
}

// edgeStaleSchemaHint augments a "not declared" edge error with the MCP-specific
// nudge that the daemon's cached schema may be stale — the CLI reloads the
// manifest each run, but the daemon must be told to tusk_reload. Other errors
// pass through unchanged.
func edgeStaleSchemaHint(err error) error {
	if errors.Is(err, node.ErrEdgeTypeNotDeclared) {
		return fmt.Errorf("%w — if you just edited tusk.toml, the daemon's cached schema is stale; call tusk_reload, then retry", err)
	}

	return err
}

func registerEdgeRemoveTool(srv *Server) {
	tool := mcpgo.NewTool("tusk_edge_remove",
		mcpgo.WithDescription("Remove a typed edge from the source node's frontmatter — the MCP equivalent of `tusk edge remove` (no shell needed). The index is updated to match; removing an edge that is not present is a no-op. See tusk_help(topic: \"edge-types\")."),
		mcpgo.WithString("type", mcpgo.Required(), mcpgo.Description("Edge type — must be declared in tusk.toml under [edge-types.<name>].")),
		mcpgo.WithString("source_id", mcpgo.Required(), mcpgo.Description("Source node id: a workspace-relative path without extension (e.g. tickets/T-001).")),
		mcpgo.WithString("target_id", mcpgo.Required(), mcpgo.Description("Target node id: a workspace-relative path without extension (e.g. tickets/auth-epic).")),
	)

	handler := func(ctx context.Context, request mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		required, parseErr := requireStrings(request, "type", "source_id", "target_id")

		if parseErr != nil {
			return toolError(parseErr), nil
		}

		edgeType, sourceID, targetID := required[0], required[1], required[2]

		if removeErr := srv.runtime.NodeService.RemoveEdge(edgeType, sourceID, targetID); removeErr != nil {
			return toolError(edgeStaleSchemaHint(removeErr)), nil
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
		mcpgo.WithDescription("Walk the workspace and bring the index up to date with disk — the MCP equivalent of `tusk reindex`. The embedding pass runs over Ollama and can be slow on a large vault; pass no_embed=true for a fast structural-only pass (embeddings still drain in the background). The response reports indexed/removed/skipped plus the remaining embed_queue_depth — poll tusk_status to watch it drain."),
		mcpgo.WithBoolean("no_embed", mcpgo.Description("Skip the synchronous embedding pass and return as soon as the structural index is up to date; pending embeddings drain in the background.")),
	)

	handler := func(ctx context.Context, request mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		noEmbed := argBoolOptional(request, "no_embed", false)

		rt := srv.snapshotRuntime() // run the (long) reindex off the read-lock

		// Unlike the Async walks (watch, reload), this tool drains inline, so
		// its config must carry the full per-file set — validators, drift
		// repos, manifest — or the rows it claims are processed with weaker
		// semantics than the background drainer's (no ref resolution, no
		// sub-unit sync) and recorded ref drift is never retried.
		config := reindex.Config{
			Root:            rt.Root,
			Repo:            rt.Nodes,
			Edges:           rt.Edges,
			EdgeTypes:       rt.Manifest.EdgeTypes,
			WorkspaceIgnore: rt.Manifest.Workspace.Ignore,
			EmbedQueue:      rt.EmbedQueue,
			Meta:            rt.Meta,
			FileStates:      rt.FileState,
			Workers:         rt.Workers,
			Manifest:        rt.Manifest,
			Behaviors:       rt.BehaviorEngine,
			DriftLog:        rt.WorkflowDrift,
			NodeTypes:       rt.Manifest.NodeTypes,
			PropertyDrift:   rt.PropertyDrift,
		}

		if !noEmbed && rt.Embedder != nil {
			config.EmbeddingRepo = rt.Embeddings
			config.Embedder = rt.Embedder
			config.Chunker = rt.Chunker
		}

		report, runErr := reindex.Run(config)

		if runErr != nil {
			return toolError(runErr), nil
		}

		result := map[string]any{
			"indexed":    report.Indexed,
			"removed":    report.Removed,
			"skipped":    report.Skipped,
			"ref_healed": report.RefHealed,
		}

		// Surface remaining embed work so the agent can see embeddings are
		// still pending (they drain in the background) rather than assuming the
		// node is immediately semantically searchable. Best-effort: a depth
		// read error is non-fatal to the reindex result.
		if depth, depthErr := rt.EmbedQueue.Depth(); depthErr == nil {
			result["embed_queue_depth"] = depth
		}

		return toolJSON(result)
	}

	srv.registerWrite(tool, handler)
}

// registerResetTool exposes the destructive index reset over MCP. The handler is
// registered via registerWrite (NO read-lock wrapper) because it takes the
// runtime write-lock itself; a read-locked handler upgrading to a write-lock
// would deadlock. The cross-process flock is acquired OUTSIDE srv.mu so the
// daemon keeps serving reads during a (possibly contended) flock-await; the
// write-lock window is only the brief structural swap.
func registerResetTool(srv *Server) {
	tool := mcpgo.NewTool("tusk_reset",
		mcpgo.WithDescription("Drop the local index and rebuild it from source files. DESTRUCTIVE: deletes .tusk/index.db (and WAL/SHM) and the embed queue, then rebuilds from disk — every node is re-embedded. Markdown files are the source of truth, so no content is lost. Requires confirm=true."),
		mcpgo.WithBoolean("confirm", mcpgo.Description("Must be true to proceed.")),
	)

	handler := func(ctx context.Context, request mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		if !argBoolOptional(request, "confirm", false) {
			return toolError(errors.New("tusk_reset requires confirm=true: it deletes .tusk/index.db and the embed queue, then rebuilds from disk (every node is re-embedded)")), nil
		}

		// resetMu serializes against a concurrent sibling reopen (Phase 7) so the
		// flock and the runtime write-lock cannot be acquired in conflicting
		// orders within this process.
		srv.resetMu.Lock()
		defer srv.resetMu.Unlock()

		rt := srv.snapshotRuntime()
		root, indexPath := rt.Root, rt.IndexPath

		// Acquire the cross-process flock OUTSIDE srv.mu: the daemon keeps serving
		// reads during the (possibly contended) flock-await, up to LockTTL.
		lockHandle, lockErr := reset.AcquireLock(ctx, root, 5*time.Second)
		if lockErr != nil {
			return toolError(lockErr), nil
		}

		defer func() { _ = lockHandle.Release() }()

		// Brief write-lock: close → delete → reap → reopen → bump → install. No
		// Ollama/embed work happens under it.
		srv.mu.Lock()
		old := srv.runtime

		result, resetErr := reset.PerformLocked(reset.Config{
			Root:      root,
			IndexPath: indexPath,
			Quiesce:   func() error { old.Index.Close(); return nil },
			Reopen:    func() (*index.Index, error) { return index.Open(indexPath) },
		})
		if resetErr != nil {
			// Quiesce already closed old.Index; recover a live handle so the daemon
			// keeps serving (PerformLocked does not reopen on its own error paths).
			if store, reopenErr := index.Open(indexPath); reopenErr == nil {
				if installErr := srv.installStoreLocked(store, old.Manifest); installErr != nil {
					store.Close()
					srv.mu.Unlock()

					return toolError(fmt.Errorf("%w (recovery reopen failed: %v; restart the daemon)", resetErr, installErr)), nil
				}
			} else {
				srv.mu.Unlock()

				return toolError(fmt.Errorf("%w (recovery reopen failed: %v; restart the daemon)", resetErr, reopenErr)), nil
			}

			srv.mu.Unlock()

			return toolError(resetErr), nil
		}

		if installErr := srv.installStoreLocked(result.Store, old.Manifest); installErr != nil {
			result.Store.Close()
			srv.mu.Unlock()

			return toolError(installErr), nil
		}

		fresh := srv.runtime              // the freshly-installed runtime
		srv.seenEpoch.Store(result.Epoch) // record our own bump; MUST be under srv.mu, before Unlock
		srv.mu.Unlock()

		report, runErr := reindex.Run(reindex.Config{
			Root:            fresh.Root,
			Repo:            fresh.Nodes,
			Edges:           fresh.Edges,
			EdgeTypes:       fresh.Manifest.EdgeTypes,
			WorkspaceIgnore: fresh.Manifest.Workspace.Ignore,
			EmbedQueue:      fresh.EmbedQueue,
			Meta:            fresh.Meta,
			FileStates:      fresh.FileState,
			PropertyDrift:   fresh.PropertyDrift,
			Workers:         fresh.Workers,
			Async:           true,
		})
		if runErr != nil {
			return toolError(runErr), nil
		}

		return toolJSON(map[string]any{
			"indexed":           report.Indexed,
			"removed":           report.Removed,
			"skipped":           report.Skipped,
			"epoch":             result.Epoch,
			"deleted_artifacts": result.DeletedArtifacts,
		})
	}

	srv.registerWrite(tool, handler)
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
			"result":  aliasdispatch.ResultPayload(result),
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

		return toolJSON(contextcompose.JSONPayload(composed))
	}

	srv.register(tool, handler)
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

// registerReloadTool exposes the manifest reload over MCP. The handler is
// registered via registerWrite (NO read-lock wrapper) because it takes the
// runtime write-lock itself and performs a cross-process flock acquire.
// The flock is acquired OUTSIDE srv.mu so the daemon keeps serving reads
// during the (possibly contended) flock-await; the write-lock window is
// only the brief structural swap. Args: no_reindex (bool, default false),
// no_embed (bool, default false). The handler body is the package-level
// reloadToolHandler so unit tests can call it directly.
func registerReloadTool(srv *Server) {
	tool := mcpgo.NewTool("tusk_reload",
		mcpgo.WithDescription("Hot reload the manifest: re-read tusk.toml, validate, swap the in-memory schema and behavior engine, bump .tusk/manifest-epoch to notify siblings, and kick a reindex pass. Returns the manifest-epoch, a diff (added/removed node-types, edge-types, behaviors), and the reindex report."),
		mcpgo.WithBoolean("no_reindex", mcpgo.Description("Skip the reindex pass (default false)")),
		mcpgo.WithBoolean("no_embed", mcpgo.Description("Skip the embedding phase of the reindex (default false)")),
	)

	handler := func(ctx context.Context, request mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		return reloadToolHandler(ctx, request, srv)
	}

	srv.registerWrite(tool, handler)
}

// registerPackAddTool exposes `tusk pack add` over MCP so an agent can seed or
// extend a workspace's schema without shelling out. It merges the pack's
// node/edge-type declarations into ./tusk.toml (via typepacks.AddPack, which
// takes the workspace flock for the write) and then hot-reloads the schema by
// delegating to reloadToolHandler — so the daemon picks up the new types in
// place and the response is the same {manifest_epoch, diff, reindex} envelope as
// tusk_reload. Registered via registerWrite (NO read-lock wrapper) because
// reloadToolHandler takes the runtime write-lock itself; a read-locked handler
// upgrading to the write-lock would deadlock. AddPack releases its flock before
// returning, so the subsequent reload re-acquires it without contention.
func registerPackAddTool(srv *Server) {
	tool := mcpgo.NewTool("tusk_pack_add",
		mcpgo.WithDescription("Merge a built-in type pack's node-type and edge-type declarations into ./tusk.toml and hot-reload the schema — the MCP equivalent of `tusk pack add`, so you can set up or extend a workspace's types without shelling out. Fails on a section collision unless force=true. Returns the same result as tusk_reload (manifest-epoch, schema diff, reindex report)."),
		mcpgo.WithString("pack", mcpgo.Required(), mcpgo.Description("Built-in pack name (vault, tags, kanban — see tusk_help(topic: \"packs\")) or a full http(s)://tusk.toml URL.")),
		mcpgo.WithBoolean("force", mcpgo.Description("Replace colliding [node-types]/[edge-types] sections instead of failing on a collision (default false).")),
	)

	handler := func(ctx context.Context, request mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		packName, parseErr := argString(request, "pack")

		if parseErr != nil {
			return toolError(parseErr), nil
		}

		force := argBoolOptional(request, "force", false)

		root := srv.snapshotRuntime().Root

		if addErr := typepacks.AddPack(ctx, packName, force, root); addErr != nil {
			return toolError(addErr), nil
		}

		// Hot-reload so the just-merged node/edge types are live in this daemon
		// (and converge into siblings) without a restart.
		return reloadToolHandler(ctx, request, srv)
	}

	srv.registerWrite(tool, handler)
}

// reloadToolHandler is the package-level body of tusk_reload, extracted so unit
// tests can invoke it directly without wiring the full MCP transport. It
// snapshots the runtime, acquires the cross-process flock OUTSIDE srv.mu,
// validates+builds a fresh Runtime via rt.buildReloaded() (which loads, merges
// packs, runs the lenient alias/context gates, and builds the behavior engine —
// returning an error on a blocking failure), swaps it under the write-lock,
// bumps the manifest-epoch, then (unless no_reindex) kicks an async reindex
// under reindexMu.
func reloadToolHandler(ctx context.Context, request mcpgo.CallToolRequest, srv *Server) (*mcpgo.CallToolResult, error) {
	noReindex := argBoolOptional(request, "no_reindex", false)
	noEmbed := argBoolOptional(request, "no_embed", false)

	// reloadMu serializes originating reloads so epochs advance monotonically
	// and only one owner reindexes. It (and the flock below) are released
	// EXPLICITLY before the reindex pass (spec §6a step 9→10) via guarded
	// closures; the deferred calls are backstops that fire on the early
	// error-return paths. The guards make the explicit release + the defer
	// idempotent (calling Unlock twice on a sync.Mutex would panic).
	srv.reloadMu.Lock()
	reloadMuHeld := true
	releaseReloadMu := func() {
		if reloadMuHeld {
			reloadMuHeld = false
			srv.reloadMu.Unlock()
		}
	}
	defer releaseReloadMu()

	// Snapshot the live runtime (brief RLock inside snapshotRuntime).
	rt := srv.snapshotRuntime()
	root := rt.Root

	// Acquire the cross-process flock OUTSIDE srv.mu: the daemon keeps serving
	// reads during the (possibly contended) flock-await.
	lockHandle, lockErr := lock.NewWorkspaceLock(root)
	if lockErr != nil {
		return toolError(lockErr), nil
	}

	acquireCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if acquireErr := lockHandle.Acquire(acquireCtx); acquireErr != nil {
		return toolError(acquireErr), nil
	}

	flockHeld := true
	releaseFlock := func() {
		if flockHeld {
			flockHeld = false
			_ = lockHandle.Release()
		}
	}
	defer releaseFlock()

	// Load + validate + build the fresh Runtime all off the write-lock (readers
	// never block on TOML parsing). buildReloaded is private and takes no context;
	// it returns an error on a blocking failure (parse/structural error or
	// behavior-engine build failure), leaving rt unmutated.
	fresh, diff, buildErr := rt.buildReloaded()
	if buildErr != nil {
		return toolJSON(map[string]any{
			"manifest_epoch":    srv.seenManifestEpoch.Load(),
			"diff":              emptyManifestDiff(),
			"reindex":           emptyReindexReport(),
			"validation_errors": []string{buildErr.Error()},
			"warnings":          []string{},
		})
	}

	// Collect warnings (non-blocking validation errors recorded on the fresh manifest).
	warnings := []string{}
	for _, aliasErr := range fresh.Manifest.AliasErrors {
		warnings = append(warnings, fmt.Sprintf("invalid alias %q: %s", aliasErr.Name, aliasErr.Message))
	}
	for _, contextErr := range fresh.Manifest.ContextErrors {
		warnings = append(warnings, fmt.Sprintf("invalid context entry: %s", contextErr.Message))
	}

	// Swap under the write-lock.
	srv.mu.Lock()
	old := srv.runtime
	srv.runtime = fresh

	// Bump manifest-epoch and record it under the write-lock.
	newEpoch, bumpErr := epoch.Manifest.Bump(root)
	if bumpErr != nil {
		// Revert the swap on bump failure.
		srv.runtime = old
		srv.mu.Unlock()

		return toolJSON(map[string]any{
			"manifest_epoch":    srv.seenManifestEpoch.Load(),
			"diff":              emptyManifestDiff(),
			"reindex":           emptyReindexReport(),
			"validation_errors": []string{bumpErr.Error()},
			"warnings":          []string{},
		})
	}

	srv.seenManifestEpoch.Store(newEpoch)
	srv.mu.Unlock()

	// Release the flock and reloadMu BEFORE the reindex pass (spec §6a step 9→10).
	// The swap + epoch bump are done and have propagated, so a concurrent
	// reset/reload on another process must not wait on the flock through this
	// reindex's (synchronous) workspace walk. After this, the reindex is
	// serialized only by reindexMu (against the file watcher and a sibling-owned
	// reindex), while the next reload's swap+bump can proceed under the flock.
	releaseFlock()
	releaseReloadMu()

	// Reindex coupling: off the flock and reloadMu, under reindexMu (skip if no_reindex).
	reindexReport := emptyReindexReport()
	reindexReport["kicked"] = false

	if !noReindex {
		srv.reindexMu.Lock()
		defer srv.reindexMu.Unlock()

		reindexConfig := reindex.Config{
			Root:            fresh.Root,
			Repo:            fresh.Nodes,
			Edges:           fresh.Edges,
			EdgeTypes:       fresh.Manifest.EdgeTypes,
			WorkspaceIgnore: fresh.Manifest.Workspace.Ignore,
			EmbedQueue:      fresh.EmbedQueue,
			Meta:            fresh.Meta,
			FileStates:      fresh.FileState,
			Behaviors:       fresh.BehaviorEngine,
			DriftLog:        fresh.WorkflowDrift,
			NodeTypes:       fresh.Manifest.NodeTypes,
			PropertyDrift:   fresh.PropertyDrift,
			Workers:         fresh.Workers,
			Logger:          fresh.Logger,
			Async:           true,
		}

		if !noEmbed && fresh.Embedder != nil {
			reindexConfig.EmbeddingRepo = fresh.Embeddings
			reindexConfig.Embedder = fresh.Embedder
			reindexConfig.Chunker = fresh.Chunker
		}

		report, runErr := reindex.Run(reindexConfig)
		if runErr != nil {
			// Non-blocking: the manifest-epoch was already bumped and the in-memory
			// schema swapped, so the reload itself succeeded and siblings still
			// converge their schema. Only the index lags; the next watcher pass or a
			// manual tusk_reindex repairs it. reindex.kicked stays false in the
			// response so the caller can see the pass did not complete.
			if fresh.Logger != nil {
				fresh.Logger.Warn("reindex after reload failed", "err", runErr)
			}
		} else {
			reindexReport = map[string]any{
				"kicked":              true,
				"async":               true,
				"indexed":             report.Indexed,
				"removed":             report.Removed,
				"skipped":             report.Skipped,
				"workflow_violations": report.WorkflowViolations,
				"property_violations": report.PropertyViolations,
				"ref_dangling":        report.RefDangling,
				"ref_ambiguous":       report.RefAmbiguous,
				"ref_type_mismatch":   report.RefTypeMismatch,
				"ref_cycle":           report.RefCycle,
			}
		}
	}

	return toolJSON(map[string]any{
		"manifest_epoch": newEpoch,
		"diff": map[string]any{
			"node_types": map[string]any{
				"added":   diff.NodeTypes.Added,
				"removed": diff.NodeTypes.Removed,
			},
			"edge_types": map[string]any{
				"added":   diff.EdgeTypes.Added,
				"removed": diff.EdgeTypes.Removed,
			},
			"behaviors": map[string]any{
				"added":   behaviorRefsToJSON(diff.Behaviors.Added),
				"removed": behaviorRefsToJSON(diff.Behaviors.Removed),
			},
		},
		"reindex":           reindexReport,
		"validation_errors": []string{},
		"warnings":          warnings,
	})
}

// emptyManifestDiff returns a zero-valued ManifestDiff as a JSON envelope.
func emptyManifestDiff() map[string]any {
	return map[string]any{
		"node_types": map[string]any{"added": []string{}, "removed": []string{}},
		"edge_types": map[string]any{"added": []string{}, "removed": []string{}},
		"behaviors":  map[string]any{"added": []map[string]string{}, "removed": []map[string]string{}},
	}
}

// emptyReindexReport returns a zero-valued reindex report envelope.
func emptyReindexReport() map[string]any {
	return map[string]any{
		"kicked":              false,
		"async":               true,
		"indexed":             0,
		"removed":             0,
		"skipped":             0,
		"workflow_violations": 0,
		"property_violations": 0,
		"ref_dangling":        0,
		"ref_ambiguous":       0,
		"ref_type_mismatch":   0,
		"ref_cycle":           0,
	}
}

// behaviorRefsToJSON converts a slice of BehaviorRef to JSON-serializable map slices.
func behaviorRefsToJSON(refs []BehaviorRef) []map[string]string {
	result := make([]map[string]string, len(refs))
	for index, ref := range refs {
		result[index] = map[string]string{
			"kind":     ref.Kind,
			"instance": ref.Instance,
		}
	}
	return result
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
		body, marshalErr := json.Marshal(aliasdispatch.ResultPayload(result))

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
// {name, message} maps the doctor tool embeds under "alias_errors". Delegates
// to aliasdispatch.AliasErrorsPayload so the shape stays shared with the
// tusk_run / tusk_context envelopes. Callers keep their own len()>0 guard so
// the empty-omits-the-key behavior stays at the call site.
func aliasErrorsPayload(errs []manifest.AliasError) []map[string]any {
	return aliasdispatch.AliasErrorsPayload(errs)
}
