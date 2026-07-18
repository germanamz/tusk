// The reading view, as a mountable module for the unified shell's router
// (shell/router.ts): mount(container) paints the three-pane reading shell —
// header / Contents / Reader / rails — into the container it is handed and
// wires hash routing on `#/node/<id>`, search, live-reload SSE, and live
// mermaid theming; unmount() tears all of that back down again. This is the
// former standalone `main.ts` folded onto the mount/unmount contract: the one
// substantive shift is that renderShell targets a provided container rather
// than taking over document.body, so the reader can live inside #viewport
// beside the graph view.
import './styles.css'
import 'katex/dist/katex.min.css'
import { fetchIndex, fetchNode, SearchUnavailableError, type IndexResponse, type NodeReadPayload, type RelatedOptions } from './api'
import { encodeId } from './encode'
import { renderContents } from './contents'
import { renderReader } from './reader'
import { renderResults, renderSearchBanner, runSearch, type SearchOptions } from './search'
import { subscribeChanges } from './stream'
import { teardownActiveDiagramZooms } from './render'
import { onThemeChange } from '../theme/theme'

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
  searchForm: HTMLFormElement
  searchInput: HTMLInputElement
  expandToggle: HTMLInputElement
  expandFields: HTMLElement
  hopsInput: HTMLInputElement
  weightInput: HTMLInputElement
}

// renderShell builds the reading shell inside `container` (never document.body,
// unlike the standalone entry it grew out of): the whole book is wrapped in a
// single `.book-root` element that carries the 3-pane grid, so unmount can drop
// it wholesale and the reader never bleeds layout onto the host page.
function renderShell(container: HTMLElement): Shell {
  const root = document.createElement('div')
  root.className = 'book-root'
  root.innerHTML = `
    <header class="shell-header">
      <div class="brand"><span class="brand-mark">tusk</span><span class="brand-sep">&middot;</span>book</div>
      <form class="search-slot search-form" id="search-form" aria-label="Search the vault">
        <input class="search-input" type="search" name="q" placeholder="Search the vault…" autocomplete="off" />
        <label class="search-toggle">
          <input type="checkbox" name="expand" id="search-expand-toggle" />
          <span>Expand</span>
        </label>
        <span class="search-expand-fields" id="search-expand-fields" hidden>
          <label class="search-field">Hops
            <input class="search-number" type="number" name="hops" min="1" max="2" step="1" placeholder="auto" />
          </label>
          <label class="search-field">Weight
            <input class="search-number" type="number" name="weight" min="0" max="1" step="0.1" placeholder="auto" />
          </label>
        </span>
        <button type="submit" class="search-submit">Search</button>
      </form>
    </header>
    <aside id="contents" class="pane pane-contents" aria-label="Contents"></aside>
    <main id="reader" class="pane pane-reader" aria-label="Reader">
      <p class="reader-empty">Select something from Contents to start reading.</p>
    </main>
    <aside id="rails" class="pane pane-rails" aria-label="Related">
      <p class="rail-empty">Select something from Contents to see related notes and links.</p>
    </aside>
  `

  container.appendChild(root)

  return {
    contents: root.querySelector('#contents') as HTMLElement,
    reader: root.querySelector('#reader') as HTMLElement,
    rails: root.querySelector('#rails') as HTMLElement,
    searchForm: root.querySelector('#search-form') as HTMLFormElement,
    searchInput: root.querySelector('.search-input') as HTMLInputElement,
    expandToggle: root.querySelector('#search-expand-toggle') as HTMLInputElement,
    expandFields: root.querySelector('#search-expand-fields') as HTMLElement,
    hopsInput: root.querySelector('input[name="hops"]') as HTMLInputElement,
    weightInput: root.querySelector('input[name="weight"]') as HTMLInputElement,
  }
}

function showError(reader: HTMLElement, id: string, err: unknown): void {
  reader.innerHTML = ''
  const message = document.createElement('p')
  message.className = 'reader-error'
  message.textContent = `Could not load "${id}": ${err instanceof Error ? err.message : String(err)}`
  reader.appendChild(message)
}

// parseOptionalFormNumber reads one of the hops/weight form fields: a blank
// input means the user never touched it, which must reach runSearch (or
// fetchRelated, for the rails) as `undefined` — never as `0` — so the caller
// (not this function) is the one place that decides what "unset" becomes on
// the wire.
function parseOptionalFormNumber(value: string | FormDataEntryValue | null): number | undefined {
  if (typeof value !== 'string') return undefined
  const trimmed = value.trim()
  if (trimmed === '') return undefined
  const parsed = Number(trimmed)
  return Number.isFinite(parsed) ? parsed : undefined
}

