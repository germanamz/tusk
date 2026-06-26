package graphview

// ancestorGroups maps each node id that participates in the hierarchy to the
// id of its depth-N ancestor along edges of type edgeType.
//
// parentIsSource selects which endpoint is the parent: false (default) means
// the edge TARGET is the parent (child→parent edges, e.g. the built-in
// "parent" edge where Source=child, Target=parent); true means the SOURCE is
// the parent (parent→child edges, e.g. "contains"/"children" style).
//
// depth > 0 returns the depth-th ancestor, or the topmost reachable ancestor
// when the chain is shorter than depth steps. depth <= 0 walks to the root
// (the topmost reachable node).
//
// A node with no parent edge maps to its OWN id, so every branch root and
// off-hierarchy node is its own distinct group rather than collapsing into a
// neutral bucket. Cycles are broken with a visited set; the last node seen
// before the repeat is used as the group.
func ancestorGroups(edges []GraphEdge, edgeType string, depth int, parentIsSource bool) map[string]string {
	// Build the immediate child→parent map from matched edges.
	// On duplicate child entries keep the first encountered (input order from
	// snapshot()'s kept-edge slice is deterministic), so the result is stable.
	parentOf := make(map[string]string)

	// Collect all node ids that appear as either endpoint of a matched edge.
	participants := make(map[string]struct{})

	for _, edge := range edges {
		if edge.Type != edgeType {
			continue
		}

		var child, parent string

		if parentIsSource {
			// edge SOURCE is the parent; TARGET is the child
			child = edge.Target
			parent = edge.Source
		} else {
			// edge TARGET is the parent (default: child→parent direction)
			child = edge.Source
			parent = edge.Target
		}

		participants[child] = struct{}{}
		participants[parent] = struct{}{}

		// First-wins dedup: only record if child not already mapped.
		if _, exists := parentOf[child]; !exists {
			parentOf[child] = parent
		}
	}

	// For each participant, walk up to the depth-N ancestor (or root).
	result := make(map[string]string, len(participants))

	for nodeID := range participants {
		result[nodeID] = walkToAncestor(nodeID, parentOf, depth)
	}

	return result
}

// walkToAncestor walks the child→parent map starting from start, taking at
// most depth steps (or until the root when depth <= 0). A visited set breaks
// cycles: the last node seen before a repeat is returned.
func walkToAncestor(start string, parentOf map[string]string, depth int) string {
	visited := make(map[string]struct{})
	visited[start] = struct{}{}

	current := start

	for steps := 0; depth <= 0 || steps < depth; steps++ {
		next, hasParent := parentOf[current]

		if !hasParent {
			// current is a root; stop here.
			break
		}

		if _, seen := visited[next]; seen {
			// Cycle detected; keep current as the answer.
			break
		}

		visited[next] = struct{}{}
		current = next
	}

	return current
}
