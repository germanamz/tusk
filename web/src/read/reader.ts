// Renders one NodeReadPayload into the reader pane: a small header (type
// stamp, title, a compact properties table) followed by the rendered
// markdown body.
//
// Title escaping: node.title comes straight from vault content, which in a
// synced vault is attacker-influenced (spec §6 / plan-corrections.md). This
// header is built entirely with createElement/textContent rather than an
// innerHTML template string, so there is no HTML-injection surface to
// escape in the first place — textContent always inserts a single text
// node, never parsed as markup, even if the resulting innerHTML is later
// re-parsed elsewhere (browsers escape on serialization). renderMarkdown
// (render.ts) is what sanitizes the *body*; this file owns the header, and
// keeping it DOM-built is the "skip the problem" option the task brief
// calls out explicitly, rather than hand-rolling a second escaper.
import { renderMarkdown, hydrate, type RenderContext } from './render'
import { renderRails } from './related'
import { fetchRelated, type NodeReadPayload, type RelatedNode, type RelatedOptions } from './api'

// Keep the properties header modest (a glance, not a property editor) —
// the brief is explicit that a full editor is out of scope.
const PROPERTY_DISPLAY_LIMIT = 12

function formatPropertyValue(value: unknown): string {
  if (value === null || value === undefined) return '—'
  if (Array.isArray(value)) return value.map((item) => String(item)).join(', ')
  if (typeof value === 'object') return JSON.stringify(value)
  return String(value)
}

function renderProperties(properties: Record<string, unknown>): HTMLElement | null {
  const keys = Object.keys(properties)
  if (keys.length === 0) return null

  const dl = document.createElement('dl')
  dl.className = 'node-properties'

  for (const key of keys.slice(0, PROPERTY_DISPLAY_LIMIT)) {
    const dt = document.createElement('dt')
    dt.textContent = key

    const dd = document.createElement('dd')
    dd.textContent = formatPropertyValue(properties[key])

    dl.append(dt, dd)
  }

  return dl
}

function nodeDirOf(path: string): string {
  return path.includes('/') ? path.slice(0, path.lastIndexOf('/')) : ''
}

// renderReader paints one node's header + body into `el`. When a rails host
// element and an onSelect callback are also supplied, it fetches this node's
// graph-walk neighbors (fetchRelated) after the body renders and hands them,
// plus the payload's own links, to renderRails — the right rail always
// reflects the node currently open in the reader. Both are omitted by
// existing reader.test.ts call sites, which only care about the body; rails
// wiring is covered separately by related.test.ts.
export async function renderReader(
  el: HTMLElement,
  node: NodeReadPayload,
  railsEl?: HTMLElement,
  onSelect?: (id: string) => void,
  relatedOptions: RelatedOptions = {},
): Promise<void> {
  const ctx: RenderContext = { nodeDir: nodeDirOf(node.path), wikilinks: node.wikilinks }

  el.innerHTML = ''

  const header = document.createElement('header')
  header.className = 'node-head'

  const typeStamp = document.createElement('span')
  typeStamp.className = 'node-type-stamp'
  typeStamp.textContent = node.type

  const title = document.createElement('h2')
  title.className = 'node-title'
  title.textContent = node.title || node.id

  header.append(typeStamp, title)

  const properties = renderProperties(node.properties)
  if (properties) header.appendChild(properties)

  const article = document.createElement('article')
  article.className = 'node-body'
  article.innerHTML = renderMarkdown(node.markdown, ctx)

  el.append(header, article)

  await hydrate(article, ctx)

  if (railsEl && onSelect) {
    await renderNodeRails(railsEl, node, onSelect, relatedOptions)
  }
}

// renderNodeRails fetches this node's graph-walk neighbors and paints both
// rail sections. A fetchRelated failure degrades to an empty Related section
// rather than losing the whole reader pane — the body above has already
// rendered successfully by the time this runs.
async function renderNodeRails(
  railsEl: HTMLElement,
  node: NodeReadPayload,
  onSelect: (id: string) => void,
  relatedOptions: RelatedOptions,
): Promise<void> {
  let related: RelatedNode[] = []

  try {
    related = (await fetchRelated(node.id, relatedOptions)).related
  } catch {
    related = []
  }

  renderRails(railsEl, node, related, onSelect)
}
