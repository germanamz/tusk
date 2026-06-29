import type { SubunitGraph } from './api'

export interface Neighbor {
  id: string; type: string; title: string; edge_type: string; kind: string; direction: string
}
export interface NodeDetail {
  id: string; type: string; title: string; path: string
  properties: Record<string, unknown>; rendered: string; neighbors: Neighbor[]
}

// encodeId percent-encodes each path segment of a node id while keeping real
// "/" separators intact, so the Go {id...} multi-segment wildcard still captures
// the whole id. Without this a "#" in a sub-unit id (`<fileID>#<address>`) is
// read as a URL fragment and stripped, and spaces malform the path. The Go route
// unescapes each segment via PathValue, so the id round-trips.
function encodeId(id: string): string {
  return id.split('/').map(encodeURIComponent).join('/')
}

export async function fetchNodeDetail(id: string): Promise<NodeDetail> {
  const resp = await fetch(`./api/node/${encodeId(id)}`)
  if (!resp.ok) throw new Error(`node ${id}: ${resp.status}`)
  return (await resp.json()) as NodeDetail
}

export async function fetchSubunits(id: string): Promise<SubunitGraph> {
  const resp = await fetch(`./api/subunits/${encodeId(id)}`)
  if (!resp.ok) throw new Error(`subunits ${id}: ${resp.status}`)
  return (await resp.json()) as SubunitGraph
}
