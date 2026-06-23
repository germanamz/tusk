export interface Neighbor {
  id: string; type: string; title: string; edge_type: string; kind: string; direction: string
}
export interface NodeDetail {
  id: string; type: string; title: string; path: string
  properties: Record<string, unknown>; rendered: string; neighbors: Neighbor[]
}

export async function fetchNodeDetail(id: string): Promise<NodeDetail> {
  const resp = await fetch(`./api/node/${id}`)
  if (!resp.ok) throw new Error(`node ${id}: ${resp.status}`)
  return (await resp.json()) as NodeDetail
}

export async function fetchSubunits(id: string): Promise<{ nodes: any[]; edges: any[] }> {
  const resp = await fetch(`./api/node/${id}/subunits`)
  if (!resp.ok) throw new Error(`subunits ${id}: ${resp.status}`)
  return await resp.json()
}
