import { JSDOM } from 'jsdom'
import { beforeEach, describe, expect, test, vi } from 'vitest'
import type { Match, SearchRequest, SearchResponse } from './api'

// vi.mock's factory is hoisted above every import in this file, including
// `./search` (which imports `./api`'s postSearch) — the mock function itself
// must live inside vi.hoisted() for the same reason main.test.ts's fixtures
// do: a plain top-level const would still be in its temporal dead zone when
// the factory runs.
const { postSearchMock } = vi.hoisted(() => ({ postSearchMock: vi.fn() }))

vi.mock('./api', () => ({
  postSearch: postSearchMock,
}))

import { renderResults, renderSearchBanner, runSearch } from './search'

function baseResponse(overrides: Partial<SearchResponse> = {}): SearchResponse {
  return { matches: [], model: 'nomic-embed-text', ...overrides }
}

describe('renderResults', () => {
  test('renders results and selects', () => {
    const el = document.createElement('div')
    const onSelect = vi.fn()
    renderResults(el, { matches: [{ id: 'a', title: 'A', type: 'note', score: 0.9 }], model: 'm' } as any, onSelect)
    const item = el.querySelector<HTMLElement>('[data-id="a"]')!
    expect(item.textContent).toContain('A')
    item.click()
    expect(onSelect).toHaveBeenCalledWith('a')
  })

  test('shows both title and type for each match', () => {
    const el = document.createElement('div')
    const matches: Match[] = [
      { id: 'a', title: 'A', type: 'note', score: 0.9 },
      { id: 'b', title: 'B', type: 'spec', score: 0.4 },
    ]
    renderResults(el, baseResponse({ matches }), () => {})

    expect(el.querySelectorAll('[data-id]')).toHaveLength(2)
    expect(el.querySelector('[data-id="a"]')?.textContent).toContain('note')
    expect(el.querySelector('[data-id="b"]')?.textContent).toContain('spec')
  })

  test('shows final_score when present, falls back to score otherwise', () => {
    const el = document.createElement('div')
    const matches: Match[] = [
      { id: 'a', title: 'A', type: 'note', score: 0.5, final_score: 0.81 },
      { id: 'b', title: 'B', type: 'note', score: 0.42 },
    ]
    renderResults(el, baseResponse({ matches }), () => {})

    expect(el.querySelector('[data-id="a"]')?.textContent).toContain('0.810')
    expect(el.querySelector('[data-id="a"]')?.textContent).not.toContain('0.500')
    expect(el.querySelector('[data-id="b"]')?.textContent).toContain('0.420')
  })

  test('an empty result set renders a "no matches" state, not an error', () => {
    const el = document.createElement('div')
    renderResults(el, baseResponse(), () => {})
    expect(el.querySelector('.results-empty')).not.toBeNull()
    expect(el.querySelector('ul')).toBeNull()
  })

  test('re-render replaces the previous contents rather than appending', () => {
    const el = document.createElement('div')
    renderResults(el, baseResponse({ matches: [{ id: 'a', title: 'A', type: 'note', score: 1 }] }), () => {})
    renderResults(el, baseResponse({ matches: [{ id: 'b', title: 'B', type: 'note', score: 1 }] }), () => {})
    expect(el.querySelectorAll('[data-id]')).toHaveLength(1)
    expect(el.querySelector('[data-id="a"]')).toBeNull()
  })

  describe('a malicious title/type is escaped, not injected', () => {
    const imgPayload = '<img src=x onerror=alert(1)>'

    test('no <img> is created and the raw text survives', () => {
      const el = document.createElement('div')
      renderResults(el, baseResponse({ matches: [{ id: 'a', title: imgPayload, type: 'note', score: 1 }] }), () => {})
      expect(el.querySelector('img')).toBeNull()
      expect(el.querySelector('[data-id="a"]')?.textContent).toContain(imgPayload)
    })

    test('never executes, even when force-parsed by a live document', () => {
      // A <script> tag is the reliable execution proof under jsdom (it runs
      // on parse regardless of resource loading, unlike an <img onerror>
      // which needs an actual image-load failure to fire) — same mechanism
      // reader.test.ts's equivalent check uses.
      const scriptPayload = '<script>window.__searchTestExecuted = true</script>'
      const el = document.createElement('div')
      renderResults(
        el,
        baseResponse({ matches: [{ id: 'a', title: scriptPayload, type: scriptPayload, score: 1 }] }),
        () => {},
      )

      // Positive control: confirm this harness can detect execution at all.
      const control = new JSDOM('<!doctype html><html><body></body></html>', { runScripts: 'dangerously' })
      control.window.document.open()
      control.window.document.write('<script>window.__controlExecuted = true</script>')
      control.window.document.close()
      expect((control.window as unknown as { __controlExecuted?: boolean }).__controlExecuted).toBe(true)

      // The real check: force-parse the rendered output's actual serialized
      // HTML the same way. If title/type had been injected via innerHTML
      // instead of textContent, this would fire __searchTestExecuted.
      const sandbox = new JSDOM('<!doctype html><html><body></body></html>', { runScripts: 'dangerously' })
      sandbox.window.document.open()
      sandbox.window.document.write(`<body>${el.innerHTML}</body>`)
      sandbox.window.document.close()
      expect(
        (sandbox.window as unknown as { __searchTestExecuted?: boolean }).__searchTestExecuted,
      ).toBeUndefined()
    })
  })
})

