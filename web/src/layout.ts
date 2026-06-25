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
