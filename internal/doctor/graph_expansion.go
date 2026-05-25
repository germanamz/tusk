package doctor

import (
	"fmt"

	"github.com/germanamz/tusk/internal/manifest"
)

// GraphExpansionPane summarizes the resolved [query.graph-expansion]
// configuration for tusk doctor. Populated whenever a manifest is available
// so users can see the active values even when the feature is disabled.
//
// UnknownEdgeTypes is computed against the merged manifest's EdgeTypes map
// (i.e., after MergeBuiltinPacks has injected sub-document edge types such
// as `contains`). The pane is always populated when a manifest is present;
// warning Issues (IssueGraphExpansionUnknownEdge, IssueGraphExpansionWeightZero)
// are emitted only when Enabled=true to avoid noise from configured-but-off
// blocks.
type GraphExpansionPane struct {
	// Enabled mirrors manifest.GraphExpansion.Enabled.
	Enabled bool
	// Hops mirrors manifest.GraphExpansion.Hops.
	Hops int
	// Weight mirrors manifest.GraphExpansion.Weight.
	Weight float64
	// CandidateMultiplier mirrors manifest.GraphExpansion.CandidateMultiplier.
	CandidateMultiplier int
	// EdgeTypes mirrors manifest.GraphExpansion.EdgeTypes (the configured list,
	// not the filtered subset).
	EdgeTypes []string

	// UnknownEdgeTypes lists entries in EdgeTypes that don't resolve to a
	// known edge type in the active manifest (after type-pack expansion).
	// Populated even when Enabled=false so users notice drift before
	// toggling the feature on.
	UnknownEdgeTypes []string

	// WeightZeroNoOp is true when Enabled=true && Weight == 0 — the
	// feature is on but contributes nothing, almost certainly a config bug.
	WeightZeroNoOp bool
}

// computeGraphExpansionPane builds the GraphExpansionPane from the resolved
// manifest and appends warning Issues for unknown edge types and the
// weight=0 no-op case. Returns (nil, nil) when no manifest is available;
// callers should guard accordingly.
//
// Issues are emitted ONLY when Enabled=true. The typed pane is populated
// unconditionally so the renderer can surface drift even on configured-but-
// disabled blocks (e.g., users wanting to inspect config before toggling on).
func computeGraphExpansionPane(loaded *manifest.Manifest) (*GraphExpansionPane, []Issue) {
	if loaded == nil {
		return nil, nil
	}

	resolved := loaded.GraphExpansion
	pane := &GraphExpansionPane{
		Enabled:             resolved.Enabled,
		Hops:                resolved.Hops,
		Weight:              resolved.Weight,
		CandidateMultiplier: resolved.CandidateMultiplier,
		EdgeTypes:           append([]string(nil), resolved.EdgeTypes...),
	}

	known := loaded.EdgeTypes

	for _, name := range resolved.EdgeTypes {
		if _, ok := known[name]; !ok {
			pane.UnknownEdgeTypes = append(pane.UnknownEdgeTypes, name)
		}
	}

	if resolved.Enabled && resolved.Weight == 0 {
		pane.WeightZeroNoOp = true
	}

	if !resolved.Enabled {
		return pane, nil
	}

	var issues []Issue

	for _, name := range pane.UnknownEdgeTypes {
		issues = append(issues, Issue{
			Kind:    IssueGraphExpansionUnknownEdge,
			NodeID:  name,
			Message: fmt.Sprintf("graph-expansion: edge type %q is not declared in the active manifest; the walker will silently skip it", name),
		})
	}

	if pane.WeightZeroNoOp {
		issues = append(issues, Issue{
			Kind:    IssueGraphExpansionWeightZero,
			Message: "graph-expansion: enabled but weight is 0 — feature is a no-op; raise weight or disable the block",
		})
	}

	return pane, issues
}
