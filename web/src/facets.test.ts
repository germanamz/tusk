import { describe, it, expect } from 'vitest'
import { applyFacets, type FacetState } from './facets'
import type { Graph } from './api'

const graph: Graph = {
  generation: 1, epoch: 0,
  nodes: [
    { id: 'a', type: 'note', group: 'g1', title: 'A', path: 'a.md', tags: [], degree: 1, in_degree: 0 },
    { id: 'b', type: 'ticket', group: 'g2', title: 'B', path: 'b.md', tags: [], degree: 1, in_degree: 1 },
    { id: 'c', type: 'note', group: 'g1', title: 'C', path: 'c.md', tags: [], degree: 0, in_degree: 0 },
  ],
  edges: [{ source: 'a', target: 'b', type: 'refs', kind: 'direct' }],
  cluster: { by: 'community', huddle: false, hull: false },
}

describe('applyFacets', () => {
  it('filters out hidden types', () => {
    const state: FacetState = {
      hiddenTypes: new Set(['ticket']),
      hiddenKinds: new Set(),
      hideOrphans: false,
      hiddenGroups: new Set(),
    }
    const out = applyFacets(graph, state)
    expect(out.nodes.map((n) => n.id).sort()).toEqual(['a', 'c'])
    expect(out.edges.length).toBe(0) // edge to b dropped
  })

  it('hides orphans', () => {
    const state: FacetState = {
      hiddenTypes: new Set(),
      hiddenKinds: new Set(),
      hideOrphans: true,
      hiddenGroups: new Set(),
    }
    const out = applyFacets(graph, state)
    expect(out.nodes.map((n) => n.id).sort()).toEqual(['a', 'b'])
  })

  it('filters out nodes in hidden groups', () => {
    const state: FacetState = {
      hiddenTypes: new Set(),
      hiddenKinds: new Set(),
      hideOrphans: false,
      hiddenGroups: new Set(['g2']),
    }
    const out = applyFacets(graph, state)
    // node b is in g2 → excluded; edge to b also dropped
    expect(out.nodes.map((n) => n.id).sort()).toEqual(['a', 'c'])
    expect(out.edges.length).toBe(0)
  })

  it('keeps nodes in non-hidden groups', () => {
    const state: FacetState = {
      hiddenTypes: new Set(),
      hiddenKinds: new Set(),
      hideOrphans: false,
      hiddenGroups: new Set(),
    }
    const out = applyFacets(graph, state)
    expect(out.nodes.map((n) => n.id).sort()).toEqual(['a', 'b', 'c'])
  })

  it('hidden group and hidden type combine (intersection)', () => {
    const state: FacetState = {
      hiddenTypes: new Set(['ticket']),
      hiddenKinds: new Set(),
      hideOrphans: false,
      hiddenGroups: new Set(['g1']),
    }
    const out = applyFacets(graph, state)
    // a (note, g1) → hidden by group; b (ticket, g2) → hidden by type; c (note, g1) → hidden by group
    expect(out.nodes).toHaveLength(0)
  })
})
