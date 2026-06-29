import type { Graph, SubunitGraph } from './api'

// mergeSubunits folds a drill-down SubunitGraph into a base graph, appending only
// the nodes/edges not already present (deduped by node id and by
// source|target|type). The base's generation/epoch/cluster are preserved.
export function mergeSubunits(base: Graph, subunits: SubunitGraph): Graph {
  const existingIds = new Set(base.nodes.map((n) => n.id))
  const newNodes = subunits.nodes.filter((n) => !existingIds.has(n.id))
  const existingEdgeKeys = new Set(base.edges.map((e) => `${e.source}|${e.target}|${e.type}`))
  const newEdges = subunits.edges.filter(
    (e) => !existingEdgeKeys.has(`${e.source}|${e.target}|${e.type}`),
  )
  return {
    ...base,
    nodes: [...base.nodes, ...newNodes],
    edges: [...base.edges, ...newEdges],
  }
}

// reapplyExpanded re-folds every cached sub-unit expansion back into a fresh base
// snapshot. SSE snapshots never carry sub-units, so without this each push would
// wipe whatever the user had expanded. For every parent id still present in
// `base.nodes` its cached payload is re-merged; any parent that has since left the
// graph is pruned from `expanded` (the map is mutated) so the cache can't grow
// without bound or resurrect deleted nodes. An empty map returns `base` untouched.
export function reapplyExpanded(base: Graph, expanded: Map<string, SubunitGraph>): Graph {
  if (expanded.size === 0) return base
  const present = new Set(base.nodes.map((n) => n.id))
  let merged = base
  for (const parentId of [...expanded.keys()]) {
    if (!present.has(parentId)) {
      expanded.delete(parentId)
      continue
    }
    merged = mergeSubunits(merged, expanded.get(parentId)!)
  }
  return merged
}
