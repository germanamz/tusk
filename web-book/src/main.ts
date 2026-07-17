// Entry point: the three-pane reading shell (header / Contents / Reader /
// rails) plus hash routing on `#/node/<id>`.
import './styles.css'
import 'katex/dist/katex.min.css'
import { fetchIndex, fetchNode, SearchUnavailableError, type IndexResponse, type RelatedOptions } from './api'
import { encodeId } from './encode'
import { renderContents } from './contents'
import { renderReader } from './reader'
import { renderResults, renderSearchBanner, runSearch, type SearchOptions } from './search'

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

function renderShell(): Shell {
  document.body.innerHTML = `
    <div class="shell">
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
    </div>
  `

  return {
    contents: document.getElementById('contents') as HTMLElement,
    reader: document.getElementById('reader') as HTMLElement,
    rails: document.getElementById('rails') as HTMLElement,
    searchForm: document.getElementById('search-form') as HTMLFormElement,
    searchInput: document.querySelector('.search-input') as HTMLInputElement,
    expandToggle: document.getElementById('search-expand-toggle') as HTMLInputElement,
    expandFields: document.getElementById('search-expand-fields') as HTMLElement,
    hopsInput: document.querySelector('input[name="hops"]') as HTMLInputElement,
    weightInput: document.querySelector('input[name="weight"]') as HTMLInputElement,
  }
}

function showError(reader: HTMLElement, id: string, err: unknown): void {
  reader.innerHTML = ''
  const message = document.createElement('p')
  message.className = 'reader-error'
  message.textContent = `Could not load "${id}": ${err instanceof Error ? err.message : String(err)}`
  reader.appendChild(message)
}

async function showNode(
  reader: HTMLElement,
  rails: HTMLElement,
  id: string,
  onSelect: (id: string) => void,
  relatedOptions: RelatedOptions,
): Promise<void> {
  try {
    const node = await fetchNode(id)
    await renderReader(reader, node, rails, onSelect, relatedOptions)
  } catch (err) {
    showError(reader, id, err)
  }
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

// boot wires the whole page together: paint the shell, load the index,
// render Contents, wire the search form's Results-mode swap, and route on
// the current (and every future) hash. It is exported, and its promise is
// exposed as `ready` below, purely so tests can await the initial async
// chain deterministically instead of polling.
export async function boot(): Promise<void> {
  const { contents, reader, rails, searchForm, expandToggle, expandFields, hopsInput, weightInput } =
    renderShell()

  let index: IndexResponse

  try {
    index = await fetchIndex()
  } catch (err) {
    contents.textContent = `Could not load the vault index: ${err instanceof Error ? err.message : String(err)}`
    return
  }

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

  // showContents is the "Contents" affordance's target: it repaints the left
  // pane back to the browse list, discarding whatever Results-mode state
  // (a result list or a banner) was there before. Search results are
  // point-in-time (spec) — there is nothing to preserve or refresh here.
  function showContents(): void {
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
    if (id) await showNode(reader, rails, id, onSelect, currentRelatedOptions())
  }

  window.addEventListener('hashchange', () => {
    void route()
  })

  await route()
}

export const ready: Promise<void> = boot()
