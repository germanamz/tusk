import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'

// A sub-unit-shaped id ("<fileID>#<address>") nested under a folder, so the
// round-trip test exercises both traps at once: a literal "/" that must
// survive as a real path separator, and a literal "#" that must NOT be read
// back as a URL fragment delimiter.
//
// vi.mock's factory below is hoisted above every import in this file,
// including `./index` (which itself imports `./api`) — so this data must be
// built inside vi.hoisted(), not as a plain top-level const the factory
// closes over.
//
// jsdom has no real EventSource implementation (see stream.test.ts); the fake
// below must exist before `./index` is imported so subscribeChanges finds it
// when mount() runs. It captures the registered 'change' handler in
// `sseListeners` so tests can simulate a live-reload signal by invoking it
// directly.
//
// jsdom also has no window.matchMedia, which the theme controller (theme.ts,
// imported by ./index for onThemeChange) reads at module load — so a minimal
// stub for it is installed here too, ahead of the import.
const { trickyId, sampleIndex, sampleNode, sseListeners } = vi.hoisted(() => {
  const trickyId = 'notes/c#S1P1'
  const sseListeners: Record<string, (event: { data: string }) => void> = {}

  ;(globalThis as unknown as { EventSource: unknown }).EventSource = class {
    addEventListener(type: string, handler: (event: { data: string }) => void) {
      sseListeners[type] = handler
    }
    close() {}
  }

  ;(window as unknown as { matchMedia: unknown }).matchMedia = () => ({
    matches: false,
    media: '',
    onchange: null,
    addEventListener() {},
    removeEventListener() {},
    addListener() {},
    removeListener() {},
    dispatchEvent() {
      return false
    },
  })

  return {
    trickyId,
    sampleIndex: { nodes: [{ id: trickyId, type: 'note', title: 'C', path: 'notes/c.md' }] },
    sampleNode: {
      id: trickyId,
      type: 'note',
      title: 'C Section',
      path: 'notes/c.md',
      properties: {},
      markdown: '# Hello',
      links: { out: [], in: [] },
      wikilinks: {},
    },
    sseListeners,
  }
})

function fireChange(): void {
  sseListeners['change']?.({ data: '{"generation":1,"epoch":1}' })
}

// postSearch and SearchUnavailableError are kept real (via importActual)
// rather than stubbed: the search describe block below drives them
// end-to-end by stubbing the global `fetch` instead (same technique
// api.test.ts uses), so it exercises index.ts's actual wiring of
// runSearch/renderResults/renderSearchBanner rather than a second, separate
// fake of that wiring.
vi.mock('./api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('./api')>()
  return {
    ...actual,
    fetchIndex: vi.fn(async () => sampleIndex),
    fetchNode: vi.fn(async (id: string) => {
      if (id !== trickyId) throw new Error(`unexpected id: ${id}`)
      return sampleNode
    }),
  }
})

import { fetchIndex, fetchNode } from './api'
import { buildNodeHash, mount, parseNodeHash } from './index'

