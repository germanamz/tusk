// The search box: a semantic query with an optional graph-expansion toggle,
// rendered into whichever pane the caller hands us. This module owns request
// building (runSearch) and result/notice rendering (renderResults,
// renderSearchBanner); main.ts owns the header form, the left-pane
// mode-switch (Contents vs Results), and classifying a postSearch failure
// into a banner kind — this file stays ignorant of both.
//
// Presence rule for hops/weight (the "same trap as everywhere else"
// mentioned in the task brief) — worth spelling out because it resolves
// differently here than in fetchRelated:
//
// GET /api/related's hops/weight are optional QUERY PARAMS, and api.ts's
// fetchRelated expresses "unset" by omitting them from the URL entirely
// (`if (opts.hops !== undefined) params.set(...)`) — the Go handler detects
// absence via `request.URL.Query().Has(name)`.
//
// POST /api/search has no such mechanism available to it: SearchRequest.Hops
// and .Weight are plain, non-pointer Go fields (`Hops int`, `Weight
// float64`, no `omitempty`), so a JSON body cannot distinguish "the key is
// absent" from "the key is 0" the way a query string can distinguish "absent"
// from "present with value 0". The Go adapter
// (internal/bookview/realdeps.go's graphExpansionOverridesFromSearch)
// resolves this by treating 0 ITSELF as the "not specified" sentinel for this
// endpoint specifically: `if req.Hops != 0 { ...override... }` /
// `if req.Weight != 0 { ...override... }`. Fighting that by trying to omit
// the JSON keys instead would just leave the Go zero value in place anyway
// (Go defaults absent int/float64 fields to 0), so it would not even change
// the wire bytes.
//
// runSearch therefore mirrors the Go contract rather than fabricating a
// second presence channel: SearchOptions.hops/weight are `number | undefined`
// (undefined = the field was left blank, or Expand is off), and `undefined`
// is turned into a literal `0` right here.
//
// edge_types gets the opposite treatment. This box exposes no edge-type
// control, so the request must never carry an override for it. Sending `[]`
// would decode on the Go side to a non-nil, zero-length slice — a real
// override meaning "no edge types", which would silently disable
// graph-expansion traversal entirely (SearchRequest.EdgeTypes has no
// zero-value sentinel trick available: nil and [] are genuinely different
// values on the wire, unlike Hops/Weight). So edge_types is simply never set
// on the request object below; api.ts's SearchRequest.edge_types is optional
// for exactly this reason, and JSON.stringify omits an absent key outright.
import { postSearch, type Match, type SearchRequest, type SearchResponse } from './api'

export interface SearchOptions {
  expand?: boolean
  hops?: number
  weight?: number
}

export async function runSearch(
  q: string,
  opts: SearchOptions = {},
  signal?: AbortSignal,
): Promise<SearchResponse> {
  const expand = opts.expand ?? false

  const req: SearchRequest = {
    q,
    filter: '',
    expand,
    hops: opts.hops ?? 0,
    weight: opts.weight ?? 0,
    limit: 0,
    // The explain fields (cosine/graph/final score, distance) only populate
    // server-side when both Explain and expansion are active — tying this to
    // `expand` means we always ask for them when they could possibly exist,
    // and never pay for them when expansion is off.
    explain: expand,
  }

  return postSearch(req, signal)
}

// scoreOf picks the score to display: final_score when graph expansion
// actually ran and populated it, the plain score otherwise. Both fields are
// `omitempty` on the Go side EXCEPT Score, which always marshals — so
// `match.score` is always safe to fall back to, but `final_score` must be
// checked for presence with `??`, never treated as always-present or
// defaulted to 0 when absent.
function scoreOf(match: Match): number {
  return match.final_score ?? match.score
}

function renderMatch(match: Match, onSelect: (id: string) => void): HTMLLIElement {
  const li = document.createElement('li')

  const button = document.createElement('button')
  button.type = 'button'
  button.className = 'contents-entry results-entry'
  button.dataset.id = match.id

  // Title/type come straight from vault content and are attacker-influenced
  // in a synced vault (the same boundary reader.ts's node head crosses).
  // Built with createElement/textContent, never an innerHTML template, so
  // there is no markup-injection surface to escape in the first place.
  const stamp = document.createElement('span')
  stamp.className = 'contents-entry-stamp'
  stamp.textContent = match.type

  const label = match.title || match.id
  const title = document.createElement('span')
  title.className = 'contents-entry-title'
  title.textContent = label
  title.title = label

  const score = document.createElement('span')
  score.className = 'results-entry-score'
  score.textContent = scoreOf(match).toFixed(3)

  button.append(stamp, title, score)
  button.addEventListener('click', () => onSelect(match.id))

  li.appendChild(button)
  return li
}

// renderResults paints resp.matches into `el`, replacing its previous
// contents. Clicking a result invokes onSelect with the match's raw id (never
// the encoded/hash form) — matching renderContents' convention, so main.ts
// can wire both the same way (`location.hash = buildNodeHash(id)`).
export function renderResults(el: HTMLElement, resp: SearchResponse, onSelect: (id: string) => void): void {
  el.innerHTML = ''

  if (resp.matches.length === 0) {
    const empty = document.createElement('p')
    empty.className = 'results-empty'
    empty.textContent = 'No matches.'
    el.appendChild(empty)
    return
  }

  const list = document.createElement('ul')
  list.className = 'contents-list results-list'

  for (const match of resp.matches) {
    list.appendChild(renderMatch(match, onSelect))
  }

  el.appendChild(list)
}

export type SearchBannerKind = 'notice' | 'error'

// renderSearchBanner paints a single inline banner into `el`, replacing its
// previous contents. 'notice' (the default) is the expected degradation —
// POST /api/search returned 422 because the embedder is off, which is not a
// failure state; browse and read keep working. 'error' is a real failure (a
// 400 malformed request or a 503 backend problem) and gets a visually
// distinct treatment so the two are never confused for one another. message
// may carry a raw Go error string (searchErr.Error(), workspace-influenced
// content), so this is textContent, not a template.
export function renderSearchBanner(el: HTMLElement, message: string, kind: SearchBannerKind = 'notice'): void {
  el.innerHTML = ''

  const banner = document.createElement('p')
  banner.className = kind === 'error' ? 'results-banner results-banner-error' : 'results-banner'
  banner.textContent = message

  el.appendChild(banner)
}
