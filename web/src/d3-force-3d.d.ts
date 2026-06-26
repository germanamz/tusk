// Local type shim for d3-force-3d@3. The package ships no bundled .d.ts;
// this minimal declaration covers the four force factories imported by scene.ts.
// Kept local rather than pulling an out-of-band @types package.
declare module 'd3-force-3d' {
  export interface ForceNode {
    x?: number
    y?: number
    z?: number
    vx?: number
    vy?: number
    vz?: number
    [key: string]: unknown
  }

  export interface Force {
    initialize?(nodes: ForceNode[]): void
    (alpha: number): void
  }

  export interface ForceAxis extends Force {
    strength(accessor: number | ((node: ForceNode, index: number, nodes: ForceNode[]) => number)): this
    x?: (accessor: number | ((node: ForceNode, index: number, nodes: ForceNode[]) => number)) => this
    y?: (accessor: number | ((node: ForceNode, index: number, nodes: ForceNode[]) => number)) => this
    z?: (accessor: number | ((node: ForceNode, index: number, nodes: ForceNode[]) => number)) => this
  }

  export interface ForceCollideInstance extends Force {
    radius(accessor: number | ((node: ForceNode, index: number, nodes: ForceNode[]) => number)): this
    strength(value: number): this
    iterations(value: number): this
  }

  export function forceX(accessor?: number | ((node: ForceNode, index: number, nodes: ForceNode[]) => number)): ForceAxis
  export function forceY(accessor?: number | ((node: ForceNode, index: number, nodes: ForceNode[]) => number)): ForceAxis
  export function forceZ(accessor?: number | ((node: ForceNode, index: number, nodes: ForceNode[]) => number)): ForceAxis
  export function forceCollide(radius?: number | ((node: ForceNode, index: number, nodes: ForceNode[]) => number)): ForceCollideInstance
}
