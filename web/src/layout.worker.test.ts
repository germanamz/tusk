import { describe, it, expect } from 'vitest'
import { scaleToViewSpace, project } from './layout.worker'

// The worker module installs self.onmessage only inside a real WorkerGlobalScope,
// so importing it here (node, no worker global) just exposes the pure helpers.

describe('scaleToViewSpace', () => {
  const HALF_EXTENT = 600
  const EPSILON = 1e-9

  it('returns an empty array for empty input', () => {
    expect(scaleToViewSpace([])).toEqual([])
  })

  it('centers the cloud on the origin (per-axis mean ≈ 0)', () => {
    const coords = [
      [1, 2, 3],
      [-1, 0, 5],
      [4, -2, 1],
    ]
    const out = scaleToViewSpace(coords)
    const mean = out.reduce(
      (acc, p) => ({ x: acc.x + p.x, y: acc.y + p.y, z: acc.z + p.z }),
      { x: 0, y: 0, z: 0 },
    )
    expect(Math.abs(mean.x / out.length)).toBeLessThan(1e-6)
    expect(Math.abs(mean.y / out.length)).toBeLessThan(1e-6)
    expect(Math.abs(mean.z / out.length)).toBeLessThan(1e-6)
  })

  it('scales so the max absolute coordinate is ≈ half-extent', () => {
    const coords = [
      [0, 0, 0],
      [10, -4, 2],
      [-6, 8, -3],
    ]
    const out = scaleToViewSpace(coords)
    let maxAbs = 0
    for (const p of out) {
      maxAbs = Math.max(maxAbs, Math.abs(p.x), Math.abs(p.y), Math.abs(p.z))
    }
    expect(Math.abs(maxAbs - HALF_EXTENT)).toBeLessThan(1e-6)
  })

  it('preserves point count and order', () => {
    const coords = [
      [1, 1, 1],
      [2, 2, 2],
      [3, 3, 3],
    ]
    const out = scaleToViewSpace(coords)
    expect(out).toHaveLength(3)
    // Monotonic input on each axis stays monotonic after centering+scaling.
    expect(out[0].x).toBeLessThan(out[1].x)
    expect(out[1].x).toBeLessThan(out[2].x)
  })

  it('returns all points at the origin when the cloud has zero extent', () => {
    const coords = [
      [7, 7, 7],
      [7, 7, 7],
    ]
    const out = scaleToViewSpace(coords)
    for (const p of out) {
      expect(Math.abs(p.x)).toBeLessThan(EPSILON)
      expect(Math.abs(p.y)).toBeLessThan(EPSILON)
      expect(Math.abs(p.z)).toBeLessThan(EPSILON)
    }
  })

  it('produces finite, non-NaN coordinates', () => {
    const out = scaleToViewSpace([
      [0.5, -0.25, 0.75],
      [-0.5, 0.25, -0.75],
    ])
    for (const p of out) {
      expect(Number.isFinite(p.x)).toBe(true)
      expect(Number.isFinite(p.y)).toBe(true)
      expect(Number.isFinite(p.z)).toBe(true)
    }
  })
})

describe('project (degenerate inputs)', () => {
  it('returns empty positions for no vectors', () => {
    expect(project({ ids: [], vectors: [] })).toEqual({ ids: [], positions: [] })
  })

  it('places a single node at the origin without running UMAP', () => {
    // UMAP throws on one point (no neighbor); project short-circuits instead.
    expect(project({ ids: ['only'], vectors: [[0.1, 0.2, 0.3]] })).toEqual({
      ids: ['only'],
      positions: [{ x: 0, y: 0, z: 0 }],
    })
  })
})
