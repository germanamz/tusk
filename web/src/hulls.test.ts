import { describe, it, expect } from 'vitest'
import { groupMembers, hullEligible } from './hulls'

describe('groupMembers', () => {
  it('returns an empty map for an empty node array', () => {
    expect(groupMembers([]).size).toBe(0)
  })

  it('groups nodes by their group field', () => {
    const nodes = [
      { id: 'a', group: 'alpha' },
      { id: 'b', group: 'beta' },
      { id: 'c', group: 'alpha' },
    ]
    const result = groupMembers(nodes)
    expect(result.size).toBe(2)
    expect(result.get('alpha')).toHaveLength(2)
    expect(result.get('beta')).toHaveLength(1)
  })

  it('excludes nodes with an empty group string', () => {
    const nodes = [
      { id: 'a', group: '' },
      { id: 'b', group: 'alpha' },
      { id: 'c', group: '' },
    ]
    const result = groupMembers(nodes)
    expect(result.size).toBe(1)
    expect(result.has('')).toBe(false)
    expect(result.get('alpha')).toHaveLength(1)
  })

  it('excludes nodes with an absent group field (undefined)', () => {
    const nodes = [{ id: 'a' }, { id: 'b', group: 'alpha' }]
    const result = groupMembers(nodes)
    expect(result.size).toBe(1)
    expect(result.get('alpha')).toHaveLength(1)
  })

  it('handles all nodes in the same group', () => {
    const nodes = [
      { id: 'a', group: 'only' },
      { id: 'b', group: 'only' },
      { id: 'c', group: 'only' },
    ]
    const result = groupMembers(nodes)
    expect(result.size).toBe(1)
    expect(result.get('only')).toHaveLength(3)
  })

  it('preserves the original node objects in the buckets', () => {
    const node = { id: 'x', group: 'g', extra: 42 }
    const result = groupMembers([node])
    expect(result.get('g')![0]).toBe(node)
  })
})

describe('hullEligible', () => {
  it('returns false for an empty array', () => {
    expect(hullEligible([])).toBe(false)
  })

  it('returns false for fewer than 4 members', () => {
    expect(hullEligible([1])).toBe(false)
    expect(hullEligible([1, 2])).toBe(false)
    expect(hullEligible([1, 2, 3])).toBe(false)
  })

  it('returns true for exactly 4 members', () => {
    expect(hullEligible([1, 2, 3, 4])).toBe(true)
  })

  it('returns true for more than 4 members', () => {
    expect(hullEligible([1, 2, 3, 4, 5, 6, 7, 8])).toBe(true)
  })

  it('the threshold is exactly 4: 3 is false, 4 is true', () => {
    const three = Array(3).fill({})
    const four = Array(4).fill({})
    expect(hullEligible(three)).toBe(false)
    expect(hullEligible(four)).toBe(true)
  })
})
