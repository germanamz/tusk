import type { Graph } from './api'

export interface FacetState {
  hiddenTypes: Set<string>
  hiddenKinds: Set<string>
  hideOrphans: boolean
}

export function applyFacets(graph: Graph, state: FacetState): Graph {
  const nodes = graph.nodes.filter((node) => !state.hiddenTypes.has(node.type))
  const visible = new Set(nodes.map((node) => node.id))

  let edges = graph.edges.filter(
    (edge) => visible.has(edge.source) && visible.has(edge.target) && !state.hiddenKinds.has(edge.kind),
  )

  let keptNodes = nodes
  if (state.hideOrphans) {
    const connected = new Set<string>()
    for (const edge of edges) {
      connected.add(edge.source)
      connected.add(edge.target)
    }
    keptNodes = nodes.filter((node) => connected.has(node.id))
    const keptIds = new Set(keptNodes.map((n) => n.id))
    edges = edges.filter((edge) => keptIds.has(edge.source) && keptIds.has(edge.target))
  }

  return { ...graph, nodes: keptNodes, edges }
}
