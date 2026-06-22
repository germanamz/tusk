import { describe, it, expect } from 'vitest'
import type { Graph } from './api'

describe('Graph type', () => {
  it('parses a snapshot shape', () => {
    const sample: Graph = { generation: 1, epoch: 0, nodes: [], edges: [] }
    expect(sample.nodes.length).toBe(0)
  })
})
