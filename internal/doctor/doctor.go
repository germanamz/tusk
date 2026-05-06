// Package doctor runs read-only health checks against the index.
package doctor

import (
	"fmt"

	"github.com/germanamz/tusk/internal/index"
)

// Issue kinds.
const (
	IssueDanglingEdge = "dangling-edge"
	IssueEmbedRetry   = "embed-retry"
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
	Nodes      *index.NodeRepo
	Edges      *index.EdgeRepo
	EmbedQueue *index.EmbedQueueRepo
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

	return report, nil
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
