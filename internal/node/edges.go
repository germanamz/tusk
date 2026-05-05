package node

import (
	"errors"
	"fmt"

	"github.com/germanamz/tusk/internal/manifest"
)

// ErrEdgeValueShape is returned by ResolveEdges when a frontmatter key matching
// an edge type carries a value that is neither a scalar string nor a sequence
// of strings.
var ErrEdgeValueShape = errors.New("node: edge value must be a string or sequence of strings")

// ResolveEdges walks node.Properties and moves entries whose key matches a
// declared edge type into node.Edges, leaving non-edge keys in place.
//
// Edge values may be:
//   - a scalar string (single target id), or
//   - a YAML sequence whose every element is a string (multiple target ids).
//
// Any other shape returns ErrEdgeValueShape wrapped with the offending key.
func ResolveEdges(parsedNode *Node, edgeTypes manifest.EdgeTypes) error {
	if parsedNode.Edges == nil {
		parsedNode.Edges = map[string][]string{}
	}

	for key, value := range parsedNode.Properties {
		if _, isEdge := edgeTypes[key]; !isEdge {
			continue
		}

		targets, convertErr := edgeValueToTargets(key, value)

		if convertErr != nil {
			return convertErr
		}

		parsedNode.Edges[key] = targets
		delete(parsedNode.Properties, key)
	}

	return nil
}

func edgeValueToTargets(key string, value any) ([]string, error) {
	switch typed := value.(type) {
	case string:
		return []string{typed}, nil
	case []any:
		result := make([]string, 0, len(typed))

		for index, element := range typed {
			elementString, isString := element.(string)

			if !isString {
				return nil, fmt.Errorf("%w: key %q index %d not a string (got %T)", ErrEdgeValueShape, key, index, element)
			}

			result = append(result, elementString)
		}

		return result, nil
	}

	return nil, fmt.Errorf("%w: key %q has unsupported value type %T", ErrEdgeValueShape, key, value)
}

// EdgeContext supplies node-type resolution to ValidateEdges. The caller
// implements ResolveTargetType against whatever store represents nodes
// (in-memory map for tests, the index in production).
//
// ResolveTargetType returns (typeName, true) when the target node's type can be
// determined, and ("", false) for unresolved targets (file does not exist).
// Unresolved targets are allowed — they surface as warnings via doctor (Plan 8)
// rather than rejections at write time.
type EdgeContext struct {
	ResolveTargetType func(targetID string) (string, bool)
}

// ValidateEdges checks that every edge in node.Edges is declared in edgeTypes
// and that source/target node types match the edge type's `from`/`to` lists.
// Returns the first violation encountered, or nil if all edges are legal.
func ValidateEdges(parsedNode *Node, edgeTypes manifest.EdgeTypes, ctx EdgeContext) error {
	for edgeName, targets := range parsedNode.Edges {
		edgeType, declared := edgeTypes[edgeName]

		if !declared {
			return fmt.Errorf("node: edge %q not declared in manifest", edgeName)
		}

		if !edgeType.AllowsSource(parsedNode.Type) {
			return fmt.Errorf("node: edge %q does not allow source type %q (allowed: %v)", edgeName, parsedNode.Type, edgeType.From)
		}

		for _, targetID := range targets {
			targetType, resolved := ctx.ResolveTargetType(targetID)

			if !resolved {
				continue
			}

			if !edgeType.AllowsTarget(targetType) {
				return fmt.Errorf("node: edge %q from %q to %q: target type %q not in allowed %v", edgeName, parsedNode.ID, targetID, targetType, edgeType.To)
			}
		}
	}

	return nil
}

// CycleProbe describes a candidate edge being checked against an existing graph.
type CycleProbe struct {
	EdgeType string
	Source   string
	Target   string
}

// ErrCycleDetected is returned by DetectCycle when adding the candidate edge
// would form a cycle in the typed sub-graph.
var ErrCycleDetected = errors.New("node: edge would create a cycle")

// DetectCycle checks whether adding candidate.Source → candidate.Target to the
// existing adjacency map (already filtered to the same edge type) would create
// a cycle. Returns ErrCycleDetected (wrapped with the offending path) when
// reachable, nil otherwise.
//
// Self-loops (Source == Target) are always reported as cycles.
func DetectCycle(candidate CycleProbe, existing map[string][]string) error {
	if candidate.Source == candidate.Target {
		return fmt.Errorf("%w: self-loop on %q", ErrCycleDetected, candidate.Source)
	}

	visited := map[string]struct{}{}
	stack := []string{candidate.Target}

	for len(stack) > 0 {
		current := stack[len(stack)-1]
		stack = stack[:len(stack)-1]

		if current == candidate.Source {
			return fmt.Errorf("%w: %s → … → %s → %s", ErrCycleDetected, candidate.Source, candidate.Target, candidate.Source)
		}

		if _, alreadyVisited := visited[current]; alreadyVisited {
			continue
		}

		visited[current] = struct{}{}

		stack = append(stack, existing[current]...)
	}

	return nil
}
