package doctor

import (
	"fmt"
	"sort"

	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/typeref"
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

	// UnknownEdgeTypes lists well-formed entries in EdgeTypes whose type is
	// not declared in the active manifest (after type-pack expansion). The
	// walker's SQL matches nothing for these, so they are silently skipped.
	// Populated even when Enabled=false so users notice drift before
	// toggling the feature on.
	UnknownEdgeTypes []string

	// InvalidEdgeTypes lists entries that are not valid type references
	// under the typeref grammar (e.g. "Refs"). The query path parses the
	// same list with typeref.ParseMany, so any malformed entry makes every
	// `--semantic` query hard-fail — a strictly worse state than an unknown
	// edge, which is why it is tracked separately. Populated even when
	// Enabled=false so users see the latent breakage.
	InvalidEdgeTypes []string

	// WeightZeroNoOp is true when Enabled=true && Weight == 0 — the
	// feature is on but contributes nothing, almost certainly a config bug.
	WeightZeroNoOp bool

	// EmptyEdgeTypesNoOp is true when Enabled=true && len(EdgeTypes) == 0 —
	// the walker adds no neighbors, so the feature is a no-op. The sibling
	// of WeightZeroNoOp. An explicit empty list is distinguishable from an
	// absent block, which resolves to the non-empty default set.
	EmptyEdgeTypesNoOp bool
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

	// Classify each configured entry with the SAME typeref grammar the query
	// path enforces (semantic_rank.go → typeref.ParseMany), so doctor agrees
	// with what a real `--semantic` query actually does:
	//
	//   - a malformed entry (e.g. "Refs") makes typeref.ParseMany fail, so
	//     every semantic query hard-errors. That is NOT a silently-skipped
	//     unknown edge — it is a hard misconfiguration (InvalidEdgeTypes).
	//   - a well-formed reference (bare "references", scoped ":references" or
	//     "markdown:contains") whose TYPE is declared resolves and walks; it
	//     must not be flagged. Scope only narrows the source column the walker
	//     matches; declaration is decided by the bare type name.
	//   - a well-formed reference whose type is undeclared is a true unknown:
	//     the walker's SQL matches nothing, so it is silently skipped.
	invalidMessages := map[string]string{}

	for _, name := range resolved.EdgeTypes {
		ref, parseErr := typeref.Parse(name)

		if parseErr != nil {
			pane.InvalidEdgeTypes = append(pane.InvalidEdgeTypes, name)
			invalidMessages[name] = parseErr.Error()

			continue
		}

		if _, ok := known[ref.Type]; !ok {
			pane.UnknownEdgeTypes = append(pane.UnknownEdgeTypes, name)
		}
	}

	sort.Strings(pane.UnknownEdgeTypes)
	sort.Strings(pane.InvalidEdgeTypes)

	if resolved.Enabled && resolved.Weight == 0 {
		pane.WeightZeroNoOp = true
	}

	if resolved.Enabled && len(resolved.EdgeTypes) == 0 {
		pane.EmptyEdgeTypesNoOp = true
	}

	if !resolved.Enabled {
		return pane, nil
	}

	var issues []Issue

	// Invalid entries first: they break every semantic query, so they are the
	// most urgent signal in the block.
	for _, name := range pane.InvalidEdgeTypes {
		issues = append(issues, Issue{
			Kind:    IssueGraphExpansionInvalidEdge,
			NodeID:  name,
			Message: fmt.Sprintf("graph-expansion: edge type %q is not a valid type reference (%s); every `--semantic` query fails to parse graph-expansion edge-types until this is fixed", name, invalidMessages[name]),
		})
	}

	for _, name := range pane.UnknownEdgeTypes {
		issues = append(issues, Issue{
			Kind:    IssueGraphExpansionUnknownEdge,
			NodeID:  name,
			Message: fmt.Sprintf("graph-expansion: edge type %q is not declared in the active manifest; the walker will silently skip it", name),
		})
	}

	if pane.EmptyEdgeTypesNoOp {
		issues = append(issues, Issue{
			Kind:    IssueGraphExpansionNoEdges,
			Message: "graph-expansion: enabled but edge-types is empty — the walker adds no neighbors, so the feature is a no-op; list edge types or disable the block",
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
