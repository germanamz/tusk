import { beforeAll, describe, expect, it, vi } from 'vitest'

// A sub-unit-shaped id ("<fileID>#<address>") nested under a folder, so the
// round-trip test exercises both traps at once: a literal "/" that must
// survive as a real path separator, and a literal "#" that must NOT be read
// back as a URL fragment delimiter.
//
// vi.mock's factory below is hoisted above every import in this file,
// including `./main` (which itself imports `./api`) — so this data must be
// built inside vi.hoisted(), not as a plain top-level const the factory
// closes over. A plain const would still be in its temporal dead zone when
// `./main`'s top-level boot() calls the mocked fetchIndex during module
// evaluation, one line before this const would otherwise have run.
const { trickyId, sampleIndex, sampleNode } = vi.hoisted(() => {
  const trickyId = 'notes/c#S1P1'
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
  }
})

vi.mock('./api', () => ({
  fetchIndex: vi.fn(async () => sampleIndex),
  fetchNode: vi.fn(async (id: string) => {
    if (id !== trickyId) throw new Error(`unexpected id: ${id}`)
    return sampleNode
  }),
}))

import { buildNodeHash, parseNodeHash, ready } from './main'

describe('hash routing', () => {
  beforeAll(async () => {
    await ready
  })

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
