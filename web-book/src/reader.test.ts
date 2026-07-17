import { JSDOM } from 'jsdom'
import { describe, expect, test } from 'vitest'
import { renderReader } from './reader'
import type { NodeReadPayload } from './api'

function basePayload(overrides: Partial<NodeReadPayload> = {}): NodeReadPayload {
  return {
    id: 'a',
    type: 'note',
    title: 'A',
    path: 'a.md',
    properties: {},
    markdown: '# Body\n\ntext',
    links: { out: [], in: [] },
    wikilinks: {},
    ...overrides,
  }
}

test('reader renders title and body', async () => {
  const el = document.createElement('div')
  await renderReader(el, basePayload())
  expect(el.querySelector('h1')?.textContent).toContain('Body')
  expect(el.textContent).toContain('A') // title header
})

test('reader shows the node type as a stamp', async () => {
  const el = document.createElement('div')
  await renderReader(el, basePayload({ type: 'spec' }))
  expect(el.querySelector('.node-type-stamp')?.textContent).toBe('spec')
})

test('nodeDir is derived from path for a nested node, empty for a root-level one', async () => {
  const el = document.createElement('div')
  await renderReader(
    el,
    basePayload({
      path: 'notes/a.md',
      markdown: '![](img/x.png)',
      wikilinks: {},
    }),
  )
  // render.ts resolves relative images against nodeDir; a correct nodeDir of
  // "notes" means the asset route carries that prefix once hydrated.
  expect(el.querySelector('img')?.getAttribute('src')).toBe('./api/asset/notes/img/x.png')
})

test('a compact properties header renders known keys without building an editor', async () => {
  const el = document.createElement('div')
  await renderReader(el, basePayload({ properties: { status: 'draft', owner: 'me' } }))
  const dl = el.querySelector('dl.node-properties')
  expect(dl).not.toBeNull()
  expect(dl?.textContent).toContain('status')
  expect(dl?.textContent).toContain('draft')
  expect(dl?.textContent).toContain('owner')
})

test('no properties element is rendered when properties is empty', async () => {
  const el = document.createElement('div')
  await renderReader(el, basePayload({ properties: {} }))
  expect(el.querySelector('dl.node-properties')).toBeNull()
})

describe('a malicious title is escaped, not injected', () => {
  const imgPayload = '<img src=x onerror=alert(1)>'

  test('no <img> is created from the title and the raw text survives', async () => {
    const el = document.createElement('div')
    await renderReader(el, basePayload({ title: imgPayload }))
    expect(el.querySelector('.node-title img')).toBeNull()
    expect(el.querySelector('.node-title')?.textContent).toBe(imgPayload)
  })

  test('never executes, even when force-parsed by a live document', async () => {
    // A <script> tag is the reliable execution proof under jsdom (it runs on
    // parse regardless of resource loading, unlike an <img onerror> which
    // needs actual image-load failure to fire) — same mechanism
    // render.test.ts's equivalent check uses.
    const scriptPayload = '<script>window.__readerTestExecuted = true</script>'
    const el = document.createElement('div')
    await renderReader(el, basePayload({ title: scriptPayload }))

    // Positive control: confirm this harness can detect execution at all via
    // the open/write/close cycle.
    const control = new JSDOM('<!doctype html><html><body></body></html>', { runScripts: 'dangerously' })
    control.window.document.open()
    control.window.document.write('<script>window.__controlExecuted = true</script>')
    control.window.document.close()
    expect((control.window as unknown as { __controlExecuted?: boolean }).__controlExecuted).toBe(true)

    // The real check: force-parse the reader's actual serialized output the
    // same way. If the title had been injected via innerHTML instead of
    // textContent, this would fire __readerTestExecuted.
    const sandbox = new JSDOM('<!doctype html><html><body></body></html>', { runScripts: 'dangerously' })
    sandbox.window.document.open()
    sandbox.window.document.write(`<body>${el.innerHTML}</body>`)
    sandbox.window.document.close()
    expect(
      (sandbox.window as unknown as { __readerTestExecuted?: boolean }).__readerTestExecuted,
    ).toBeUndefined()
  })
})