// Shared by both the 'search' and 'live reload' describe blocks below.
function stubFetch(impl: (...args: unknown[]) => unknown) {
  const fetchMock = vi.fn(impl)
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

function submitQuery(q: string, expand = false): void {
  const form = document.getElementById('search-form') as HTMLFormElement
  ;(form.querySelector('input[name="q"]') as HTMLInputElement).value = q
  ;(form.querySelector('input[name="expand"]') as HTMLInputElement).checked = expand
  form.dispatchEvent(new Event('submit', { cancelable: true }))
}

async function flush(): Promise<void> {
  await Promise.resolve()
  await Promise.resolve()
  await Promise.resolve()
}

// mount() renders into a container rather than taking over document.body, so
// the whole suite shares one mounted instance in a container attached to the
// document — the existing tests still query through `document`, which reaches
// into it. Awaiting mount is the deterministic replacement for the standalone
// entry's `await ready`.
beforeAll(async () => {
  const container = document.createElement('div')
  document.body.appendChild(container)
  await mount(container)
})

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('hash routing', () => {
  it('round-trips an id containing both "/" and "#" through the hash route', () => {
    const hash = buildNodeHash(trickyId)
    // encodeId keeps real "/" separators and percent-encodes "#" within a
    // segment — a naive `hash.split('/')[2]` truncation would lose
    // everything from "#" onward instead of round-tripping it.
    expect(hash).toBe('#/node/notes/c%23S1P1')
    expect(parseNodeHash(hash)).toBe(trickyId)
  })

  it('an unrelated hash does not parse as a node route', () => {
    expect(parseNodeHash('#/search?q=x')).toBeNull()
    expect(parseNodeHash('')).toBeNull()
    expect(parseNodeHash('#/node/')).toBeNull()
  })

  it('paints the three-pane shell and lists the index in Contents', () => {
    expect(document.querySelector('header.shell-header')).not.toBeNull()
    expect(document.getElementById('contents')).not.toBeNull()
    expect(document.getElementById('reader')).not.toBeNull()
    expect(document.getElementById('rails')).not.toBeNull()

    const entry = document.querySelector<HTMLButtonElement>(`button[data-id="${trickyId}"]`)
    expect(entry).not.toBeNull()
  })

  it('selecting a Contents entry sets location.hash and routes to the fetched node', async () => {
    const entry = document.querySelector<HTMLButtonElement>(`button[data-id="${trickyId}"]`)
    entry?.click()

    expect(location.hash).toBe(buildNodeHash(trickyId))

    // In a real browser, assigning location.hash dispatches 'hashchange' on
    // its own; dispatch it explicitly here so the assertion below does not
    // depend on jsdom's own timing for that event.
    window.dispatchEvent(new Event('hashchange'))
    await Promise.resolve()
    await Promise.resolve()
    await Promise.resolve()

    expect(document.getElementById('reader')?.textContent).toContain('C Section')
  })
})

describe('search', () => {
  it('toggling Expand reveals the hops/weight fields', () => {
    const expandBox = document.getElementById('search-expand-toggle') as HTMLInputElement
    const fields = document.getElementById('search-expand-fields') as HTMLElement
    expect(fields.hidden).toBe(true)

    expandBox.checked = true
    expandBox.dispatchEvent(new Event('change'))
    expect(fields.hidden).toBe(false)

    expandBox.checked = false
    expandBox.dispatchEvent(new Event('change'))
    expect(fields.hidden).toBe(true)
  })

  it('an empty query does not submit a search', () => {
    const fetchMock = stubFetch(async () => ({ ok: true, status: 200, json: async () => ({ matches: [] }) }))
    submitQuery('   ')
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it('submitting a query swaps the left pane into Results mode, and "Contents" restores the browse list', async () => {
    stubFetch(async () => ({
      ok: true,
      status: 200,
      json: async () => ({
        matches: [{ id: trickyId, title: 'C Section', type: 'note', score: 0.9 }],
        model: 'm',
      }),
    }))

    submitQuery('hello')
    await flush()

    expect(document.querySelector('.contents-tree')).toBeNull()
    expect(document.querySelector('.results-back')).not.toBeNull()

    const resultEntry = document.querySelector<HTMLButtonElement>(`.results-list [data-id="${trickyId}"]`)
    expect(resultEntry).not.toBeNull()
    expect(resultEntry?.textContent).toContain('C Section')

    document.querySelector<HTMLButtonElement>('.results-back')?.click()

    expect(document.querySelector('.contents-tree')).not.toBeNull()
    expect(document.querySelector('.results-back')).toBeNull()
  })

  it('clicking a result routes to it, the same way a Contents entry does', async () => {
    stubFetch(async () => ({
      ok: true,
      status: 200,
      json: async () => ({
        matches: [{ id: trickyId, title: 'C Section', type: 'note', score: 0.9 }],
        model: 'm',
      }),
    }))

    submitQuery('hello')
    await flush()

    document.querySelector<HTMLButtonElement>(`.results-list [data-id="${trickyId}"]`)?.click()
    expect(location.hash).toBe(buildNodeHash(trickyId))

    window.dispatchEvent(new Event('hashchange'))
    await flush()

    expect(document.getElementById('reader')?.textContent).toContain('C Section')
  })

  it('a 422 (embedder down) renders a notice banner, not an error state', async () => {
    stubFetch(async () => ({ ok: false, status: 422, text: async () => 'embedder unavailable' }))

    submitQuery('hello')
    await flush()

    const banner = document.querySelector('.results-banner')
    expect(banner).not.toBeNull()
    expect(banner?.classList.contains('results-banner-error')).toBe(false)
    expect(document.querySelector('.results-back')).not.toBeNull()
  })

  it('a 503 (real failure) renders the distinct error banner variant', async () => {
    stubFetch(async () => ({ ok: false, status: 503, text: async () => 'index corrupt' }))

    submitQuery('hello')
    await flush()

    expect(document.querySelector('.results-banner-error')).not.toBeNull()
  })

  it('a 400 (bad request) also renders the error banner variant, not the notice', async () => {
    stubFetch(async () => ({ ok: false, status: 400, text: async () => 'bad request body' }))

    submitQuery('hello')
    await flush()

    expect(document.querySelector('.results-banner-error')).not.toBeNull()
  })
})

describe('live reload', () => {
  // Force the left pane back into Contents mode before each test, undoing
  // whatever mode a prior test in this file left behind (the whole suite
  // shares the one mount() instance).
  beforeEach(() => {
    document.querySelector<HTMLButtonElement>('.results-back')?.click()
  })

  it('a change event while Contents is showing refetches the index and re-renders it', async () => {
    const callsBefore = vi.mocked(fetchIndex).mock.calls.length

    fireChange()
    await flush()

    expect(vi.mocked(fetchIndex).mock.calls.length).toBe(callsBefore + 1)
    expect(document.querySelector('.contents-tree')).not.toBeNull()
  })

  it('Results mode is NOT clobbered by a change event — a re-run affordance shows instead, but the index data is still refreshed underneath', async () => {
    stubFetch(async () => ({
      ok: true,
      status: 200,
      json: async () => ({
        matches: [{ id: trickyId, title: 'C Section', type: 'note', score: 0.9 }],
        model: 'm',
      }),
    }))

    submitQuery('hello')
    await flush()
    expect(document.querySelector(`.results-list [data-id="${trickyId}"]`)).not.toBeNull()

    const callsBefore = vi.mocked(fetchIndex).mock.calls.length

    fireChange()
    await flush()

    // The result list — and the whole Results-mode DOM — is untouched: not
    // silently re-run, not replaced by the Contents tree. This is the part
    // spec §7 actually protects: the repaint, not the data fetch.
    expect(document.querySelector(`.results-list [data-id="${trickyId}"]`)).not.toBeNull()
    expect(document.querySelector('.contents-tree')).toBeNull()

    // The underlying index data IS refreshed even in Results mode, so a
    // later "← Contents" has current data to repaint from rather than a
    // stale snapshot from before the change event.
    expect(vi.mocked(fetchIndex).mock.calls.length).toBe(callsBefore + 1)

    const notice = document.querySelector('.results-stale-banner')
    expect(notice).not.toBeNull()
    expect(notice?.textContent).toContain('re-run search')
  })

  it('after a change event in Results mode, "← Contents" repaints with the freshly fetched index, not the stale snapshot from before the change', async () => {
    stubFetch(async () => ({
      ok: true,
      status: 200,
      json: async () => ({
        matches: [{ id: trickyId, title: 'C Section', type: 'note', score: 0.9 }],
        model: 'm',
      }),
    }))

    submitQuery('hello')
    await flush()
    expect(document.querySelector('.contents-tree')).toBeNull()

    const newId = 'notes/brand-new'
    vi.mocked(fetchIndex).mockResolvedValueOnce({
      nodes: [...sampleIndex.nodes, { id: newId, type: 'note', title: 'Brand New', path: 'notes/brand-new.md' }],
    })

    fireChange()
    await flush()

    // Still parked in Results mode — untouched, per spec §7.
    expect(document.querySelector(`.results-list [data-id="${trickyId}"]`)).not.toBeNull()
    expect(document.querySelector('.contents-tree')).toBeNull()

    document.querySelector<HTMLButtonElement>('.results-back')?.click()

    // The freshly fetched index (containing the node that arrived during
    // the change event) is what renders — not the snapshot fetched at boot,
    // before the user ever left Contents. Against the old code (which only
    // called fetchIndex inside the mode === 'contents' branch), the mocked
    // fetchIndex would never have been re-invoked in Results mode, so this
    // node would be absent here.
    expect(document.querySelector(`.contents-tree [data-id="${newId}"]`)).not.toBeNull()
  })

  it('a second change event while the notice is already showing does not duplicate it', async () => {
    stubFetch(async () => ({
      ok: true,
      status: 200,
      json: async () => ({ matches: [], model: 'm' }),
    }))

    submitQuery('hello')
    await flush()

    fireChange()
    await flush()
    fireChange()
    await flush()

    expect(document.querySelectorAll('.results-stale-banner')).toHaveLength(1)
  })

  it('a change event refetches the currently open node', async () => {
    location.hash = buildNodeHash(trickyId)
    window.dispatchEvent(new Event('hashchange'))
    await flush()

    const callsBefore = vi.mocked(fetchNode).mock.calls.length

    fireChange()
    await flush()

    expect(vi.mocked(fetchNode).mock.calls.length).toBe(callsBefore + 1)
  })
})