// Teardown handles captured at mount time so unmount can undo exactly what
// mount wired: the SSE subscription, the theme listener, the hashchange
// listener, and the container we appended into. Held at module scope because
// mount and unmount are separate top-level calls, not one closure.
let mountedContainer: HTMLElement | null = null
let unsubscribeChanges: (() => void) | null = null
let unsubscribeTheme: (() => void) | null = null
let hashChangeHandler: (() => void) | null = null

// mount paints the reading view into `container` and wires it live: load the
// index, render Contents, wire the Expand toggle + search form, route on the
// current (and every future) hash, subscribe to the change stream, and follow
// the app theme so mermaid diagrams recolor with it. The returned promise
// resolves once the initial route has painted — the router awaits it, and the
// tests await it for a deterministic initial async chain.
export function mount(container: HTMLElement): Promise<void> {
  mountedContainer = container
  return boot(container)
}

// unmount fully tears the view down: close the SSE EventSource, drop the
// hashchange and theme listeners, destroy any live Panzoom instances the last
// rendered node created (render.ts owns them), and empty the container. Every
// handle was captured in mount; clearing them here makes a later re-mount
// start clean.
export function unmount(): void {
  if (hashChangeHandler) {
    window.removeEventListener('hashchange', hashChangeHandler)
    hashChangeHandler = null
  }

  if (unsubscribeChanges) {
    unsubscribeChanges()
    unsubscribeChanges = null
  }

  if (unsubscribeTheme) {
    unsubscribeTheme()
    unsubscribeTheme = null
  }

  teardownActiveDiagramZooms()

  if (mountedContainer) {
    mountedContainer.replaceChildren()
    mountedContainer = null
  }
}

