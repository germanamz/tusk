import { describe, it, expect } from 'vitest'
import { buildGroupColors, buildTypeColors, BASE_PALETTE } from './encode'

const HEX = /^#[0-9a-f]{6}$/

describe('buildGroupColors', () => {
  it('assigns a distinct color to each distinct group value', () => {
    const colors = buildGroupColors(['eng', 'design', 'sales'])
    const values = [...colors.values()]
    expect(new Set(values).size).toBe(values.length)
    expect(colors.get('eng')).not.toBe(colors.get('design'))
  })

  it('is stable across calls (deterministic, order-independent)', () => {
    const first = buildGroupColors(['eng', 'design', 'sales'])
    const second = buildGroupColors(['sales', 'eng', 'design'])
    expect(first.get('eng')).toBe(second.get('eng'))
    expect(first.get('design')).toBe(second.get('design'))
    expect(first.get('sales')).toBe(second.get('sales'))
  })

  it('returns valid #rrggbb colors for every group', () => {
    const colors = buildGroupColors(['alpha', 'beta', 'gamma'])
    for (const value of colors.values()) {
      expect(value).toMatch(HEX)
    }
  })

  it('stays collision-free past the base palette (golden-angle hues)', () => {
    const groups = Array.from({ length: BASE_PALETTE.length + 12 }, (_, i) => `group-${i}`)
    const colors = buildGroupColors(groups)
    expect(colors.size).toBe(groups.length)
    const values = [...colors.values()]
    expect(new Set(values).size).toBe(values.length)
    for (const value of values) expect(value).toMatch(HEX)
  })

  it('produces the same colors as buildTypeColors when given type values (pixel-identical default)', () => {
    // When by = "type", group === type, so buildGroupColors(types) should
    // equal buildTypeColors(types) in every slot.
    const types = ['note', 'spec', 'ticket']
    const typeMap = buildTypeColors(types)
    const groupMap = buildGroupColors(types)
    for (const key of types) {
      expect(groupMap.get(key)).toBe(typeMap.get(key))
    }
  })

  it('excludes empty-string group from the color map (ungrouped nodes use neutral grey)', () => {
    // Nodes missing the property field have group = "". They must NOT receive
    // a palette color so that the `?? '#888888'` fallback in nodeColor is
    // reachable, rendering them in neutral grey as the spec requires.
    const colors = buildGroupColors(['', 'eng'])
    expect(colors.has('')).toBe(false)
    expect(colors.has('eng')).toBe(true)
    expect(colors.get('eng')).toMatch(HEX)
  })

  it('handles a single group value without error', () => {
    const colors = buildGroupColors(['solo'])
    expect(colors.size).toBe(1)
    expect(colors.get('solo')).toMatch(HEX)
  })

  it('returns an empty map for an empty input', () => {
    const colors = buildGroupColors([])
    expect(colors.size).toBe(0)
  })
})
