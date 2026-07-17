import { describe, it, expect } from 'vitest'
import { mergeSubunits, reapplyExpanded } from './subunits'
import type { Graph, GraphNode, SubunitGraph } from './api'

// Small builders mirroring facets.test.ts's fixture style.
function node(id: string): GraphNode {
  return { id, type: 'note', group: 'g', title: id, path: `${id}.md`, tags: [], degree: 0, in_degree: 0 }
}

function graph(ids: string[]): Graph {
  return {
    generation: 1,
    epoch: 0,
    nodes: ids.map(node),
    edges: [],
    cluster: { by: 'type', huddle: false, hull: false },
  }
}

// A sub-unit payload for parent `id`: one child node `id#sub` plus a contains edge.
function subOf(id: string): SubunitGraph {
  return {
    nodes: [node(`${id}#sub`)],
    edges: [{ source: id, target: `${id}#sub`, type: 'contains', kind: 'structural' }],
  }
}

describe('mergeSubunits', () => {
  it('appends only nodes/edges not already present', () => {
    const base = graph(['a'])
    const merged = mergeSubunits(base, subOf('a'))
    expect(merged.nodes.map((n) => n.id).sort()).toEqual(['a', 'a#sub'])
    expect(merged.edges).toHaveLength(1)
    // Re-merging the same payload is a no-op (deduped by id and edge key).
    const again = mergeSubunits(merged, subOf('a'))
    expect(again.nodes).toHaveLength(2)
    expect(again.edges).toHaveLength(1)
  })
})

describe('reapplyExpanded', () => {
  it('empty map fast-path returns the base unchanged', () => {
    const base = graph(['a', 'b'])
    expect(reapplyExpanded(base, new Map())).toBe(base)
  })

  it('re-merges a cached sub-unit into a fresh snapshot', () => {
    const expanded = new Map<string, SubunitGraph>([['a', subOf('a')]])
    // A brand-new snapshot that never carries the sub-unit.
    const snapshot = graph(['a', 'b'])
    const out = reapplyExpanded(snapshot, expanded)
    expect(out.nodes.map((n) => n.id).sort()).toEqual(['a', 'a#sub', 'b'])
    expect(out.edges).toHaveLength(1)
    // The parent is still present, so the cache entry survives.
    expect(expanded.has('a')).toBe(true)
  })

  it('prunes an expansion whose parent left the graph (node absent AND entry deleted)', () => {
    const expanded = new Map<string, SubunitGraph>([['a', subOf('a')]])
    // The snapshot no longer contains parent "a".
    const snapshot = graph(['b', 'c'])
    const out = reapplyExpanded(snapshot, expanded)
    expect(out.nodes.map((n) => n.id).sort()).toEqual(['b', 'c'])
    expect(out.nodes.some((n) => n.id === 'a#sub')).toBe(false)
    expect(expanded.has('a')).toBe(false)
  })

  it('is idempotent — calling twice yields identical counts', () => {
    const expanded = new Map<string, SubunitGraph>([
      ['a', subOf('a')],
      ['b', subOf('b')],
    ])
    const snapshot = graph(['a', 'b'])
    const once = reapplyExpanded(snapshot, expanded)
    const twice = reapplyExpanded(snapshot, expanded)
    expect(twice.nodes).toHaveLength(once.nodes.length)
    expect(twice.edges).toHaveLength(once.edges.length)
    expect(once.nodes).toHaveLength(4) // a, b, a#sub, b#sub
    expect(once.edges).toHaveLength(2)
  })
})
