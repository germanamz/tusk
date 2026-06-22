export interface GraphNode {
  id: string
  type: string
  title: string
  path: string
  tags: string[]
  degree: number
}

export interface GraphEdge {
  source: string
  target: string
  type: string
  kind: string
}

export interface Graph {
  generation: number
  epoch: number
  nodes: GraphNode[]
  edges: GraphEdge[]
}

export async function fetchGraph(signal?: AbortSignal): Promise<Graph> {
  const resp = await fetch('./api/graph', { signal })
  if (!resp.ok) throw new Error(`GET /api/graph failed: ${resp.status}`)
  return (await resp.json()) as Graph
}
