// Typed client mirroring internal/bookview/bookview.go's wire contract. Every
// interface here is the JSON shape of a Go struct in that file; keep them in
// sync by field name and by the omitempty asymmetries noted below rather than
// by "what looks right" — bookview.go is the source of truth, not this file.
import { encodeId } from './encode'

// IndexNode deliberately has no "parent" field. Every row GET /api/index
// returns has a NULL parent_id by construction (ListFileNodes selects WHERE
// parent_id IS NULL), so a Parent field would always be empty — the Go side
// dropped it rather than ship a field that lies. The Contents pane derives its
// tree from `path` instead (e.g. "specs/x.md" nests under "specs/").
export interface IndexNode {
  id: string
  type: string
  title: string
  path: string
}

export interface IndexResponse {
  nodes: IndexNode[]
}

// LinkRef is one end of a link shown in the reading rails. Sub-unit far ends
// are rolled up to their parent file by bookview before this ever reaches the
// wire, so `id` here is always a file id.
export interface LinkRef {
  id: string
  title: string
  type: string
  edge_type: string
}

// WikilinkTarget is keyed in NodeReadPayload.wikilinks on the RAW wikilink
// target text (fragments retained, e.g. "c#S1"), not on a resolved id.
export interface WikilinkTarget {
  id: string
  title: string
  exists: boolean
}

// NodeReadPayload mirrors bookview.NodeReadPayload. `links.out`/`links.in`
// and `wikilinks` always marshal as `[]`/`{}` on the Go side, never `null`.
export interface NodeReadPayload {
  id: string
  type: string
  title: string
  path: string
  properties: Record<string, unknown>
  markdown: string
  links: {
    out: LinkRef[]
    in: LinkRef[]
  }
  wikilinks: Record<string, WikilinkTarget>
}

// edge_types is optional here (unlike Go's `EdgeTypes []string`, which has no
// omitempty and is always present in a decoded struct) so a caller building a
// request can leave the KEY OUT of the JSON body entirely. That distinction
// matters: an explicit `edge_types: []` decodes on the Go side to a non-nil,
// zero-length slice, which manifest.MergeGraphExpansion treats as "no edge
// types" — a real override that disables graph-expansion traversal, not an
// "unset" signal. Only an absent key decodes to nil (= inherit the manifest
// default). search.ts's runSearch relies on this to build a request with no
// edge-type control at all.
export interface SearchRequest {
  q: string
  filter: string
  expand: boolean
  hops: number
  edge_types?: string[]
  weight: number
  limit: number
  explain: boolean
}

// Match's score-breakdown fields are Go `omitempty`: a legitimately-0 value
// vanishes from the JSON entirely, so they must be optional here too. Do NOT
// widen this to match RelatedNode — the two are asymmetric on purpose (see
// RelatedNode below).
export interface Match {
  id: string
  title: string
  type: string
  score: number
  cosine_score?: number
  graph_score?: number
  final_score?: number
  distance?: number
}

export interface SearchResponse {
  matches: Match[]
  model: string
}

// SearchUnavailableError signals a 422 from POST /api/search: the embedder is
// missing or unreachable (expected — the user's Ollama may simply be off).
// Browse and read keep working; catch this specifically (instanceof) to
// render a "semantic search unavailable" banner instead of a generic failure.
// A 400 (bad body) or 503 (a real failure) surface as a plain Error instead,
// so callers can tell the two apart.
export class SearchUnavailableError extends Error {
  constructor(message: string) {
    super(message)
    this.name = 'SearchUnavailableError'
  }
}

// RelatedNode's graph_score is NOT omitempty on the Go side (unlike
// Match.graph_score) — it always marshals, including when it is 0. That
// asymmetry is deliberate and load-bearing: model it honestly by leaving this
// field required rather than "fixing" it to match Match.
export interface RelatedNode {
  id: string
  title: string
  type: string
  graph_score: number
  distance: number
}

export interface RelatedResponse {
  related: RelatedNode[]
}

async function fetchJSON<T>(input: string, init?: RequestInit): Promise<T> {
  const resp = await fetch(input, init)
  if (!resp.ok) throw new Error(`${input}: ${resp.status}`)
  return (await resp.json()) as T
}

export async function fetchIndex(signal?: AbortSignal): Promise<IndexResponse> {
  return fetchJSON<IndexResponse>('./api/index', { signal })
}

export async function fetchNode(id: string, signal?: AbortSignal): Promise<NodeReadPayload> {
  return fetchJSON<NodeReadPayload>(`./api/node/${encodeId(id)}`, { signal })
}

// postSearch throws SearchUnavailableError on a 422 rather than folding it
// into the generic error path, so a caller can distinguish "search is
// expectedly unavailable" from "something is actually broken" with
// `instanceof` instead of parsing a status code back out of a message string.
export async function postSearch(req: SearchRequest, signal?: AbortSignal): Promise<SearchResponse> {
  const resp = await fetch('./api/search', {
    method: 'POST',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify(req),
    signal,
  })

  if (resp.status === 422) {
    throw new SearchUnavailableError(await resp.text())
  }

  if (!resp.ok) throw new Error(`./api/search: ${resp.status}`)

  return (await resp.json()) as SearchResponse
}

// RelatedOptions carries GET /api/related/{id...}'s optional query params.
// `undefined` means "not specified — inherit the workspace manifest's
// [query.graph-expansion] default"; the Go handler reads absence of the query
// param itself as that signal (queryIntParam/queryFloatParam key off
// URL.Query().Has, not a zero value). Fields are therefore left optional
// rather than defaulted to 0 — defaulting hops/weight to 0 here would send an
// explicit `weight=0` that silently zeroes the graph term for every distance-2
// score, which is exactly the trap the Go side's pointer types exist to avoid.
export interface RelatedOptions {
  hops?: number
  weight?: number
  edgeTypes?: string[]
}

export async function fetchRelated(
  id: string,
  opts: RelatedOptions = {},
  signal?: AbortSignal,
): Promise<RelatedResponse> {
  const params = new URLSearchParams()

  if (opts.hops !== undefined) params.set('hops', String(opts.hops))
  if (opts.weight !== undefined) params.set('weight', String(opts.weight))
  if (opts.edgeTypes !== undefined) params.set('edge_types', opts.edgeTypes.join(','))

  const qs = params.toString()
  const url = `./api/related/${encodeId(id)}${qs ? `?${qs}` : ''}`

  return fetchJSON<RelatedResponse>(url, { signal })
}
