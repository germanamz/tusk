import type { Match } from './api' // add Match to api.ts: { id: string; score: number }

export interface QueryPayload { filter: string; q: string }

// classifyQuery routes `key:value ...` to the structural filter and free prose
// to semantic. A heuristic: if every whitespace token contains a colon, it's a
// filter; otherwise it's prose.
export function classifyQuery(text: string): QueryPayload {
  const trimmed = text.trim()
  const tokens = trimmed.split(/\s+/)
  const looksStructural = trimmed.length > 0 && tokens.every((token) => token.includes(':'))
  return looksStructural ? { filter: trimmed, q: '' } : { filter: '', q: trimmed }
}

export async function runSearch(text: string, limit = 50): Promise<{ matches: Match[]; unavailable: boolean }> {
  const payload = classifyQuery(text)
  const resp = await fetch('/api/graph/query', {
    method: 'POST',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify({ ...payload, limit }),
  })
  if (resp.status === 422) return { matches: [], unavailable: true }
  if (!resp.ok) throw new Error(`query failed: ${resp.status}`)
  const body = (await resp.json()) as { matches: Match[] }
  return { matches: body.matches, unavailable: false }
}
