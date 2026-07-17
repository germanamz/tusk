// The client-side markdown -> HTML pipeline for tusk book's reader. The
// server sends raw markdown (frontmatter already stripped, NodeReadPayload.markdown);
// everything from here down — math, mermaid, wikilinks, sanitization — is the
// browser's job, because KaTeX and mermaid only exist in JS. Pipeline order
// (spec §5) is load-bearing:
//
//   1. markdown-it (CommonMark + tables + task lists)
//   2. math ($…$ / $$…$$ -> KaTeX)
//   3. mermaid fences deferred to <pre class="mermaid">, not executed here
//   4. images resolved to ./api/asset/... or turned into click-through links
//   5. wikilinks ([[id]] / [[id|alias]]) resolved against the payload's map
//   6. DOMPurify sanitizes the assembled HTML before mermaid ever runs
//
// Steps 1, 2, 5 and 6 happen in renderMarkdown(); steps 3 (the actual mermaid
// run) and 4 happen in hydrate(), after the sanitized HTML is already in the
// DOM — mermaid's SVG output never passes through DOMPurify because it is
// inserted directly by mermaid itself under securityLevel: 'strict', not
// parsed back out of renderMarkdown's return value.
import MarkdownIt from 'markdown-it'
import texmath from 'markdown-it-texmath'
import katex from 'katex'
import DOMPurify from 'dompurify'
import { encodeId } from './encode'
import { applyDiagramZoom } from './diagramzoom'
import type { WikilinkTarget } from './api'

export type RenderContext = {
  // path.slice(0, path.lastIndexOf('/')) of the node's own path; '' for a
  // top-level file. Vault-relative image srcs resolve against this.
  nodeDir: string
  // NodeReadPayload.wikilinks: keyed on the RAW wikilink target text,
  // fragments retained (e.g. "c#S1"), never null (the server always sends
  // {} at minimum). A fragment target resolves to its file by ruling, so the
  // map key can differ from `.id`; route on `.id`/`.exists`, never the key.
  wikilinks: Record<string, WikilinkTarget>
}

// One markdown-it instance, module-level: constructing it (plus registering
// texmath/katex and the fence + task-list + wikilink rules) is one-time setup,
// not per-render work. Per-render data (the wikilinks map) rides through
// markdown-it's own `env` parameter instead of a closure, so this singleton
// never goes stale across nodes with different wikilink maps.
const md: MarkdownIt = new MarkdownIt({ html: true, linkify: true, breaks: false })

// Math: $…$ inline, $$…$$ block, both -> KaTeX. throwOnError: false means a
// malformed expression renders as flagged source text instead of throwing;
// trust: false keeps KaTeX from honoring \includegraphics/\href-style
// commands that could otherwise reach arbitrary URLs from vault content.
md.use(texmath, {
  engine: katex,
  delimiters: 'dollars',
  katexOptions: { throwOnError: false, trust: false },
})

// ```mermaid fences are deferred: render them as an inert <pre class="mermaid">
// carrying the raw diagram source, never executed here. hydrate() finds these
// after the sanitized HTML is in the DOM and calls mermaid.run() on them.
const defaultFenceRule = md.renderer.rules.fence!.bind(md.renderer.rules)
md.renderer.rules.fence = (tokens, idx, options, env, self) => {
  const token = tokens[idx]

  if (token.info.trim().toLowerCase() === 'mermaid') {
    return `<pre class="mermaid">${md.utils.escapeHtml(token.content)}</pre>`
  }

  return defaultFenceRule(tokens, idx, options, env, self)
}

// Task lists: markdown-it's CommonMark core has no notion of `- [ ] item` —
// GFM task-list checkboxes are a sugar on top that markdown-it does not ship,
// and this project has no markdown-it-task-lists dependency, so it is
// implemented directly as a small core rule (runs after 'inline', appended to
// the end of the ruler, so token trees are already built). A tight list item
// is `list_item_open, paragraph_open(hidden), inline, paragraph_close(hidden),
// list_item_close`; the inline token's first child is the leading text run
// carrying the checkbox syntax.
const TASK_MARKER = /^\[([ xX])\]\s+/

