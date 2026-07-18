// The right rail: renders after a node loads (wired from reader.ts) into two
// sections — "Related (graph)" (RelatedNodes from fetchRelated, ranked by
// graph-walk distance) and "Links" (this node's direct out/in LinkRefs,
// already rolled up to file level by bookview — plan-corrections.md's
// sub-unit-far-end ruling). Every entry is a real anchor (so it carries a
// correct, inspectable `href` and is right-click/keyboard navigable) but the
// actual routing goes through `onSelect` (preventDefault + onSelect(id)),
// matching the single navigation path renderContents/renderResults already
// use elsewhere in this app.
//
// Title/type escaping: RelatedNode.title, LinkRef.title, and .type all come
// straight from vault content, attacker-influenced in a synced vault (spec
// §6 / plan-corrections.md — the same boundary reader.ts's node head and
// search.ts's result list cross). This file is built entirely with
// createElement/textContent, never an innerHTML template string, so there is
// no markup-injection surface to escape in the first place. See
// related.test.ts for the non-execution proof, following reader.test.ts's
// force-parse-through-jsdom pattern.
import { encodeId } from './encode'
import type { LinkRef, NodeReadPayload, RelatedNode } from './api'

// nodeHref mirrors main.ts's buildNodeHash (`#/node/${encodeId(id)}`)
// without importing main.ts — main.ts is what imports renderRails, so
// importing back would make it a circular module dependency. render.ts's
// wikilink hrefs already duplicate this same literal for the identical
// reason (see render.ts's `wikilink` renderer rule).
function nodeHref(id: string): string {
  return `#/node/${encodeId(id)}`
}

function badge(className: string, text: string): HTMLSpanElement {
  const span = document.createElement('span')
  span.className = className
  span.textContent = text
  return span
}

// railEntry builds one rail row: a real anchor (correct href, so it is
// inspectable and survives e.g. a middle-click) whose click is intercepted
// to route through `onSelect` instead of a real navigation — the same
// contract every other list in this app uses.
function railEntry(id: string, parts: Array<Node | string>, onSelect: (id: string) => void): HTMLLIElement {
  const li = document.createElement('li')

  const link = document.createElement('a')
  link.className = 'rail-entry'
  link.href = nodeHref(id)
  link.dataset.id = id
  link.append(...parts)

  link.addEventListener('click', (evt) => {
    evt.preventDefault()
    onSelect(id)
  })

  li.appendChild(link)
  return li
}

function railSection(heading: string): HTMLElement {
  const section = document.createElement('section')
  section.className = 'rail-section'

  const h = document.createElement('h3')
  h.className = 'rail-heading'
  h.textContent = heading
  section.appendChild(h)

  return section
}

function railEmpty(text: string): HTMLParagraphElement {
  const empty = document.createElement('p')
  empty.className = 'rail-empty'
  empty.textContent = text
  return empty
}

function formatDistance(distance: number): string {
  return `${distance} hop${distance === 1 ? '' : 's'}`
}

// renderRelatedSection lists graph-walk neighbors. Their ranking is purely
// distance-derived — every distance-1 neighbor scores exactly the configured
// weight and every distance-2 neighbor scores exactly weight²
// (internal/graphexpand/blend_test.go), so it cannot discriminate among
// same-distance neighbors. The score renders as a plain number (honest about
// its precision), not styled to imply a finer ranking than it carries.
function renderRelatedSection(related: RelatedNode[], onSelect: (id: string) => void): HTMLElement {
  const section = railSection('Related (graph)')

  if (related.length === 0) {
    section.appendChild(railEmpty('Nothing related yet.'))
    return section
  }

  const list = document.createElement('ul')
  list.className = 'rail-list'

  for (const node of related) {
    const label = node.title || node.id

    const title = document.createElement('span')
    title.className = 'rail-entry-title'
    title.textContent = label
    title.title = label

    const stamp = badge('rail-entry-stamp', node.type)
    const distance = badge('rail-entry-distance', formatDistance(node.distance))
    // graph_score is NOT omitempty on the wire (unlike Match.graph_score) —
    // it always arrives, including an honest 0. Read it directly; guarding
    // with `node.graph_score || …` would treat a real 0 as if it were absent.
    const score = badge('rail-entry-score', node.graph_score.toFixed(3))

    list.appendChild(railEntry(node.id, [stamp, title, distance, score], onSelect))
  }

  section.appendChild(list)
  return section
}

function renderLinksSection(links: NodeReadPayload['links'], onSelect: (id: string) => void): HTMLElement {
  const section = railSection('Links')

  if (links.out.length === 0 && links.in.length === 0) {
    section.appendChild(railEmpty('Nothing linked yet.'))
    return section
  }

  const list = document.createElement('ul')
  list.className = 'rail-list'

  const appendLink = (link: LinkRef, arrow: string): void => {
    const label = link.title || link.id

    const title = document.createElement('span')
    title.className = 'rail-entry-title'
    title.textContent = label
    title.title = label

    const arrowBadge = badge('rail-entry-arrow', arrow)
    const stamp = badge('rail-entry-stamp', link.type)

    list.appendChild(railEntry(link.id, [arrowBadge, ' ', title, stamp], onSelect))
  }

  for (const link of links.out) appendLink(link, '→')
  for (const link of links.in) appendLink(link, '←')

  section.appendChild(list)
  return section
}

// renderRails paints both rail sections into `el`, replacing whatever was
// there before (the initial "select something from Contents" placeholder, or
// a previous node's rails). Every entry navigates by calling `onSelect(id)`.
export function renderRails(
  el: HTMLElement,
  node: NodeReadPayload,
  related: RelatedNode[],
  onSelect: (id: string) => void,
): void {
  el.innerHTML = ''
  el.appendChild(renderRelatedSection(related, onSelect))
  el.appendChild(renderLinksSection(node.links, onSelect))
}
