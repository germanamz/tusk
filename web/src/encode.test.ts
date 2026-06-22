import { describe, it, expect } from 'vitest'
import { colorForType, sizeForDegree, EDGE_KIND_COLORS } from './encode'

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
})
