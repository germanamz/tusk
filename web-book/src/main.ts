// Entry point: the three-pane reading shell (header / Contents / Reader /
// rails) plus hash routing on `#/node/<id>`.
import './styles.css'
import 'katex/dist/katex.min.css'
import { fetchIndex, fetchNode, type IndexResponse } from './api'
import { encodeId } from './encode'
import { renderContents } from './contents'
import { renderReader } from './reader'

const NODE_HASH_PREFIX = '#/node/'

// buildNodeHash mirrors render.ts's own `#/node/${encodeId(target.id)}`
// wikilink hrefs, so a wikilink click and a Contents-pane click land on the
// same route shape.
export function buildNodeHash(id: string): string {
  return `${NODE_HASH_PREFIX}${encodeId(id)}`
}

// parseNodeHash is encodeId's inverse: it decodes '/'-separated segments
// independently rather than decoding the whole remainder in one call, so an
// id containing its own literal "/" and "#" (sub-unit ids are
// `<fileID>#<address>`) round-trips instead of being truncated the way a
// naive `location.hash.split('/')[2]` would truncate it.
export function parseNodeHash(hash: string): string | null {
  if (!hash.startsWith(NODE_HASH_PREFIX)) return null

  const rest = hash.slice(NODE_HASH_PREFIX.length)
  if (!rest) return null

  return rest
    .split('/')
    .map((segment) => decodeURIComponent(segment))
    .join('/')
}

interface Shell {
  contents: HTMLElement
  reader: HTMLElement
  rails: HTMLElement
}

function renderShell(): Shell {
  document.body.innerHTML = `
    <div class="shell">
      <header class="shell-header">
        <div class="brand"><span class="brand-mark">tusk</span><span class="brand-sep">&middot;</span>book</div>
        <div class="search-slot">
          <input class="search-input" type="search" placeholder="Search the vault... (coming soon)" disabled />
        </div>
      </header>
      <aside id="contents" class="pane pane-contents" aria-label="Contents"></aside>
      <main id="reader" class="pane pane-reader" aria-label="Reader">
        <p class="reader-empty">Select something from Contents to start reading.</p>
      </main>
      <aside id="rails" class="pane pane-rails" aria-label="Related">
        <section class="rail-section">
          <h3 class="rail-heading">Links</h3>
          <p class="rail-empty">Nothing linked yet.</p>
        </section>
        <section class="rail-section">
          <h3 class="rail-heading">Related</h3>
          <p class="rail-empty">Nothing related yet.</p>
        </section>
      </aside>
    </div>
  `

  return {
    contents: document.getElementById('contents') as HTMLElement,
    reader: document.getElementById('reader') as HTMLElement,
    rails: document.getElementById('rails') as HTMLElement,
  }
}

function showError(reader: HTMLElement, id: string, err: unknown): void {
  reader.innerHTML = ''
  const message = document.createElement('p')
  message.className = 'reader-error'
  message.textContent = `Could not load "${id}": ${err instanceof Error ? err.message : String(err)}`
  reader.appendChild(message)
}

async function showNode(reader: HTMLElement, id: string): Promise<void> {
  try {
    const node = await fetchNode(id)
    await renderReader(reader, node)
  } catch (err) {
    showError(reader, id, err)
  }
}

// boot wires the whole page together: paint the shell, load the index,
// render Contents, and route on the current (and every future) hash. It is
// exported, and its promise is exposed as `ready` below, purely so tests can
// await the initial async chain deterministically instead of polling.
export async function boot(): Promise<void> {
  const { contents, reader } = renderShell()

  let index: IndexResponse

  try {
    index = await fetchIndex()
  } catch (err) {
    contents.textContent = `Could not load the vault index: ${err instanceof Error ? err.message : String(err)}`
    return
  }

  renderContents(contents, index, (id) => {
    location.hash = buildNodeHash(id)
  })

  async function route(): Promise<void> {
    const id = parseNodeHash(location.hash)
    if (id) await showNode(reader, id)
  }

  window.addEventListener('hashchange', () => {
    void route()
  })

  await route()
}

export const ready: Promise<void> = boot()
