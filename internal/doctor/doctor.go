// Package doctor runs read-only health checks against the index.
package doctor

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/germanamz/tusk/internal/index"
)

// Issue kinds.
const (
	IssueDanglingEdge      = "dangling-edge"
	IssueEmbedRetry        = "embed-retry"
	IssueWorkflowViolation = "workflow-violation"

	IssueUndeclaredProperty = "undeclared-property"
	IssueTypeMismatch       = "type-mismatch"
	IssueRequiredMissing    = "required-missing"
	IssueEnumViolation      = "enum-violation"

	IssueRefDangling     = "ref_dangling"
	IssueRefAmbiguous    = "ref_ambiguous"
	IssueRefTypeMismatch = "ref_type_mismatch"
	IssueRefCycle        = "ref_cycle"
)

// Issue is a single problem the doctor surfaced.
type Issue struct {
	Kind    string
	NodeID  string
	Message string
}

// Report is the doctor's verdict.
type Report struct {
	Issues          []Issue
	EmbedQueueDepth int
}

// Config configures Run.
type Config struct {
	Nodes         *index.NodeRepo
	Edges         *index.EdgeRepo
	EmbedQueue    *index.EmbedQueueRepo
	WorkflowDrift *index.WorkflowDriftRepo // optional; nil = no workflow checks
	PropertyDrift *index.PropertyDriftRepo // optional; nil = no property checks
}

// Run executes every check and returns the aggregate Report.
func Run(config Config) (*Report, error) {
	report := &Report{}

	if config.Edges != nil && config.Nodes != nil {
		dangling, danglingErr := findDanglingEdges(config.Nodes, config.Edges)

		if danglingErr != nil {
			return nil, danglingErr
		}

		report.Issues = append(report.Issues, dangling...)
	}

	if config.EmbedQueue != nil {
		depth, depthErr := config.EmbedQueue.Depth()

		if depthErr != nil {
			return nil, depthErr
		}

		report.EmbedQueueDepth = depth
	}

	if config.WorkflowDrift != nil {
		drift, listErr := config.WorkflowDrift.ListAll()

		if listErr != nil {
			return nil, listErr
		}

		for _, row := range drift {
			report.Issues = append(report.Issues, Issue{
				Kind:   IssueWorkflowViolation,
				NodeID: row.NodeID,
				Message: fmt.Sprintf("workflow %q: status %q is not a declared state for property %q",
					row.PackInstance, row.ObservedStatus, row.Property),
			})
		}
	}

	if config.PropertyDrift != nil {
		propDrift, listErr := config.PropertyDrift.ListAll()

		if listErr != nil {
			return nil, listErr
		}

		for _, row := range propDrift {
			report.Issues = append(report.Issues, Issue{
				Kind:    row.Kind,
				NodeID:  row.NodeID,
				Message: renderPropertyDriftMessage(row),
			})
		}
	}

	return report, nil
}

// renderPropertyDriftMessage formats the Issue message for a property drift
// row per spec §7.3.
func renderPropertyDriftMessage(row index.PropertyDriftRow) string {
	switch row.Kind {
	case IssueUndeclaredProperty:
		return fmt.Sprintf("node-types: property %q not declared on type %q", row.Property, row.NodeType)
	case IssueTypeMismatch:
		return fmt.Sprintf("node-types: property %q — %s", row.Property, row.Details)
	case IssueRequiredMissing:
		return fmt.Sprintf("node-types: required property %q missing on type %q", row.Property, row.NodeType)
	case IssueEnumViolation:
		return fmt.Sprintf("node-types: property %q — %s", row.Property, row.Details)
	case IssueRefDangling:
		return formatRefDangling(row)
	case IssueRefAmbiguous:
		return formatRefAmbiguous(row)
	case IssueRefTypeMismatch:
		return formatRefTypeMismatch(row)
	case IssueRefCycle:
		return formatRefCycle(row)
	default:
		return fmt.Sprintf("node-types: %s on property %q", row.Kind, row.Property)
	}
}

func formatRefDangling(row index.PropertyDriftRow) string {
	var details struct {
		Value string `json:"value"`
		To    string `json:"to"`
	}

	_ = json.Unmarshal([]byte(row.Details), &details) // best-effort

	return fmt.Sprintf("node-types: ref property %q value %q did not resolve to any %q", row.Property, details.Value, details.To)
}

func formatRefAmbiguous(row index.PropertyDriftRow) string {
	var details struct {
		Value      string   `json:"value"`
		To         string   `json:"to"`
		Candidates []string `json:"candidates"`
	}

	_ = json.Unmarshal([]byte(row.Details), &details) // best-effort

	return fmt.Sprintf("node-types: ref property %q value %q matches multiple %q candidates: %s",
		row.Property, details.Value, details.To, strings.Join(details.Candidates, ", "))
}

func formatRefTypeMismatch(row index.PropertyDriftRow) string {
	var details struct {
		Value      string `json:"value"`
		To         string `json:"to"`
		ActualType string `json:"actual_type"`
	}

	_ = json.Unmarshal([]byte(row.Details), &details) // best-effort

	return fmt.Sprintf("node-types: ref property %q value %q target type %q does not match required %q",
		row.Property, details.Value, details.ActualType, details.To)
}

func formatRefCycle(row index.PropertyDriftRow) string {
	var details struct {
		Path []string `json:"path"`
	}

	_ = json.Unmarshal([]byte(row.Details), &details) // best-effort

	return fmt.Sprintf("node-types: ref property %q forms a cycle: %s",
		row.Property, strings.Join(details.Path, " → "))
}

// findDanglingEdges scans every edge and flags those whose target_id has no
// node row.
func findDanglingEdges(nodes *index.NodeRepo, edges *index.EdgeRepo) ([]Issue, error) {
	allEdges, listErr := edges.ListAll()

	if listErr != nil {
		return nil, listErr
	}

	var issues []Issue

	// Cache existence checks: NodeRepo.Get returns an error when the row is
	// missing; cache positive lookups in a set, query on first miss.
	resolved := map[string]bool{}

	for _, edge := range allEdges {
		if cached, hit := resolved[edge.TargetID]; hit {
			if cached {
				continue
			}

			issues = append(issues, Issue{
				Kind:    IssueDanglingEdge,
				NodeID:  edge.SourceID,
				Message: fmt.Sprintf("edge %q -> %q (target missing)", edge.Type, edge.TargetID),
			})

			continue
		}

		if _, getErr := nodes.Get(edge.TargetID); getErr != nil {
			resolved[edge.TargetID] = false

			issues = append(issues, Issue{
				Kind:    IssueDanglingEdge,
				NodeID:  edge.SourceID,
				Message: fmt.Sprintf("edge %q -> %q (target missing)", edge.Type, edge.TargetID),
			})

			continue
		}

		resolved[edge.TargetID] = true
	}

	return issues, nil
}
