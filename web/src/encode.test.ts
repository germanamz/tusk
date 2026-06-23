import { describe, it, expect } from 'vitest'
import { colorForType, sizeForDegree, EDGE_KIND_COLORS, dimColor } from './encode'

describe('encode', () => {
  it('assigns a stable color per type', () => {
    expect(colorForType('note')).toBe(colorForType('note'))
    expect(colorForType('note')).not.toBe(colorForType('ticket'))
  })

  it('scales size monotonically with degree', () => {
    expect(sizeForDegree(10)).toBeGreaterThan(sizeForDegree(0))
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