describe('renderSearchBanner', () => {
  test('renders a notice banner by default (the expected 422 degradation)', () => {
    const el = document.createElement('div')
    renderSearchBanner(el, 'Semantic search is unavailable right now.')
    const banner = el.querySelector('.results-banner')
    expect(banner).not.toBeNull()
    expect(banner?.classList.contains('results-banner-error')).toBe(false)
    expect(banner?.textContent).toBe('Semantic search is unavailable right now.')
  })

  test('renders a visually distinct error variant for a real failure', () => {
    const el = document.createElement('div')
    renderSearchBanner(el, 'bad request body: unexpected EOF', 'error')
    const banner = el.querySelector('.results-banner-error')
    expect(banner).not.toBeNull()
    expect(banner?.textContent).toBe('bad request body: unexpected EOF')
  })

  test('re-render replaces the previous banner rather than appending', () => {
    const el = document.createElement('div')
    renderSearchBanner(el, 'first')
    renderSearchBanner(el, 'second', 'error')
    expect(el.querySelectorAll('.results-banner')).toHaveLength(1)
    expect(el.textContent).toBe('second')
  })
})

describe('runSearch', () => {
  beforeEach(() => {
    postSearchMock.mockReset()
  })

  test('an untouched form defaults hops/weight to 0 and omits edge_types', async () => {
    postSearchMock.mockResolvedValueOnce(baseResponse())
    await runSearch('hello')

    expect(postSearchMock).toHaveBeenCalledTimes(1)
    const [sentReq] = postSearchMock.mock.calls[0] as [SearchRequest]
    expect(sentReq.q).toBe('hello')
    expect(sentReq.expand).toBe(false)
    expect(sentReq.hops).toBe(0)
    expect(sentReq.weight).toBe(0)
    expect(sentReq.explain).toBe(false)
    // The key itself must be absent (not merely undefined-valued) so
    // JSON.stringify drops it — an explicit `[]` would be a real
    // "no edge types" override on the Go side, not an "unset" signal.
    expect('edge_types' in sentReq).toBe(false)
  })

  test('forwards an explicit hops/weight override and ties explain to expand', async () => {
    postSearchMock.mockResolvedValueOnce(baseResponse())
    await runSearch('hello', { expand: true, hops: 2, weight: 0.4 })

    const [sentReq] = postSearchMock.mock.calls[0] as [SearchRequest]
    expect(sentReq.expand).toBe(true)
    expect(sentReq.hops).toBe(2)
    expect(sentReq.weight).toBe(0.4)
    expect(sentReq.explain).toBe(true)
  })

  test('expand on with hops/weight left blank still sends 0 (inherit the manifest default)', async () => {
    postSearchMock.mockResolvedValueOnce(baseResponse())
    await runSearch('hello', { expand: true })

    const [sentReq] = postSearchMock.mock.calls[0] as [SearchRequest]
    expect(sentReq.expand).toBe(true)
    expect(sentReq.hops).toBe(0)
    expect(sentReq.weight).toBe(0)
    expect(sentReq.explain).toBe(true)
  })

  test('propagates a postSearch failure (e.g. SearchUnavailableError) unchanged', async () => {
    const err = new Error('embedder unavailable')
    postSearchMock.mockRejectedValueOnce(err)
    await expect(runSearch('hello')).rejects.toBe(err)
  })
})
