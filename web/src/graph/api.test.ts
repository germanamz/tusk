import { describe, it, expect, vi, afterEach } from 'vitest'
import type { Graph, EmbeddingsResponse } from './api'
import { fetchEmbeddings } from './api'

describe('Graph type', () => {
  it('parses a snapshot shape', () => {
    const sample: Graph = {
      generation: 1,
      epoch: 0,
      nodes: [],
      edges: [],
      cluster: { by: 'type', huddle: false, hull: false },
    }
    expect(sample.nodes.length).toBe(0)
  })
})

describe('fetchEmbeddings', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('returns the parsed payload on a 200', async () => {
    const payload: EmbeddingsResponse = {
      model: 'nomic-embed-text',
      dim: 2,
      signature: 'abc',
      vectors: { 'node-a': [0.6, 0.8] },
    }
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => ({ ok: true, json: async () => payload })),
    )
    const out = await fetchEmbeddings()
    expect(out.model).toBe('nomic-embed-text')
    expect(out.vectors['node-a']).toEqual([0.6, 0.8])
  })

  it('throws on a non-ok response', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => ({ ok: false, status: 503, json: async () => ({}) })),
    )
    await expect(fetchEmbeddings()).rejects.toThrow('503')
  })
})
