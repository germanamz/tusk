import { describe, it, expect } from 'vitest'
import { applyFacets, type FacetState } from './facets'
import type { Graph } from './api'

const graph: Graph = {
  generation: 1, epoch: 0,
  nodes: [
    { id: 'a', type: 'note', title: 'A', path: 'a.md', tags: [], degree: 1 },
    { id: 'b', type: 'ticket', title: 'B', path: 'b.md', tags: [], degree: 1 },
    { id: 'c', type: 'note', title: 'C', path: 'c.md', tags: [], degree: 0 },
  ],
  edges: [{ source: 'a', target: 'b', type: 'refs', kind: 'direct' }],
}

describe('applyFacets', () => {
  it('filters out hidden types', () => {
    const state: FacetState = { hiddenTypes: new Set(['ticket']), hiddenKinds: new Set(), hideOrphans: false }
    const out = applyFacets(graph, state)
    expect(out.nodes.map((n) => n.id).sort()).toEqual(['a', 'c'])
    expect(out.edges.length).toBe(0) // edge to b dropped
  })

  it('hides orphans', () => {
    const state: FacetState = { hiddenTypes: new Set(), hiddenKinds: new Set(), hideOrphans: true }
    const out = applyFacets(graph, state)
    expect(out.nodes.map((n) => n.id).sort()).toEqual(['a', 'b'])
  })
})
