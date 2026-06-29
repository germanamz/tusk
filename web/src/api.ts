export interface Match {
  id: string
  score: number
}

export interface GraphNode {
  id: string
  type: string
  group: string
  title: string
  path: string
  tags: string[]
  degree: number
  in_degree: number
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
  cluster: { by: string; property?: string; huddle: boolean; hull: boolean }
}

// SubunitGraph mirrors the Go graphview.SubunitGraph struct: the drill-down
// payload returned by GET /api/subunits/{id...}.
export interface SubunitGraph {
  nodes: GraphNode[]
  edges: GraphEdge[]
}

export async function fetchGraph(signal?: AbortSignal): Promise<Graph> {
  const resp = await fetch('./api/graph', { signal })
  if (!resp.ok) throw new Error(`GET /api/graph failed: ${resp.status}`)
  return (await resp.json()) as Graph
}

// One mean-pooled, L2-normalized vector per embedded file node. `vectors` is
// `{}` when no embeddings exist. `signature` is unused here (Phase 3 keys a
// cache on it).
export interface EmbeddingsResponse {
  model: string
  dim: number
  signature: string
  vectors: Record<string, number[]>
}

export async function fetchEmbeddings(signal?: AbortSignal): Promise<EmbeddingsResponse> {
  const resp = await fetch('./api/embeddings', { signal })
  if (!resp.ok) throw new Error(`GET /api/embeddings failed: ${resp.status}`)
  return (await resp.json()) as EmbeddingsResponse
}
