import { describe, it, expect, vi, afterEach } from 'vitest'
import { fetchNodeDetail, fetchSubunits } from './nodeapi'

describe('fetchNodeDetail', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('percent-encodes a "#" in the id instead of letting it become a fragment', async () => {
    const fetchMock = vi.fn(async () => ({ ok: true, json: async () => ({}) }))
    vi.stubGlobal('fetch', fetchMock)
    await fetchNodeDetail('notes/a#s1')
    expect(fetchMock).toHaveBeenCalledWith('/api/graph/node/notes/a%23s1')
  })

  it('percent-encodes a space but preserves real "/" separators', async () => {
    const fetchMock = vi.fn(async () => ({ ok: true, json: async () => ({}) }))
    vi.stubGlobal('fetch', fetchMock)
    await fetchNodeDetail('notes/foo b')
    expect(fetchMock).toHaveBeenCalledWith('/api/graph/node/notes/foo%20b')
  })

  it('throws on a non-ok response', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => ({ ok: false, status: 404, json: async () => ({}) })),
    )
    await expect(fetchNodeDetail('notes/x')).rejects.toThrow('404')
  })
})

describe('fetchSubunits', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('fetches the dedicated /api/graph/subunits prefix', async () => {
    const fetchMock = vi.fn(async () => ({ ok: true, json: async () => ({ nodes: [], edges: [] }) }))
    vi.stubGlobal('fetch', fetchMock)
    await fetchSubunits('notes/p')
    expect(fetchMock).toHaveBeenCalledWith('/api/graph/subunits/notes/p')
  })

  it('throws on a non-ok response', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => ({ ok: false, status: 503, json: async () => ({}) })),
    )
    await expect(fetchSubunits('notes/p')).rejects.toThrow('503')
  })
})
