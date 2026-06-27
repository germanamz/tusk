import { describe, it, expect } from 'vitest'
import { groupRows, matchesSearch, bulkAll, bulkNone, bulkInvert, setRowVisible } from './controls'
import type { Graph } from './api'

// ---------------------------------------------------------------------------
// Minimal graph factory
// ---------------------------------------------------------------------------

function makeGraph(groups: string[]): Graph {
  return {
    generation: 1,
    epoch: 0,
    nodes: groups.map((g, i) => ({
      id: `n${i}`,
      type: 'note',
      group: g,
      title: `Node ${i}`,
      path: `n${i}.md`,
      tags: [],
      degree: 0,
      in_degree: 0,
    })),
    edges: [],
    cluster: { by: 'community', huddle: false, hull: false },
  }
}

// ---------------------------------------------------------------------------
// groupRows
// ---------------------------------------------------------------------------

describe('groupRows', () => {
  it('counts each distinct group', () => {
    const g = makeGraph(['a', 'a', 'b', 'c', 'c', 'c'])
    const rows = groupRows(g)
    const byGroup = Object.fromEntries(rows.map((r) => [r.group, r.count]))
    expect(byGroup).toEqual({ a: 2, b: 1, c: 3 })
  })

  it('sorts by count desc', () => {
    const g = makeGraph(['a', 'b', 'b', 'c', 'c', 'c'])
    const rows = groupRows(g)
    const counts = rows.map((r) => r.count)
    expect(counts).toEqual([3, 2, 1])
  })

  it('breaks ties by label asc', () => {
    const g = makeGraph(['beta', 'alpha', 'beta', 'alpha'])
    const rows = groupRows(g)
    // both count == 2; alpha < beta
    expect(rows[0].label).toBe('alpha')
    expect(rows[1].label).toBe('beta')
  })

  it('maps empty string to "(none)"', () => {
    const g = makeGraph(['', 'x', 'x'])
    const rows = groupRows(g)
    const noneRow = rows.find((r) => r.group === '')
    expect(noneRow).toBeDefined()
    expect(noneRow!.label).toBe('(none)')
  })

  it('returns correct count for "(none)" entries', () => {
    const g = makeGraph(['', '', '', 'x'])
    const rows = groupRows(g)
    const noneRow = rows.find((r) => r.group === '')!
    expect(noneRow.count).toBe(3)
  })

  it('returns empty array for an empty node set', () => {
    const g = makeGraph([])
    expect(groupRows(g)).toEqual([])
  })

  it('includes a label field matching the group (or "(none)")', () => {
    const g = makeGraph(['alpha', ''])
    const rows = groupRows(g)
    const alpha = rows.find((r) => r.group === 'alpha')!
    expect(alpha.label).toBe('alpha')
  })
})

// ---------------------------------------------------------------------------
// matchesSearch
// ---------------------------------------------------------------------------

describe('setRowVisible', () => {
  // Regression guard for the group filter: rows must hide via inline display,
  // NOT the `hidden` attribute (which `.controls-row { display: flex }` overrides,
  // so the filter would silently do nothing).
  it('hides via inline display:none and shows via empty display', () => {
    const row = { style: {} as { display?: string } } as unknown as HTMLElement
    setRowVisible(row, false)
    expect(row.style.display).toBe('none')
    setRowVisible(row, true)
    expect(row.style.display).toBe('')
  })
})

describe('matchesSearch', () => {
  it('returns true for empty query', () => {
    expect(matchesSearch('anything', '')).toBe(true)
  })

  it('matches case-insensitively', () => {
    expect(matchesSearch('FooBar', 'foo')).toBe(true)
    expect(matchesSearch('foobar', 'FOO')).toBe(true)
  })

  it('does substring matching', () => {
    expect(matchesSearch('community/decisions', 'dec')).toBe(true)
    expect(matchesSearch('community/decisions', 'xyz')).toBe(false)
  })

  it('returns false when query is not a substring', () => {
    expect(matchesSearch('alpha', 'beta')).toBe(false)
  })

  it('matches the full label', () => {
    expect(matchesSearch('exact', 'exact')).toBe(true)
  })
})

// ---------------------------------------------------------------------------
// Bulk set operations
// ---------------------------------------------------------------------------

describe('bulkAll', () => {
  it('clears the hidden set', () => {
    const hidden = new Set(['a', 'b', 'c'])
    bulkAll(hidden)
    expect(hidden.size).toBe(0)
  })

  it('is a no-op on an already-empty set', () => {
    const hidden = new Set<string>()
    bulkAll(hidden)
    expect(hidden.size).toBe(0)
  })
})

describe('bulkNone', () => {
  it('adds every universe item to the hidden set', () => {
    const hidden = new Set<string>()
    bulkNone(['a', 'b', 'c'], hidden)
    expect([...hidden].sort()).toEqual(['a', 'b', 'c'])
  })

  it('works when some items are already hidden', () => {
    const hidden = new Set(['a'])
    bulkNone(['a', 'b', 'c'], hidden)
    expect([...hidden].sort()).toEqual(['a', 'b', 'c'])
  })

  it('leaves hidden intact when universe is empty', () => {
    const hidden = new Set(['x'])
    bulkNone([], hidden)
    expect([...hidden]).toEqual(['x'])
  })
})

describe('bulkInvert', () => {
  it('inverts: visible items become hidden and hidden items become visible', () => {
    const hidden = new Set(['b'])
    bulkInvert(['a', 'b', 'c'], hidden)
    // a was visible → hidden; b was hidden → visible; c was visible → hidden
    expect([...hidden].sort()).toEqual(['a', 'c'])
  })

  it('hides all when starting with none hidden', () => {
    const hidden = new Set<string>()
    bulkInvert(['x', 'y'], hidden)
    expect([...hidden].sort()).toEqual(['x', 'y'])
  })

  it('shows all when starting with all hidden', () => {
    const hidden = new Set(['x', 'y'])
    bulkInvert(['x', 'y'], hidden)
    expect(hidden.size).toBe(0)
  })

  it('is a no-op on an empty universe', () => {
    const hidden = new Set(['z'])
    bulkInvert([], hidden)
    expect([...hidden]).toEqual(['z'])
  })
})
