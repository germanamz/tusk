import { JSDOM } from 'jsdom'
import { describe, expect, test } from 'vitest'
import { renderRails } from './related'
import type { NodeReadPayload, RelatedNode } from './api'

function basePayload(overrides: Partial<NodeReadPayload> = {}): NodeReadPayload {
  return {
    id: 'a',
    type: 'note',
    title: 'A',
    path: 'a.md',
    properties: {},
    markdown: '',
    links: { out: [], in: [] },
    wikilinks: {},
    ...overrides,
  }
}

test('Related shows graph neighbors, Links shows out/in refs, and every entry navigates', () => {
  const node = basePayload({
    links: {
      out: [{ id: 'f', title: 'F', type: 'note', edge_type: 'links-to' }],
      in: [{ id: 'g', title: 'G', type: 'note', edge_type: 'links-to' }],
    },
  })
  const related: RelatedNode[] = [{ id: 'd', title: 'D', type: 'note', graph_score: 0.6, distance: 1 }]

  const el = document.createElement('div')
  const selected: string[] = []
  renderRails(el, node, related, (id) => selected.push(id))

  const sections = el.querySelectorAll('.rail-section')
  expect(sections).toHaveLength(2)

  const [relatedSection, linksSection] = Array.from(sections)
  expect(relatedSection.querySelector('.rail-heading')?.textContent).toContain('Related')
  expect(relatedSection.textContent).toContain('D')

  expect(linksSection.querySelector('.rail-heading')?.textContent).toBe('Links')
  expect(linksSection.textContent).toContain('→ F')
  expect(linksSection.textContent).toContain('← G')

  const relatedEntry = relatedSection.querySelector<HTMLAnchorElement>('.rail-entry[data-id="d"]')
  relatedEntry?.click()
  expect(selected).toEqual(['d'])

  const outEntry = linksSection.querySelector<HTMLAnchorElement>('.rail-entry[data-id="f"]')
  outEntry?.click()
  expect(selected).toEqual(['d', 'f'])

  const inEntry = linksSection.querySelector<HTMLAnchorElement>('.rail-entry[data-id="g"]')
  inEntry?.click()
  expect(selected).toEqual(['d', 'f', 'g'])
})

test('a graph_score of exactly 0 still renders — it is not omitempty on the wire', () => {
  const node = basePayload()
  const related: RelatedNode[] = [{ id: 'z', title: 'Z', type: 'note', graph_score: 0, distance: 2 }]

  const el = document.createElement('div')
  renderRails(el, node, related, () => {})

  const score = el.querySelector('.rail-entry-score')
  expect(score).not.toBeNull()
  expect(score?.textContent).toBe('0.000')
  const distance = el.querySelector('.rail-entry-distance')
  expect(distance?.textContent).toBe('2 hops')
})

test('an id needing encoding produces a correct href', () => {
  const node = basePayload()
  const related: RelatedNode[] = [
    { id: 'notes/c#S1P1', title: 'C', type: 'note', graph_score: 0.5, distance: 1 },
  ]

  const el = document.createElement('div')
  renderRails(el, node, related, () => {})

  const entry = el.querySelector<HTMLAnchorElement>('.rail-entry[data-id="notes/c#S1P1"]')
  expect(entry?.getAttribute('href')).toBe('#/node/notes/' + encodeURIComponent('c#S1P1'))
})

test('empty related and links render an empty state, not a crash', () => {
  const node = basePayload()
  const el = document.createElement('div')

  expect(() => renderRails(el, node, [], () => {})).not.toThrow()

  const empties = el.querySelectorAll('.rail-empty')
  expect(empties).toHaveLength(2)
  expect(empties[0].textContent).toBe('Nothing related yet.')
  expect(empties[1].textContent).toBe('Nothing linked yet.')
  expect(el.querySelectorAll('.rail-entry')).toHaveLength(0)
})

test('re-render replaces the previous rails rather than appending', () => {
  const node = basePayload()
  const el = document.createElement('div')

  renderRails(el, node, [{ id: 'x', title: 'X', type: 'note', graph_score: 0.2, distance: 1 }], () => {})
  renderRails(el, node, [], () => {})

  expect(el.querySelector('.rail-entry[data-id="x"]')).toBeNull()
  expect(el.querySelectorAll('.rail-section')).toHaveLength(2)
})

describe('a malicious title/type is escaped, not injected', () => {
  const imgPayload = '<img src=x onerror=alert(1)>'

  test('no <img> is created from Related/Links content and the raw text survives', () => {
    const node = basePayload({
      links: { out: [{ id: 'f', title: imgPayload, type: imgPayload, edge_type: 'links-to' }], in: [] },
    })
    const related: RelatedNode[] = [{ id: 'd', title: imgPayload, type: imgPayload, graph_score: 0.3, distance: 1 }]

    const el = document.createElement('div')
    renderRails(el, node, related, () => {})

    expect(el.querySelector('img')).toBeNull()
    expect(el.querySelector('.rail-entry[data-id="d"] .rail-entry-title')?.textContent).toBe(imgPayload)
    expect(el.querySelector('.rail-entry[data-id="f"] .rail-entry-title')?.textContent).toBe(imgPayload)
  })

  test('never executes, even when force-parsed by a live document', () => {
    // A <script> tag is the reliable execution proof under jsdom (it runs on
    // parse regardless of resource loading) — same mechanism reader.test.ts
    // and search.test.ts's equivalent checks use.
    const scriptPayload = '<script>window.__relatedTestExecuted = true</script>'
    const node = basePayload({
      links: { out: [{ id: 'f', title: scriptPayload, type: scriptPayload, edge_type: 'links-to' }], in: [] },
    })
    const related: RelatedNode[] = [
      { id: 'd', title: scriptPayload, type: scriptPayload, graph_score: 0.3, distance: 1 },
    ]

    const el = document.createElement('div')
    renderRails(el, node, related, () => {})

    // Positive control: confirm this harness can detect execution at all via
    // the open/write/close cycle.
    const control = new JSDOM('<!doctype html><html><body></body></html>', { runScripts: 'dangerously' })
    control.window.document.open()
    control.window.document.write('<script>window.__controlExecuted = true</script>')
    control.window.document.close()
    expect((control.window as unknown as { __controlExecuted?: boolean }).__controlExecuted).toBe(true)

    // The real check: force-parse the rails' actual serialized output the
    // same way. If a title/type had been injected via innerHTML instead of
    // textContent, this would fire __relatedTestExecuted.
    const sandbox = new JSDOM('<!doctype html><html><body></body></html>', { runScripts: 'dangerously' })
    sandbox.window.document.open()
    sandbox.window.document.write(`<body>${el.innerHTML}</body>`)
    sandbox.window.document.close()
    expect(
      (sandbox.window as unknown as { __relatedTestExecuted?: boolean }).__relatedTestExecuted,
    ).toBeUndefined()
  })
})