md.core.ruler.push('task_lists', (state) => {
  const tokens = state.tokens

  for (let i = 0; i < tokens.length; i++) {
    if (tokens[i].type !== 'list_item_open') continue

    const inline = tokens[i + 2]
    const firstChild = inline?.type === 'inline' ? inline.children?.[0] : undefined

    if (!inline || firstChild?.type !== 'text') continue

    const match = TASK_MARKER.exec(firstChild.content)
    if (!match) continue

    const checked = match[1].toLowerCase() === 'x'
    firstChild.content = firstChild.content.slice(match[0].length)

    tokens[i].attrJoin('class', 'task-list-item')

    const checkbox = new state.Token('html_inline', '', 0)
    checkbox.content = `<input type="checkbox" disabled${checked ? ' checked' : ''}> `
    inline.children!.unshift(checkbox)
  }
})

// Wikilinks: [[id]] / [[id|alias]] -> <a href="#/node/<id>">, or a
// `.wikilink-dead` span when unresolved. Implemented as a markdown-it inline
// rule (not a pre-parse string rewrite) so it naturally respects markdown-it's
// own tokenization: a wikilink inside a fenced code block is never seen at
// all (fence content is a single raw block token, never inline-parsed), and
// one inside an inline code span is skipped because the 'backticks' rule
// claims that span first. This mirrors internal/node's ExtractWikilinks,
// which strips fenced code before matching for the same reason — the two
// sides agree on what counts as a "real" wikilink without coordinating on it
// directly.
//
// The inner-content class `[^[\]]+` matches internal/node/wikilinks.go's
// wikilinkPattern exactly: neither `[` nor `]` may appear inside, so a
// wikilink can never span brackets or run past its closing `]]`.
const WIKILINK = /^\[\[([^[\]]+)\]\]/

interface WikilinkMeta {
  target: string
  alias: string | null
}

function splitAlias(inner: string): [target: string, alias: string | null] {
  const bar = inner.indexOf('|')
  if (bar < 0) return [inner.trim(), null]
  return [inner.slice(0, bar).trim(), inner.slice(bar + 1).trim()]
}

md.inline.ruler.before('link', 'wikilink', (state, silent) => {
  if (state.src.charCodeAt(state.pos) !== 0x5b || state.src.charCodeAt(state.pos + 1) !== 0x5b) {
    return false
  }

  const match = WIKILINK.exec(state.src.slice(state.pos))
  if (!match) return false

  if (!silent) {
    const [target, alias] = splitAlias(match[1])
    const token = state.push('wikilink', '', 0)
    const meta: WikilinkMeta = { target, alias }
    token.meta = meta
  }

  state.pos += match[0].length
  return true
})

md.renderer.rules.wikilink = (tokens, idx, _options, env) => {
  const { target, alias } = tokens[idx].meta as WikilinkMeta
  const ctx = env as RenderContext
  const resolved = ctx.wikilinks[target]
  const label = alias || resolved?.title || target

  if (resolved?.exists) {
    return `<a href="#/node/${encodeId(resolved.id)}">${md.utils.escapeHtml(label)}</a>`
  }

  return `<span class="wikilink-dead">${md.utils.escapeHtml(label)}</span>`
}

// KaTeX's HTML output needs exactly two tags beyond DOMPurify's default
// allowlist: `semantics` and `annotation`, both part of its (visually hidden,
// screen-reader/copy-paste facing) MathML branch. Everything else KaTeX
// emits — math/mrow/mi/mo/mn/msup/msqrt/mfrac/..., the katex-html spans, and
// even the <svg><path> KaTeX uses for stretchy delimiters like \sqrt — is
// already covered by DOMPurify's default HTML+SVG+MathML profile (checked by
// rendering katex.renderToString output for x^2, a block integral, and
// \sqrt{x+1} through DOMPurify with only this ADD_TAGS and diffing against
// the raw KaTeX output — see render.test.ts). Do not widen this further
// without re-running that check; a wider allowlist is a wider attack surface
// against vault content that may not be locally authored.
const SANITIZE_OPTIONS = { ADD_TAGS: ['semantics', 'annotation'] }

