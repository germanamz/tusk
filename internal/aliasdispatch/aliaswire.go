package aliasdispatch

import (
	"github.com/germanamz/tusk/internal/doctor"
	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/query"
	"github.com/germanamz/tusk/internal/status"
)

// ResultPayload converts a typed DispatchResult into the JSON-friendly shape
// the {alias, command, kind, result} envelope embeds under "result". It is the
// single shaper shared by the CLI (tusk run / tusk context) and the MCP server
// (tusk_run / tusk_context) so both surfaces emit identical JSON.
func ResultPayload(result *DispatchResult) any {
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

	case *NodeGetResult:
		getResult := typed.Result
		envelope := map[string]any{
			"id":    getResult.Node.ID,
			"type":  getResult.Node.Type,
			"path":  getResult.Node.Path,
			"title": getResult.Node.Title,
		}

		if getResult.IncludeProperties {
			envelope["properties"] = getResult.Node.Properties
		}

		// Edges are hydrated from the index by runNodeGet (both directions,
		// with titles); node.GetResult.Node.Edges is always nil on a read.
		if getResult.IncludeEdges {
			envelope["edges"] = typed.Edges
		}

		if getResult.IncludeBody {
			envelope["body"] = string(getResult.Node.Body)
		}

		return envelope

	case *index.EdgeListResult:
		return map[string]any{
			"rows":  typed.Rows,
			"count": len(typed.Rows),
		}

	case *doctor.Result:
		envelope := DoctorIssuesPayload(typed.Report)
		envelope["embed_queue_depth"] = typed.Report.EmbedQueueDepth
		envelope["reindex_queue_depth"] = typed.Report.ReindexQueueDepth

		if typed.Migration != nil {
			envelope["migrated"] = typed.Migration.Migrated
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

// DoctorIssuesPayload returns the issue part of every doctor JSON surface
// (tusk_doctor and doctor aliases): the issues, ordered errors first with
// their severities, and the per-severity counts. issues is never null.
// Callers add their surface-specific keys to the returned map.
func DoctorIssuesPayload(report *doctor.Report) map[string]any {
	issues := report.Issues

	if issues == nil {
		issues = []doctor.Issue{}
	}

	counts := report.Counts()

	return map[string]any{
		"issues":        issues,
		"error_count":   counts.Errors,
		"warning_count": counts.Warnings,
		"advice_count":  counts.Advice,
	}
}
