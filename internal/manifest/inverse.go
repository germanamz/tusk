package manifest

import (
	"errors"
	"fmt"
	"slices"
	"sort"
)

// ErrEdgeTypeNotDeclared reports an edge type that tusk.toml doesn't declare,
// for callers that add context with errors.Is (see node.ErrEdgeTypeNotDeclared).
var ErrEdgeTypeNotDeclared = errors.New("not declared in manifest")

// InverseLabels maps each declared `inverse` label to the edge type that
// declares it. Validate guarantees no label is also an edge type and no two
// edge types share one; on a hand-built manifest that breaks the second rule,
// the first edge type by name wins.
func InverseLabels(edgeTypes EdgeTypes) map[string]string {
	labels := map[string]string{}

	names := make([]string, 0, len(edgeTypes))

	for name := range edgeTypes {
		names = append(names, name)
	}

	sort.Strings(names)

	for _, name := range names {
		label := edgeTypes[name].Inverse

		if _, taken := labels[label]; label != "" && !taken {
			labels[label] = name
		}
	}

	return labels
}

// InverseOf returns the edge type whose `inverse` label is label, and whether
// one is. A declared edge type of that name is not an inverse, so callers
// check the edge types first.
func InverseOf(edgeTypes EdgeTypes, label string) (string, bool) {
	forward, found := InverseLabels(edgeTypes)[label]

	return forward, found
}

// validateInverses rejects an `inverse` label that can't stand for one edge
// type walked backwards: one that names an edge type (its own included), and
// one two edge types both declare. Runs after ref properties are synthesized,
// so their inverses are checked too.
func validateInverses(loaded *Manifest) error {
	claimedBy := map[string][]string{}

	for name, edgeType := range loaded.EdgeTypes {
		if edgeType.Inverse == "" {
			continue
		}

		if _, isEdgeType := loaded.EdgeTypes[edgeType.Inverse]; isEdgeType {
			return fmt.Errorf("manifest: edge-type %q: inverse %q is also a declared edge type; an inverse names its edge type walked backwards, so pick another name", name, edgeType.Inverse)
		}

		claimedBy[edgeType.Inverse] = append(claimedBy[edgeType.Inverse], name)
	}

	labels := make([]string, 0, len(claimedBy))

	for label := range claimedBy {
		labels = append(labels, label)
	}

	sort.Strings(labels)

	for _, label := range labels {
		if owners := claimedBy[label]; len(owners) > 1 {
			slices.Sort(owners)

			return fmt.Errorf("manifest: edge-types %q and %q both declare inverse %q; an inverse names one edge type", owners[0], owners[1], label)
		}
	}

	return nil
}
