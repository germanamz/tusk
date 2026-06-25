import { describe, it, expect } from 'vitest'
import {
  buildTypeColors,
  importanceColor,
  hexToHsl,
  sizeForDegree,
  EDGE_KIND_COLORS,
  dimColor,
  BASE_PALETTE,
} from './encode'

const HEX = /^#[0-9a-f]{6}$/

describe('encode', () => {
  describe('buildTypeColors', () => {
    it('assigns a distinct color to each distinct type', () => {
      const colors = buildTypeColors(['note', 'ticket', 'spec'])
      const values = [...colors.values()]
      expect(new Set(values).size).toBe(values.length)
      expect(colors.get('note')).not.toBe(colors.get('ticket'))
    })

    it('is stable across calls (deterministic, order-independent)', () => {
      const a = buildTypeColors(['note', 'ticket', 'spec'])
      const b = buildTypeColors(['spec', 'note', 'ticket'])
      expect(a.get('note')).toBe(b.get('note'))
      expect(a.get('ticket')).toBe(b.get('ticket'))
      expect(a.get('spec')).toBe(b.get('spec'))
    })

    it('stays collision-free past the base palette (golden-angle hues)', () => {
      const types = Array.from({ length: BASE_PALETTE.length + 12 }, (_, i) => `type-${i}`)
      const colors = buildTypeColors(types)
      expect(colors.size).toBe(types.length)
      const values = [...colors.values()]
      expect(new Set(values).size).toBe(values.length)
      for (const v of values) expect(v).toMatch(HEX)
    })
  })

  describe('importanceColor', () => {
    it('returns a valid #rrggbb color', () => {
      expect(importanceColor('#4f8cff', 3, 10)).toMatch(HEX)
    })

    it('raises lightness monotonically with degree while preserving hue', () => {
      const dim = importanceColor('#4f8cff', 0, 10)
      const mid = importanceColor('#4f8cff', 4, 10)
      const hub = importanceColor('#4f8cff', 10, 10)
      const lum = (hex: string) => {
        const n = parseInt(hex.slice(1), 16)
        const r = (n >> 16) & 0xff
        const g = (n >> 8) & 0xff
        const b = n & 0xff
        return (Math.max(r, g, b) + Math.min(r, g, b)) / 2
      }
      expect(lum(mid)).toBeGreaterThan(lum(dim))
      expect(lum(hub)).toBeGreaterThan(lum(mid))
    })

    it('preserves the base hue across the degree range (type stays readable)', () => {
      const base = '#4f8cff'
      const baseHue = hexToHsl(base)[0]
      const lo = hexToHsl(importanceColor(base, 0, 10))[0]
      const hi = hexToHsl(importanceColor(base, 10, 10))[0]
      // Hue is held fixed; only S/L track degree. Allow a tiny epsilon for
      // the round-trip through 8-bit hex quantization.
      expect(Math.abs(lo - baseHue)).toBeLessThan(1)
      expect(Math.abs(hi - baseHue)).toBeLessThan(1)
      expect(Math.abs(hi - lo)).toBeLessThan(1)
    })

    it('caps short of white so deselect never restores #ffffff', () => {
      // The web e2e relies on a deselected node's base color not being white;
      // L_MAX enforces that even for the brightest hub of a near-white base.
      expect(importanceColor('#4f8cff', 10, 10)).not.toBe('#ffffff')
      expect(importanceColor('#ffffff', 10, 10)).not.toBe('#ffffff')
    })

    it('is safe when maxDegree is 0 (no NaN, valid hex)', () => {
      const c = importanceColor('#4f8cff', 0, 0)
      expect(c).toMatch(HEX)
      expect(c).not.toContain('NaN')
    })

    it('amplifies brightness: the top hub reaches the widened lightness ceiling', () => {
      // Widened L_MAX = 0.78; allow for hex round-trip quantization.
      expect(hexToHsl(importanceColor('#4f8cff', 10, 10))[2]).toBeGreaterThan(0.7)
    })
  })

  it('scales size monotonically with degree', () => {
    expect(sizeForDegree(10, 10)).toBeGreaterThan(sizeForDegree(0, 10))
  })

  it('amplifies the hub/leaf gap: top hub radius is ≥2.5× a leaf', () => {
    // 3d-force-graph renders sphere radius ∝ ∛val, so compare cube roots.
    const hub = Math.cbrt(sizeForDegree(10, 10))
    const leaf = Math.cbrt(sizeForDegree(0, 10))
    expect(hub / leaf).toBeGreaterThanOrEqual(2.5)
  })

  it('size is safe when maxDegree is 0 (pins the flat minimum, no NaN)', () => {
    // An all-orphan view (maxDegree 0) must collapse to the floor val, not the
    // ceiling — assert the value, not just that the two endpoints agree.
    expect(sizeForDegree(0, 0)).toBe(3)
    expect(sizeForDegree(5, 0)).toBe(3)
  })

  it('normalizes against maxDegree: a fixed degree shrinks/dims as the view max grows', () => {
    // The defining behavior of this change: importance is sqrt(degree/maxDegree),
    // so a fixed-degree node reads larger & brighter when the rendered set's max
    // degree is smaller. Guards against silently dropping the maxDegree wiring.
    const lum = (hex: string) => {
      const n = parseInt(hex.slice(1), 16)
      const ch = [(n >> 16) & 0xff, (n >> 8) & 0xff, n & 0xff]
      return (Math.max(...ch) + Math.min(...ch)) / 2
    }
    expect(sizeForDegree(5, 10)).toBeGreaterThan(sizeForDegree(5, 20))
    expect(lum(importanceColor('#4f8cff', 5, 10))).toBeGreaterThan(lum(importanceColor('#4f8cff', 5, 20)))
  })

  it('has a color for each edge kind', () => {
    expect(EDGE_KIND_COLORS.direct).toBeDefined()
    expect(EDGE_KIND_COLORS.derived).toBeDefined()
    expect(EDGE_KIND_COLORS.structural).toBeDefined()
  })

  describe('dimColor', () => {
    it('returns a valid hex color', () => {
      expect(dimColor('#4f8cff')).toMatch(/^#[0-9a-f]{6}$/)
    })

    it('blends toward the dark background (channels move down)', () => {
      const dimmed = dimColor('#ffffff')
      const value = parseInt(dimmed.slice(1), 16)
      expect(value).toBeLessThan(0xffffff)
    })

    it('is identity at amount 0 and the background at amount 1', () => {
      expect(dimColor('#4f8cff', 0)).toBe('#4f8cff')
      expect(dimColor('#4f8cff', 1)).toBe('#0b0e14')
    })

    it('leaves non-hex input untouched', () => {
      expect(dimColor('rebeccapurple')).toBe('rebeccapurple')
    })
  })
})
