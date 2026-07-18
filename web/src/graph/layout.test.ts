import { describe, it, expect } from 'vitest'
import { carryPositions, fibonacciSphereAnchors } from './layout'

describe('carryPositions', () => {
  it('copies coordinates for ids present in prev', () => {
    const prev = new Map([
      ['node-a', { x: 10, y: 20, z: 30, vx: 1, vy: 2, vz: 3 }],
    ])
    const nextNodes = [{ id: 'node-a', title: 'A', type: 'note', degree: 2 }]
    const result = carryPositions(prev, nextNodes)

    expect(result).toHaveLength(1)
    expect(result[0].x).toBe(10)
    expect(result[0].y).toBe(20)
    expect(result[0].z).toBe(30)
    expect(result[0].vx).toBe(1)
    expect(result[0].vy).toBe(2)
    expect(result[0].vz).toBe(3)
    // Original node payload fields are preserved
    expect(result[0].id).toBe('node-a')
    expect(result[0].title).toBe('A')
    expect(result[0].type).toBe('note')
    expect(result[0].degree).toBe(2)
  })

  it('returns coord-less objects for new ids not in prev', () => {
    const prev = new Map([
      ['node-a', { x: 10, y: 20, z: 30, vx: 1, vy: 2, vz: 3 }],
    ])
    const nextNodes = [{ id: 'node-b', title: 'B', type: 'note', degree: 0 }]
    const result = carryPositions(prev, nextNodes)

    expect(result).toHaveLength(1)
    expect(result[0].x).toBeUndefined()
    expect(result[0].y).toBeUndefined()
    expect(result[0].z).toBeUndefined()
    expect(result[0].vx).toBeUndefined()
    expect(result[0].vy).toBeUndefined()
    expect(result[0].vz).toBeUndefined()
  })

  it('omits ids absent from nextNodes (removed nodes are not returned)', () => {
    const prev = new Map([
      ['node-a', { x: 10, y: 20, z: 30 }],
      ['node-b', { x: 50, y: 60, z: 70 }],
    ])
    const nextNodes = [{ id: 'node-a', title: 'A', type: 'note', degree: 1 }]
    const result = carryPositions(prev, nextNodes)

    expect(result).toHaveLength(1)
    expect(result[0].id).toBe('node-a')
    // node-b is absent — it was removed from the next snapshot
    expect(result.find((nd: any) => nd.id === 'node-b')).toBeUndefined()
  })

  it('never mutates the input prev objects', () => {
    const priorNode = { x: 10, y: 20, z: 30, vx: 1, vy: 2, vz: 3 }
    const prev = new Map([['node-a', priorNode]])
    const nextNodes = [{ id: 'node-a', title: 'A', type: 'note', degree: 5 }]
    carryPositions(prev, nextNodes)

    // The prior object in the map must be unchanged
    expect(priorNode.x).toBe(10)
    expect(priorNode.y).toBe(20)
    expect(priorNode.z).toBe(30)
    expect(priorNode.vx).toBe(1)
    expect(priorNode.vy).toBe(2)
    expect(priorNode.vz).toBe(3)
    // The map itself must still have the same entry
    expect(prev.get('node-a')).toBe(priorNode)
  })

  it('only copies coordinate fields that are defined on the prior node', () => {
    // A node whose prior entry has only x/y (not z/vx/vy/vz) should not get
    // those undefined fields stamped onto the result object.
    const prev = new Map([['node-a', { x: 5, y: 15 }]])
    const nextNodes = [{ id: 'node-a', title: 'A', type: 'note', degree: 0 }]
    const result = carryPositions(prev, nextNodes)

    expect(result[0].x).toBe(5)
    expect(result[0].y).toBe(15)
    expect(result[0].z).toBeUndefined()
    expect(result[0].vx).toBeUndefined()
  })

  it('handles an empty nextNodes array', () => {
    const prev = new Map([['node-a', { x: 10, y: 20, z: 30 }]])
    const result = carryPositions(prev, [])
    expect(result).toHaveLength(0)
  })

  it('handles an empty prev map (all nodes are treated as new)', () => {
    const prev = new Map<string, {}>()
    const nextNodes = [
      { id: 'node-a', title: 'A', type: 'note', degree: 1 },
      { id: 'node-b', title: 'B', type: 'spec', degree: 3 },
    ]
    const result = carryPositions(prev, nextNodes)

    expect(result).toHaveLength(2)
    for (const nd of result) {
      expect((nd as any).x).toBeUndefined()
      expect((nd as any).y).toBeUndefined()
    }
  })
})