// boot wires the whole view together: paint the shell into the container, load
// the index, render Contents, wire the search form's Results-mode swap, route
// on the hash, and stand up the live subscriptions. Its promise resolves once
// the initial route has painted.
async function boot(container: HTMLElement): Promise<void> {
  const { contents, reader, rails, searchForm, expandToggle, expandFields, hopsInput, weightInput } =
    renderShell(container)

  let index: IndexResponse

  try {
    index = await fetchIndex()
  } catch (err) {
    contents.textContent = `Could not load the vault index: ${err instanceof Error ? err.message : String(err)}`
    return
  }

  // mode tracks what the left pane currently shows, so a live-reload change
  // event (below) knows whether it's safe to repaint Contents or whether
  // doing so would clobber a point-in-time search result. currentNodeId
  // tracks the node the hash route currently points at (regardless of
  // whether its fetch actually succeeded) so a change event knows whether
  // there is an open node to refetch. currentNode holds the last successfully
  // fetched payload so a theme flip can re-render the open node's diagrams
  // without a fresh network round-trip.
  let mode: 'contents' | 'results' = 'contents'
  let currentNodeId: string | null = null
  let currentNode: NodeReadPayload | null = null

  function onSelect(id: string): void {
    location.hash = buildNodeHash(id)
  }

  // currentRelatedOptions carries the search controls' hops/weight into the
  // Related rail's fetchRelated call, but only when genuinely set — a blank
  // field must reach fetchRelated as `undefined` (omit the query param
  // entirely), never as a literal `0`, or an unset weight would silently
  // zero out every distance-2 graph score (RelatedOptions' own contract,
  // api.ts). This is independent of whether the Expand toggle is currently
  // checked: the rail is not itself a search, it just reuses whatever
  // graph-expansion knobs the user has already dialed in.
  function currentRelatedOptions(): RelatedOptions {
    const opts: RelatedOptions = {}
    const hops = parseOptionalFormNumber(hopsInput.value)
    const weight = parseOptionalFormNumber(weightInput.value)
    if (hops !== undefined) opts.hops = hops
    if (weight !== undefined) opts.weight = weight
    return opts
  }

  async function showNode(id: string): Promise<void> {
    try {
      const node = await fetchNode(id)
      currentNode = node
      await renderReader(reader, node, rails, onSelect, currentRelatedOptions())
    } catch (err) {
      currentNode = null
      showError(reader, id, err)
    }
  }

  // repaintOpenNode re-runs the reader's render path for the currently open
  // node against the cached payload. It exists for the theme listener: mermaid
  // reads the active theme at hydrate time (render.ts), and re-rendering the
  // body re-initializes mermaid with the new theme and redraws its diagrams so
  // they don't read as light-on-light after a flip. The rails are left as they
  // are — they're CSS-variable driven and recolor on their own, no refetch
  // needed for a pure theme change.
  function repaintOpenNode(): void {
    if (currentNode) void renderReader(reader, currentNode)
  }

  // showContents is the "Contents" affordance's target: it repaints the left
  // pane back to the browse list, discarding whatever Results-mode state
  // (a result list or a banner) was there before. Search results are
  // point-in-time (spec) — there is nothing to preserve or refresh here.
  function showContents(): void {
    mode = 'contents'
    renderContents(contents, index, onSelect)
  }

  // resultsBar prefixes Results mode (both a result list and a banner) with
  // the "Contents" affordance that gets back to the browse list.
  function resultsBar(): HTMLElement {
    const bar = document.createElement('div')
    bar.className = 'results-bar'

    const back = document.createElement('button')
    back.type = 'button'
    back.className = 'results-back'
    back.textContent = '← Contents'
    back.addEventListener('click', showContents)

    bar.appendChild(back)
    return bar
  }

  function showResults(resp: Awaited<ReturnType<typeof runSearch>>): void {
    mode = 'results'
    contents.innerHTML = ''
    contents.appendChild(resultsBar())

    const host = document.createElement('div')
    contents.appendChild(host)
    renderResults(host, resp, onSelect)
  }

  // showSearchBanner handles a postSearch failure. A 422
  // (SearchUnavailableError) is the expected "the embedder is off" condition
  // — browse and read keep working, so it gets the calmer 'notice' banner,
  // not an error treatment. Anything else (400 bad request, 503 a real
  // backend failure) is a genuine failure and gets the 'error' variant.
  function showSearchBanner(err: unknown): void {
    mode = 'results'
    contents.innerHTML = ''
    contents.appendChild(resultsBar())

    const host = document.createElement('div')
    contents.appendChild(host)

    if (err instanceof SearchUnavailableError) {
      renderSearchBanner(host, 'Semantic search is unavailable right now — browse Contents instead.', 'notice')
      return
    }

    const message = err instanceof Error ? err.message : String(err)
    renderSearchBanner(host, `Search failed: ${message}`, 'error')
  }

  showContents()

  expandToggle.addEventListener('change', () => {
    expandFields.hidden = !expandToggle.checked
  })

  searchForm.addEventListener('submit', (evt) => {
    evt.preventDefault()

    const data = new FormData(searchForm)
    const q = String(data.get('q') ?? '').trim()
    if (!q) return

    const opts: SearchOptions = {
      expand: data.get('expand') === 'on',
      hops: parseOptionalFormNumber(data.get('hops')),
      weight: parseOptionalFormNumber(data.get('weight')),
    }

    runSearch(q, opts).then(showResults, showSearchBanner)
  })

  async function route(): Promise<void> {
    const id = parseNodeHash(location.hash)
    currentNodeId = id
    if (id) await showNode(id)
  }

  hashChangeHandler = () => {
    void route()
  }
  window.addEventListener('hashchange', hashChangeHandler)

  // markResultsStale is the "vault changed — re-run search" affordance
  // (spec §7): Results mode is point-in-time by design, so a live-reload
  // change event must never silently re-run the search or repaint over the
  // existing result list — it only prompts. Idempotent: a second change
  // event while the banner is already showing is a no-op rather than a pile
  // of duplicate banners.
  function markResultsStale(): void {
    if (contents.querySelector('.results-stale-banner')) return

    const notice = document.createElement('p')
    notice.className = 'results-stale-banner'
    notice.textContent = 'Vault changed — re-run search.'

    const bar = contents.querySelector('.results-bar')
    if (bar) bar.after(notice)
    else contents.prepend(notice)
  }

  // handleChange responds to a live-reload signal from the SSE stream
  // (stream.ts). The underlying index data is always refreshed, regardless
  // of which pane is showing, so that a later "← Contents" repaints with
  // current data rather than whatever snapshot was last fetched. Only the
  // *repaint* is gated on mode: repainting Contents while Results mode is
  // showing would be exactly the "silently re-running or clobbering" spec
  // §7 forbids, hence the stale-banner branch instead, which touches
  // neither the network nor the existing result list. The open node (if
  // any) is refetched unconditionally: that's the reader pane, independent
  // of what the left pane is currently showing.
  async function handleChange(): Promise<void> {
    try {
      index = await fetchIndex()
      if (mode === 'contents') showContents()
    } catch {
      // A transient failure on a live-reload signal shouldn't wipe out
      // whatever Contents/Results state is already on screen.
    }

    if (mode === 'results') markResultsStale()

    if (currentNodeId) {
      await showNode(currentNodeId)
    }
  }

  unsubscribeChanges = subscribeChanges(() => {
    void handleChange()
  })

  // Follow the app theme: mermaid reads the resolved theme when it initializes
  // (render.ts), so on a flip re-render the open node to recolor its diagrams.
  // The unsubscribe is held for unmount.
  unsubscribeTheme = onThemeChange(() => {
    repaintOpenNode()
  })

  await route()
}
