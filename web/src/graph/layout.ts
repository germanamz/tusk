// Golden-angle in radians for the Fibonacci sphere construction. Defined
// independently of encode.ts's degree-based GOLDEN_ANGLE constant to keep
// layout.ts free of any import dependency (and therefore unit-testable
// without a DOM or library import).
const GOLDEN_ANGLE_RAD = Math.PI * (3 - Math.sqrt(5))

// fibonacciSphereAnchors returns a deterministic per-group anchor set spread
// evenly on a sphere of the given radius using the golden-angle (Fibonacci
// sphere) construction. Groups are de-duplicated and sorted so the same group
// set always produces the same anchor for the same group, regardless of input
// order. The empty-string group ("no group") is treated like any other distinct
// value; the caller decides whether to pull it.
//
// Edge cases: empty input returns an empty map; a single group gets the north
// pole (0, radius, 0) with no division-by-zero risk.
export function fibonacciSphereAnchors(
  groups: string[],
  radius: number,
): Map<string, { x: number; y: number; z: number }> {
  const distinct = [...new Set(groups)].sort()
  const count = distinct.length
  const out = new Map<string, { x: number; y: number; z: number }>()

  if (count === 0) return out

  // Single-group special case: place at the pole to avoid division by zero
  // in the (count - 1) denominator below.
  if (count === 1) {
    out.set(distinct[0], { x: 0, y: radius, z: 0 })
    return out
  }

  for (let index = 0; index < count; index++) {
    // y spans from +1 down to -1 as index goes from 0 to count-1.
    const yy = 1 - (index / (count - 1)) * 2
    const rr = Math.sqrt(Math.max(0, 1 - yy * yy))
    const theta = index * GOLDEN_ANGLE_RAD
    out.set(distinct[index], {
      x: Math.cos(theta) * rr * radius,
      y: yy * radius,
      z: Math.sin(theta) * rr * radius,
    })
  }

  return out
}

// Returns new node objects for `nextNodes`, copying x/y/z/vx/vy/vz forward
// from `prev` (keyed by id) when the id already existed. New ids get a plain
// spread (no coords → the engine seeds them). Removed ids are simply absent.
export function carryPositions(
  prev: Map<string, { x?: number; y?: number; z?: number; vx?: number; vy?: number; vz?: number }>,
  nextNodes: any[],
): any[] {
  return nextNodes.map((node) => {
    const carried = { ...node }
    const prior = prev.get(node.id)
    if (prior !== undefined) {
      if (prior.x !== undefined) carried.x = prior.x
      if (prior.y !== undefined) carried.y = prior.y
      if (prior.z !== undefined) carried.z = prior.z
      if (prior.vx !== undefined) carried.vx = prior.vx
      if (prior.vy !== undefined) carried.vy = prior.vy
      if (prior.vz !== undefined) carried.vz = prior.vz
    }
    return carried
  })
}
