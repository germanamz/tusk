import { JSDOM } from 'jsdom'
import katex from 'katex'
import DOMPurify from 'dompurify'
import { beforeAll, describe, expect, test } from 'vitest'
import { hydrate, renderMarkdown, type RenderContext } from './render'

// hydrate now pulls the theme controller (theme.ts) in lazily, alongside
// mermaid, to color diagrams to the active theme — and theme.ts reads
// window.matchMedia at module load, which jsdom does not implement. Install a
// minimal stub before the mermaid hydrate test triggers that dynamic import.
beforeAll(() => {
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
})

const ctx: RenderContext = {
  nodeDir: '',
  wikilinks: {
    b: { id: 'b', title: 'B', exists: true },
    Ghost: { id: '', title: '', exists: false },
  },
}

test('renders heading + inline math', () => {
  const html = renderMarkdown('# Hi\n\n$x^2$', ctx)
  expect(html).toContain('<h1>')
  expect(html).toContain('katex')
})

test('renders block math', () => {
  const html = renderMarkdown('$$\\int_0^1 x^2\\,dx$$', ctx)
  expect(html).toContain('katex')
})

describe('mermaid fences are deferred, not executed at render time', () => {
  test('fence becomes <pre class="mermaid">', () => {
    const html = renderMarkdown('```mermaid\ngraph TD;A-->B\n```', ctx)
    expect(html).toContain('class="mermaid"')
  })

  test('the diagram source is left as escaped text, not run through mermaid', () => {
    const html = renderMarkdown('```mermaid\ngraph TD;A-->B\n```', ctx)
    // renderMarkdown never imports mermaid at all; if it had run, the fence's
    // raw "graph TD;A-->B" text would have been replaced by an <svg>.
    expect(html).toContain('graph TD;A--&gt;B')
    expect(html).not.toContain('<svg')
  })
})

describe('wikilinks', () => {
  test('a resolved target becomes a node link', () => {
    const html = renderMarkdown('see [[b]]', ctx)
    expect(html).toContain('href="#/node/b"')
  })

  test('an unresolved target becomes a dead-link span', () => {
    const html = renderMarkdown('see [[Ghost]]', ctx)
    expect(html).toContain('wikilink-dead')
  })

  test('an alias label wins over the resolved node title', () => {
    const html = renderMarkdown('see [[b|Custom]]', ctx)
    expect(html).toContain('Custom')
    expect(html).not.toContain('>B<')
  })

  test('an id needing encoding (spaces, #) produces a correctly encoded href', () => {
    const withFragment: RenderContext = {
      nodeDir: '',
      wikilinks: { 'my note#S1': { id: 'my note', title: 'My Note', exists: true } },
    }
    const html = renderMarkdown('[[my note#S1]]', withFragment)
    expect(html).toContain(`href="#/node/${encodeURIComponent('my note')}"`)
  })

  test('a wikilink inside a fenced code block is left untouched', () => {
    // internal/node/wikilinks.go's ExtractWikilinks strips fenced code before
    // matching, so the server's `wikilinks` map never gets an entry for this
    // target — the client-side rewriter must agree it isn't a "real" link.
    const html = renderMarkdown('```\nsee [[b]] here\n```', ctx)
    expect(html).not.toContain('href="#/node/b"')
    expect(html).not.toContain('wikilink-dead')
    expect(html).toContain('[[b]]')
  })

  test('a wikilink inside an inline code span is left untouched', () => {
    const html = renderMarkdown('type `[[b]]` to link', ctx)
    expect(html).not.toContain('href="#/node/b"')
    expect(html).toContain('[[b]]')
  })
})

test('task list items render as disabled checkboxes', () => {
  const html = renderMarkdown('- [ ] todo\n- [x] done\n', ctx)
  // DOMPurify's sanitize pass re-serializes boolean attributes in their
  // explicit empty-string form (disabled="" rather than bare `disabled`) —
  // functionally identical in the DOM, so assert on the parsed form.
  expect(html).toContain('<input type="checkbox" disabled="">')
  expect(html).toContain('<input type="checkbox" disabled="" checked="">')
  expect(html).not.toContain('[ ] todo')
  expect(html).not.toContain('[x] done')
})