export function renderMarkdown(source: string, ctx: RenderContext): string {
  const raw = md.render(source, ctx)
  return DOMPurify.sanitize(raw, SANITIZE_OPTIONS)
}

// resolveAssetPath resolves a vault-relative image src against the node's
// directory, collapsing "." and ".." segments, then routes it through the
// same-origin asset endpoint. encodeId is reused here (not just for node
// ids) because asset paths share the same shape: '/'-separated segments that
// may contain spaces or other characters needing escaping, while the '/'
// separators themselves must survive to match the Go route's {path...}
// wildcard.
function resolveAssetPath(nodeDir: string, src: string): string {
  const joined = nodeDir ? `${nodeDir}/${src}` : src
  const parts: string[] = []

  for (const segment of joined.split('/')) {
    if (segment === '' || segment === '.') continue
    if (segment === '..') {
      parts.pop()
      continue
    }
    parts.push(segment)
  }

  return `./api/asset/${encodeId(parts.join('/'))}`
}

const REMOTE_IMAGE = /^https?:\/\//

// hydrate runs after renderMarkdown's sanitized HTML is already inserted into
// the DOM. It rewrites image srcs and boots mermaid — both are deliberately
// kept out of renderMarkdown: image rewriting needs live <img> elements to
// mutate, and mermaid.run() must never fire during the sanitize-then-insert
// step (spec §5.3), only once its <pre class="mermaid"> host is trusted DOM.
export async function hydrate(container: HTMLElement, ctx: RenderContext): Promise<void> {
  container.querySelectorAll('img').forEach((img) => {
    const src = img.getAttribute('src') || ''

    if (REMOTE_IMAGE.test(src)) {
      // Spec §6: never load remote images silently from vault content — a
      // vault may be synced from elsewhere or agent-authored, and a bare
      // remote <img> would leak what's being read to a third-party host on
      // every view. img-src 'self' data: would block the request anyway;
      // this replaces it with an explicit, inert click-through link instead
      // of a broken image icon.
      const link = document.createElement('a')
      link.className = 'remote-image'
      link.href = src
      link.target = '_blank'
      link.rel = 'noopener noreferrer'
      link.textContent = img.getAttribute('alt') || src
      img.replaceWith(link)
    } else if (!src.startsWith('data:')) {
      img.setAttribute('src', resolveAssetPath(ctx.nodeDir, src))
    }
  })

  // Tear down zoom instances wired for the previously-rendered node before this
  // one's DOM (already swapped in by renderReader) gets its own — so Panzoom
  // instances never accumulate across navigations.
  teardownPreviousDiagramZooms()

  const mermaidBlocks = container.querySelectorAll<HTMLElement>('pre.mermaid')

  if (mermaidBlocks.length > 0) {
    const mermaid = (await import('mermaid')).default
    mermaid.initialize({ startOnLoad: false, securityLevel: 'strict', theme: 'neutral' })

    try {
      await mermaid.run({ nodes: Array.from(mermaidBlocks) })
      // Each diagram now holds an <svg>; make it pinch/pan/zoom in place.
      mermaidBlocks.forEach((block) => {
        activeZoomTeardowns.push(applyDiagramZoom(block))
      })
    } catch {
      // Leave the raw diagram source visible on a parse error rather than
      // throwing out of hydrate() and losing the rest of the rendered node.
    }
  }
}

// activeZoomTeardowns holds the teardown for every diagram made zoomable in the
// last hydrate() pass, so the next pass can destroy them — Panzoom instances must
// not accumulate across navigations. Rapid re-navigation can briefly overlap two
// hydrate() calls; whichever renderReader swapped the DOM in last wins, and a
// stale pass's teardown is swept by the following navigation.
let activeZoomTeardowns: Array<() => void> = []

function teardownPreviousDiagramZooms(): void {
  activeZoomTeardowns.forEach((teardown) => teardown())
  activeZoomTeardowns = []
}
