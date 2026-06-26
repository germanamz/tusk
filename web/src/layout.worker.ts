// UMAP projection worker. Self-contained: imports only umap-js (no scene/main/
// DOM). Given one unit vector per file node, it runs UMAP down to 3D and scales
// the cloud into the view's ~hundreds-of-units range, then posts back the pinned
// coordinates. The heavy library stays off the main thread so the UI never
// blocks while a vault's worth of vectors is projected.
import { UMAP } from 'umap-js'

// Request: ids + their embedding vectors (parallel arrays). nComponents is fixed
// at 3 for the 3D view but accepted in the message for forward-compatibility.
export interface LayoutRequest {
  ids: string[]
  vectors: number[][]
  nComponents?: 3
}

export interface Position {
  x: number
  y: number
  z: number
}

// Reply: ids echoed back in the same order, paired with their 3D positions. On
// failure, `error` carries the message instead.
export interface LayoutReply {
  ids?: string[]
  positions?: Position[]
  error?: string
}

// Target half-extent of the projected cloud in view space. The existing layout
// works in a ~hundreds-of-units range (ANCHOR_RADIUS = 400 in encode.ts); raw
// UMAP output is ~-10..10 and would clump at the origin without this rescale.
const VIEW_HALF_EXTENT = 450

// mulberry32: a tiny, fast seeded PRNG. UMAP only needs a uniform [0,1) source;
// seeding it with a fixed constant makes the projection deterministic, so the
// same vectors always yield the same map across runs and reloads.
function mulberry32(seed: number): () => number {
  let a = seed >>> 0
  return () => {
    a += 0x6d2b79f5
    let t = a
    t = Math.imul(t ^ (t >>> 15), t | 1)
    t ^= t + Math.imul(t ^ (t >>> 7), t | 61)
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296
  }
}

const SEED = 0x9e3779b9

// scaleToViewSpace centers a cloud of raw [x,y,z] coordinates on the origin
// (subtracting the per-axis mean) and scales it so the maximum absolute
// coordinate is ~VIEW_HALF_EXTENT. A zero-extent cloud (all points identical)
// is returned centered at the origin (scale guarded against divide-by-zero).
// Exported for unit testing; the worker uses it on the UMAP output.
export function scaleToViewSpace(coords: number[][]): Position[] {
  if (coords.length === 0) return []

  const mean = [0, 0, 0]
  for (const c of coords) {
    mean[0] += c[0]
    mean[1] += c[1]
    mean[2] += c[2]
  }
  mean[0] /= coords.length
  mean[1] /= coords.length
  mean[2] /= coords.length

  let maxAbs = 0
  for (const c of coords) {
    maxAbs = Math.max(maxAbs, Math.abs(c[0] - mean[0]), Math.abs(c[1] - mean[1]), Math.abs(c[2] - mean[2]))
  }

  const scale = maxAbs === 0 ? 1 : VIEW_HALF_EXTENT / maxAbs

  return coords.map((c) => ({
    x: (c[0] - mean[0]) * scale,
    y: (c[1] - mean[1]) * scale,
    z: (c[2] - mean[2]) * scale,
  }))
}

// project runs the full pipeline for one request: clamp nNeighbors for tiny
// inputs, fit UMAP with a seeded PRNG, and rescale into view space.
export function project(req: LayoutRequest): LayoutReply {
  const { ids, vectors } = req
  if (vectors.length === 0) return { ids: [], positions: [] }

  // nNeighbors must stay below the sample count; UMAP's default is 15. Clamp to
  // vectors.length - 1 for tiny inputs and keep it at least 1.
  const nNeighbors = Math.max(1, Math.min(15, vectors.length - 1))

  const umap = new UMAP({
    nComponents: 3,
    nNeighbors,
    minDist: 0.1,
    random: mulberry32(SEED),
  })

  const coords = umap.fit(vectors)
  return { ids, positions: scaleToViewSpace(coords) }
}

// Worker message wiring. Guarded so the module can be imported by unit tests
// (node, no worker global) without registering a handler; in a real module
// worker `self` is always defined.
if (typeof self !== 'undefined') {
  self.onmessage = (ev: MessageEvent<LayoutRequest>): void => {
    try {
      const reply = project(ev.data)
      self.postMessage(reply)
    } catch (err) {
      self.postMessage({ error: err instanceof Error ? err.message : String(err) } satisfies LayoutReply)
    }
  }
}
