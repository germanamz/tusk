import { describe, it, expect } from 'vitest'
import {
  buildTypeColors,
  buildGroupColors,
  importanceColor,
  hexToHsl,
  sizeForDegree,
  EDGE_KIND_COLORS,
  dimColor,
  BASE_PALETTE,
  rgba,
  edgeAlpha,
  HUB_FLOOR,
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

  // Golden lock: pins the EXACT hex assignment so the assignPalette refactor
  // stays byte-for-byte identical. Inputs cover dedup (repeated values),
  // out-of-order sort, the curated base-palette slots, golden-angle overflow
  // (>BASE_PALETTE.length distinct values), and the empty-string group sentinel.
  describe('palette golden lock', () => {
    it('buildTypeColors is byte-stable across dedup, sort, base slots, and golden-angle overflow', () => {
      // 14 distinct values, shuffled, with three duplicates (alpha, zeta, mu).
      const types = [
        'zeta', 'alpha', 'mu', 'alpha', 'beta', 'gamma', 'delta', 'epsilon',
        'zeta', 'eta', 'theta', 'iota', 'kappa', 'lambda', 'mu', 'nu', 'xi',
      ]
      expect(Object.fromEntries(buildTypeColors(types))).toMatchInlineSnapshot(`
        {
          "alpha": "#4f8cff",
          "beta": "#ff6b6b",
          "delta": "#51cf66",
          "epsilon": "#ffd43b",
          "eta": "#cc5de8",
          "gamma": "#22b8cf",
          "iota": "#ff922b",
          "kappa": "#94d82d",
          "lambda": "#f06595",
          "mu": "#20c997",
          "nu": "#a78bfa",
          "theta": "#fab005",
          "xi": "#5a99d8",
          "zeta": "#d85a74",
        }
      `)
    })

    it('buildGroupColors drops the empty-string sentinel and otherwise matches the type palette', () => {
      const groups = ['', 'zeta', 'alpha', '', 'beta', 'gamma']
      const out = buildGroupColors(groups)
      expect(out.has('')).toBe(false)
      expect(Object.fromEntries(out)).toMatchInlineSnapshot(`
        {
          "alpha": "#4f8cff",
          "beta": "#ff6b6b",
          "gamma": "#51cf66",
          "zeta": "#ffd43b",
        }
      `)
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

  describe('rgba', () => {
    it('converts a #rrggbb hex to an rgba() string', () => {
      expect(rgba('#7aa2f7', 0.1)).toBe('rgba(122,162,247,0.1)')
    })

    it('clamps alpha to [0,1]', () => {
      expect(rgba('#7aa2f7', -0.5)).toBe('rgba(122,162,247,0)')
      expect(rgba('#7aa2f7', 1.5)).toBe('rgba(122,162,247,1)')
    })

    it('returns the input unchanged for non-#rrggbb strings', () => {
      expect(rgba('#888', 0.5)).toBe('#888')
      expect(rgba('rebeccapurple', 0.5)).toBe('rebeccapurple')
    })
  })

  describe('edgeAlpha', () => {
    const opts = { intraAlpha: 0.85, interAlpha: 0.10, hubStrength: 0.75 }

    it('intra-cluster alpha is greater than inter-cluster alpha', () => {
      expect(edgeAlpha(true, 1, 1, 10, opts)).toBeGreaterThan(edgeAlpha(false, 1, 1, 10, opts))
    })

    it('hub damp lowers alpha as endpoint degree rises', () => {
      const low = edgeAlpha(true, 1, 1, 100, opts)
      const high = edgeAlpha(true, 99, 99, 100, opts)
      expect(high).toBeLessThan(low)
    })

    it('hubStrength=0 produces no hub damp (hubDamp=1)', () => {
      const noHub = { ...opts, hubStrength: 0 }
      const withHub = { ...opts, hubStrength: 0.75 }
      // At high degree, hubStrength=0 should give higher alpha than hubStrength>0
      expect(edgeAlpha(true, 90, 90, 100, noHub)).toBeGreaterThan(edgeAlpha(true, 90, 90, 100, withHub))
    })

    it('maxDegree=0 is safe (no NaN, returns base alpha floor)', () => {
      const a = edgeAlpha(true, 0, 0, 0, opts)
      expect(Number.isNaN(a)).toBe(false)
      expect(a).toBeGreaterThanOrEqual(0)
      expect(a).toBeLessThanOrEqual(1)
    })

    it('hub floor is respected — hubDamp clamps to HUB_FLOOR even at full strength', () => {
      // hubStrength=1.0 at max degree makes the raw hubDamp 1 - 1*1 = 0, which must
      // clamp UP to HUB_FLOOR. So alpha lands exactly at intraAlpha * HUB_FLOOR.
      const a = edgeAlpha(true, 100, 100, 100, { ...opts, hubStrength: 1.0 })
      expect(a).toBeCloseTo(opts.intraAlpha * HUB_FLOOR, 6)
    })

    it('output is always in [0,1]', () => {
      const cases = [
        [true, 0, 0, 0],
        [true, 10, 10, 10],
        [false, 10, 10, 10],
        [false, 100, 100, 100],
        [true, 50, 30, 100],
      ] as const
      for (const [sg, src, tgt, max] of cases) {
        const a = edgeAlpha(sg, src, tgt, max, opts)
        expect(a).toBeGreaterThanOrEqual(0)
        expect(a).toBeLessThanOrEqual(1)
      }
    })
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