describe('fibonacciSphereAnchors', () => {
  const RADIUS = 400
  const EPSILON = 1e-9

  it('returns an empty map for an empty input', () => {
    const result = fibonacciSphereAnchors([], RADIUS)
    expect(result.size).toBe(0)
  })

  it('returns one anchor for a single group with finite, non-NaN coordinates', () => {
    const result = fibonacciSphereAnchors(['alpha'], RADIUS)
    expect(result.size).toBe(1)
    const anchor = result.get('alpha')!
    expect(anchor).toBeDefined()
    expect(isFinite(anchor.x)).toBe(true)
    expect(isFinite(anchor.y)).toBe(true)
    expect(isFinite(anchor.z)).toBe(true)
    expect(isNaN(anchor.x)).toBe(false)
    expect(isNaN(anchor.y)).toBe(false)
    expect(isNaN(anchor.z)).toBe(false)
  })

  it('places the single-group anchor on the sphere surface (|v| ≈ radius)', () => {
    const result = fibonacciSphereAnchors(['alpha'], RADIUS)
    const { x, y, z } = result.get('alpha')!
    const dist = Math.sqrt(x * x + y * y + z * z)
    expect(Math.abs(dist - RADIUS)).toBeLessThan(EPSILON)
  })

  it('returns distinct anchors for each distinct group', () => {
    const result = fibonacciSphereAnchors(['a', 'b', 'c'], RADIUS)
    expect(result.size).toBe(3)
    const anchors = [...result.values()]
    for (let ii = 0; ii < anchors.length; ii++) {
      for (let jj = ii + 1; jj < anchors.length; jj++) {
        const dx = anchors[ii].x - anchors[jj].x
        const dy = anchors[ii].y - anchors[jj].y
        const dz = anchors[ii].z - anchors[jj].z
        const dist = Math.sqrt(dx * dx + dy * dy + dz * dz)
        expect(dist).toBeGreaterThan(EPSILON)
      }
    }
  })

  it('places every anchor on the sphere surface (|v| ≈ radius)', () => {
    const result = fibonacciSphereAnchors(['a', 'b', 'c', 'd', 'e'], RADIUS)
    for (const { x, y, z } of result.values()) {
      const dist = Math.sqrt(x * x + y * y + z * z)
      expect(Math.abs(dist - RADIUS)).toBeLessThan(EPSILON)
    }
  })

  it('is deterministic: same groups, same anchors regardless of input order', () => {
    const forward = fibonacciSphereAnchors(['a', 'b', 'c'], RADIUS)
    const shuffled = fibonacciSphereAnchors(['c', 'a', 'b'], RADIUS)
    expect(shuffled.size).toBe(forward.size)
    for (const [group, anchor] of forward) {
      const other = shuffled.get(group)!
      expect(other).toBeDefined()
      expect(Math.abs(other.x - anchor.x)).toBeLessThan(EPSILON)
      expect(Math.abs(other.y - anchor.y)).toBeLessThan(EPSILON)
      expect(Math.abs(other.z - anchor.z)).toBeLessThan(EPSILON)
    }
  })

  it('de-duplicates groups: duplicate inputs yield the same map size as distinct inputs', () => {
    const withDupes = fibonacciSphereAnchors(['a', 'b', 'a', 'c', 'b'], RADIUS)
    const distinct = fibonacciSphereAnchors(['a', 'b', 'c'], RADIUS)
    expect(withDupes.size).toBe(distinct.size)
  })

  it('the empty-string group is treated as a valid distinct value', () => {
    const result = fibonacciSphereAnchors(['', 'a'], RADIUS)
    expect(result.size).toBe(2)
    expect(result.has('')).toBe(true)
    expect(result.has('a')).toBe(true)
  })
})
