import { describe, it, expect } from 'vitest'
import { carryPositions } from './layout'

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
