import { afterEach, describe, expect, it, vi } from 'vitest'
import {
  fetchIndex,
  fetchNode,
  fetchRelated,
  postSearch,
  SearchUnavailableError,
  type IndexResponse,
  type NodeReadPayload,
  type RelatedResponse,
  type SearchRequest,
  type SearchResponse,
} from './api'

function stubFetch(impl: (...args: unknown[]) => unknown) {
  const fetchMock = vi.fn(impl)
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('fetchIndex', () => {
  it('GETs the namespaced index endpoint', async () => {
    const payload: IndexResponse = { nodes: [{ id: 'a', type: 'note', title: 'A', path: 'a.md' }] }
    const fetchMock = stubFetch(async () => ({ ok: true, json: async () => payload }))
    const out = await fetchIndex()
    expect(fetchMock).toHaveBeenCalledWith('/api/read/index', expect.anything())
    expect(out).toEqual(payload)
  })

  it('throws on a non-ok response', async () => {
    stubFetch(async () => ({ ok: false, status: 503, json: async () => ({}) }))
    await expect(fetchIndex()).rejects.toThrow('503')
  })
})

describe('fetchNode', () => {
  it('percent-encodes the id and preserves "/" separators', async () => {
    const payload: NodeReadPayload = {
      id: 'notes/c#S1P1',
      type: 'note',
      title: 'C',
      path: 'notes/c.md',
      properties: {},
      markdown: 'body',
      links: { out: [], in: [] },
      wikilinks: {},
    }
    const fetchMock = stubFetch(async () => ({ ok: true, json: async () => payload }))
    await fetchNode('notes/c#S1P1')
    expect(fetchMock).toHaveBeenCalledWith(
      '/api/read/node/notes/' + encodeURIComponent('c#S1P1'),
      expect.anything(),
    )
  })

  it('throws on a non-ok response', async () => {
    stubFetch(async () => ({ ok: false, status: 404, json: async () => ({}) }))
    await expect(fetchNode('notes/x')).rejects.toThrow('404')
  })
})

describe('postSearch', () => {
  const req: SearchRequest = {
    q: 'hello',
    filter: '',
    expand: false,
    hops: 1,
    edge_types: [],
    weight: 0.2,
    limit: 50,
    explain: false,
  }

  it('POSTs the JSON body to the namespaced search endpoint', async () => {
    const payload: SearchResponse = { matches: [], model: 'nomic-embed-text' }
    const fetchMock = stubFetch(async () => ({ ok: true, status: 200, json: async () => payload }))
    const out = await postSearch(req)
    expect(fetchMock).toHaveBeenCalledWith(
      '/api/read/search',
      expect.objectContaining({ method: 'POST', body: JSON.stringify(req) }),
    )
    expect(out).toEqual(payload)
  })

  it('throws SearchUnavailableError on a 422, distinguishable from a real failure', async () => {
    stubFetch(async () => ({ ok: false, status: 422, text: async () => 'embedder unavailable' }))
    await expect(postSearch(req)).rejects.toBeInstanceOf(SearchUnavailableError)
  })

  it('throws a plain Error (not SearchUnavailableError) on a 503', async () => {
    stubFetch(async () => ({ ok: false, status: 503, text: async () => 'boom' }))
    const failure = postSearch(req)
    await expect(failure).rejects.toThrow('503')
    await expect(failure).rejects.not.toBeInstanceOf(SearchUnavailableError)
  })

  it('throws a plain Error on a 400', async () => {
    stubFetch(async () => ({ ok: false, status: 400, text: async () => 'bad body' }))
    await expect(postSearch(req)).rejects.toThrow('400')
  })
})

describe('fetchRelated', () => {
  it('omits hops/weight/edge_types entirely when unspecified', async () => {
    const payload: RelatedResponse = { related: [] }
    const fetchMock = stubFetch(async () => ({ ok: true, json: async () => payload }))
    await fetchRelated('notes/a')
    expect(fetchMock).toHaveBeenCalledWith('/api/read/related/notes/a', expect.anything())
  })

  it('does NOT send weight=0 for an unspecified weight (presence must be preserved)', async () => {
    const fetchMock = stubFetch(async () => ({ ok: true, json: async () => ({ related: [] }) }))
    await fetchRelated('notes/a', { hops: 2 })
    const calledUrl = fetchMock.mock.calls[0][0] as string
    expect(calledUrl).toContain('hops=2')
    expect(calledUrl).not.toContain('weight')
  })

  it('sends an explicit weight=0 when the caller passes 0 (distinguishable from absent)', async () => {
    const fetchMock = stubFetch(async () => ({ ok: true, json: async () => ({ related: [] }) }))
    await fetchRelated('notes/a', { weight: 0 })
    const calledUrl = fetchMock.mock.calls[0][0] as string
    expect(calledUrl).toContain('weight=0')
  })

  it('joins edgeTypes with commas and percent-encodes the id', async () => {
    const fetchMock = stubFetch(async () => ({ ok: true, json: async () => ({ related: [] }) }))
    await fetchRelated('notes/c#S1', { hops: 1, weight: 0.5, edgeTypes: ['links-to', 'refs'] })
    const calledUrl = fetchMock.mock.calls[0][0] as string
    expect(calledUrl).toContain('notes/' + encodeURIComponent('c#S1'))
    expect(calledUrl).toContain('hops=1')
    expect(calledUrl).toContain('weight=0.5')
    expect(calledUrl).toContain('edge_types=links-to%2Crefs')
  })

  it('throws on a non-ok response', async () => {
    stubFetch(async () => ({ ok: false, status: 503, json: async () => ({}) }))
    await expect(fetchRelated('notes/a')).rejects.toThrow('503')
  })
})
