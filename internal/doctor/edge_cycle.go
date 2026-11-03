package doctor

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/manifest"
)

// checkEdgeCycles reports every cycle on an edge type declared acyclic, at the
// type's level (#852). `tusk edge add` and node writes refuse a cycle at the
// error level, but reindex indexes a hand-edited file as written, so doctor
// is where one surfaces. One issue per group of nodes that reach each other
// (a strongly connected component), so a tangle of many overlapping cycles is
// one finding rather than one per edge.
//
// Direct and derived rows count, so a ref property's acyclic is checked too.
// Structural rows (a file containing its sub-units) can't loop, and the
// built-in sub-document types are skipped without being read. A sub-unit's
// copy of a body link is skipped because the file's row carries the same
// link, and a link to a heading counts as a link to its file.
//
// No-op when the manifest or the edge repo is absent.
func checkEdgeCycles(config Config) ([]Issue, error) {
	if config.Edges == nil || config.Manifest == nil {
		return nil, nil
	}

	builtin := manifest.EdgeTypes{}

	if config.Manifest.SubUnitsEnabled() {
		builtin = manifest.SubdocumentEdgeTypes()
	}

	var names []string

	for name, edgeType := range config.Manifest.EdgeTypes {
		if _, isBuiltin := builtin[name]; isBuiltin || edgeType.Acyclic == manifest.AcyclicNone {
			continue
		}

		names = append(names, name)
	}

	sort.Strings(names)

	var issues []Issue

	for _, name := range names {
		rows, listErr := config.Edges.ListByType(name)

		if listErr != nil {
			return nil, fmt.Errorf("doctor: list %s edges: %w", name, listErr)
		}

		adjacency := map[string][]string{}

		for _, row := range rows {
			if row.Kind == "structural" || strings.Contains(row.SourceID, index.SubUnitIDSeparator) {
				continue
			}

			target, _, _ := strings.Cut(row.TargetID, index.SubUnitIDSeparator)

			if !slices.Contains(adjacency[row.SourceID], target) {
				adjacency[row.SourceID] = append(adjacency[row.SourceID], target)
			}
		}

		for _, targets := range adjacency {
			sort.Strings(targets)
		}

		severity := string(config.Manifest.EdgeTypes[name].Acyclic)

		for _, group := range cycleGroups(adjacency) {
			cycle := shortestCycle(group[0], adjacency, group)
			onCycle := make(map[string]struct{}, len(cycle))

			for _, id := range cycle {
				onCycle[id] = struct{}{}
			}

			var others []string

			for _, id := range group {
				if _, shown := onCycle[id]; !shown {
					others = append(others, id)
				}
			}

			issues = append(issues, Issue{
				Kind:      IssueEdgeCycle,
				Severity:  severity,
				NodeID:    group[0],
				Message:   fmt.Sprintf("edge %q is acyclic, but these nodes form a cycle: %s", name, strings.Join(cycle, " → ")),
				Locations: others,
			})
		}
	}

	return issues, nil
}

// cycleGroups returns every strongly connected component of the graph that
// holds a cycle: two or more nodes, or one node with an edge to itself. Each
// group is sorted, and the groups are ordered by their first id. Tarjan's
// algorithm, visiting nodes and targets in sorted order so the result is
// stable.
func cycleGroups(adjacency map[string][]string) [][]string {
	sources := make([]string, 0, len(adjacency))

	for source := range adjacency {
		sources = append(sources, source)
	}

	sort.Strings(sources)

	var (
		groups  [][]string
		stack   []string
		counter int
	)

	order := map[string]int{}
	lowest := map[string]int{}
	onStack := map[string]bool{}

	var visit func(id string)

	visit = func(id string) {
		order[id] = counter
		lowest[id] = counter
		counter++

		stack = append(stack, id)
		onStack[id] = true

		for _, target := range adjacency[id] {
			if _, seen := order[target]; !seen {
				visit(target)
				lowest[id] = min(lowest[id], lowest[target])
			} else if onStack[target] {
				lowest[id] = min(lowest[id], order[target])
			}
		}

		if lowest[id] != order[id] {
			return
		}

		var group []string

		for {
			top := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			onStack[top] = false
			group = append(group, top)

			if top == id {
				break
			}
		}

		if len(group) > 1 || slices.Contains(adjacency[id], id) {
			sort.Strings(group)
			groups = append(groups, group)
		}
	}

	for _, source := range sources {
		if _, seen := order[source]; !seen {
			visit(source)
		}
	}

	sort.Slice(groups, func(left, right int) bool {
		return groups[left][0] < groups[right][0]
	})

	return groups
}

// shortestCycle returns the shortest cycle from start back to itself, walking
// only nodes of its group, as the ids along it with start at both ends. A
// breadth-first walk over sorted targets, so ties go to the smaller ids.
func shortestCycle(start string, adjacency map[string][]string, group []string) []string {
	inGroup := make(map[string]struct{}, len(group))

	for _, id := range group {
		inGroup[id] = struct{}{}
	}

	previous := map[string]string{}
	queue := []string{start}

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		for _, target := range adjacency[current] {
			if target == start {
				cycle := []string{start}

				for step := current; step != start; step = previous[step] {
					cycle = append(cycle, step)
				}

				slices.Reverse(cycle)

				return append([]string{start}, cycle...)
			}

			if _, member := inGroup[target]; !member {
				continue
			}

			if _, seen := previous[target]; seen {
				continue
			}

			previous[target] = current
			queue = append(queue, target)
		}
	}

	return []string{start}
}