describe('sanitization', () => {
  test('a script tag is stripped from the output', () => {
    const html = renderMarkdown('<script>window.__renderTestExecuted = true</script>ok', ctx)
    expect(html).not.toContain('<script>')
  })

  test('a stripped script never executes, even when force-parsed by a live document', () => {
    const dirty = renderMarkdown('<script>window.__renderTestExecuted = true</script>ok', ctx)

    // Positive control: confirm this harness *can* detect execution at all —
    // a real <script>, force-parsed the same way, does run in this sandbox.
    const control = new JSDOM('<!doctype html><html><body></body></html>', { runScripts: 'dangerously' })
    control.window.document.open()
    control.window.document.write('<script>window.__controlExecuted = true</script>')
    control.window.document.close()
    expect((control.window as unknown as { __controlExecuted?: boolean }).__controlExecuted).toBe(true)

    // The real check: DOMPurify's sanitized output, force-parsed the same
    // way. Assigning to innerHTML would never execute a <script> in ANY
    // browser regardless of sanitization (the HTML spec marks fragment-parsed
    // scripts inert), so that alone would not prove anything — document.write
    // during an open/write/close cycle is the one path that behaves like a
    // real page load and actually runs embedded scripts.
    const sandbox = new JSDOM('<!doctype html><html><body></body></html>', { runScripts: 'dangerously' })
    sandbox.window.document.open()
    sandbox.window.document.write(dirty)
    sandbox.window.document.close()
    expect((sandbox.window as unknown as { __renderTestExecuted?: boolean }).__renderTestExecuted).toBeUndefined()
  })
})

test("DOMPurify's default allowlist plus {semantics, annotation} preserves KaTeX output byte-for-byte", () => {
  // renderMarkdown's ADD_TAGS is deliberately just ['semantics', 'annotation']
  // (see render.ts) rather than the larger list a first draft might reach
  // for. Verify that choice directly against katex.renderToString, across
  // inline, block, and a stretchy-delimiter case (\sqrt forces an inline SVG
  // <path>) rather than trusting that DOMPurify's defaults cover it.
  const cases: Array<[string, boolean]> = [
    ['x^2', false],
    ['\\int_0^1 x^2\\,dx', true],
    ['\\sqrt{x+1}', false],
  ]

  for (const [expr, displayMode] of cases) {
    const raw = katex.renderToString(expr, { throwOnError: false, trust: false, displayMode })
    const sanitized = DOMPurify.sanitize(raw, { ADD_TAGS: ['semantics', 'annotation'] })
    // DOMPurify's serializer normalizes self-closing tags (`<path .../>` ->
    // `<path ...></path>`); strip that cosmetic difference before comparing.
    const normalize = (value: string) => value.replace(/<(path|br|hr)([^>]*?)\/>/g, '<$1$2></$1>')
    expect(normalize(sanitized)).toBe(normalize(raw))
  }
})

describe('hydrate', () => {
  test('rewrites a relative image src to the asset route', async () => {
    const relCtx: RenderContext = { nodeDir: 'notes', wikilinks: {} }
    const el = document.createElement('div')
    el.innerHTML = renderMarkdown('![](img/x.png)', relCtx)
    await hydrate(el, relCtx)
    expect(el.querySelector('img')?.getAttribute('src')).toBe('/api/read/asset/notes/img/x.png')
  })

  test('converts a remote image to a click-through link', async () => {
    const rootCtx: RenderContext = { nodeDir: '', wikilinks: {} }
    const el = document.createElement('div')
    el.innerHTML = renderMarkdown('![pic](https://ex.com/a.png)', rootCtx)
    await hydrate(el, rootCtx)
    expect(el.querySelector('img')).toBeNull()
    const link = el.querySelector('a.remote-image')
    expect(link?.getAttribute('href')).toBe('https://ex.com/a.png')
    expect(link?.getAttribute('target')).toBe('_blank')
  })

  test('leaves a data: image src alone', async () => {
    const dataCtx: RenderContext = { nodeDir: 'notes', wikilinks: {} }
    const el = document.createElement('div')
    el.innerHTML = renderMarkdown('![](data:image/png;base64,AAAA)', dataCtx)
    await hydrate(el, dataCtx)
    expect(el.querySelector('img')?.getAttribute('src')).toBe('data:image/png;base64,AAAA')
  })

  test('runs mermaid on a deferred fence and replaces it with an SVG', async () => {
    const el = document.createElement('div')
    el.innerHTML = renderMarkdown('```mermaid\ngraph TD;A-->B\n```', ctx)
    expect(el.querySelector('pre.mermaid')).not.toBeNull()
    await hydrate(el, ctx)
    expect(el.querySelector('pre.mermaid svg')).not.toBeNull()
  })
})
