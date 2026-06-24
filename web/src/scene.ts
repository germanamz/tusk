import ForceGraph3D from '3d-force-graph'
import type { Graph } from './api'
import {
  importanceColor,
  sizeForDegree,
  dimColor,
  EDGE_KIND_COLORS,
  SELECTED_COLOR,
  HIGHLIGHT_LINK_COLOR,
  PULSE_COLOR,
} from './encode'

export interface Scene {
  setGraph(graph: Graph, typeColors: Map<string, string>): void
  focus(ids: string[]): void
  /** Highlight a node + its incident edges and fly the camera to it. Pass null to clear. */
  select(id: string | null): void
  /** Transiently emphasize a set of nodes (search matches) without changing the selection. */
  pulse(ids: string[]): void
  /** Drop the transient pulse emphasis. */
  clearPulse(): void
  instance: ReturnType<typeof ForceGraph3D>
}

export function createScene(el: HTMLElement): Scene {
  const graph = ForceGraph3D()(el)
    // Size the canvas to its container, not the window. 3d-force-graph defaults
    // width/height to window.innerWidth/innerHeight and never reflows on its own,
    // so without this the WebGL canvas spills past #graph and shoves the rest of
    // the UI out of bounds.
    .width(el.clientWidth)
    .height(el.clientHeight)
    .backgroundColor('#0b0e14')
    .nodeId('id')
    .nodeLabel((node: any) => `${node.title || node.id} (${node.type})`)
    .linkDirectionalArrowLength(3)
    .linkDirectionalParticleWidth(2.5)
    // Hide the library's built-in nav hint; it hard-codes "right-click: pan" and
    // can't advertise the Alt+drag pan we wire below. We render our own (#legend).
    .showNavInfo(false)

  // Selection + highlight state. `selectedId` is the focused node; `highlightNodes`
  // additionally holds its neighbors; `highlightLinks` holds the incident link
  // objects (matched by identity). `pulseIds` is a separate, transient emphasis
  // driven by search and never touches the selection.
  let selectedId: string | null = null
  const highlightNodes = new Set<string>()
  const highlightLinks = new Set<any>()
  let pulseIds = new Set<string>()
  const isHighlighting = () => selectedId !== null

  // Per-graph visual-encoding state. `typeColors` maps node type → base hue and
  // is supplied by the caller from the FULL type universe (never recomputed from
  // a filtered subset, or hiding one type would shift every other type's color).
  // `maxInDegree` normalizes the brightness/size scales over the rendered set.
  let typeColors = new Map<string, string>()
  let maxInDegree = 0

  // All node/link styling flows through these accessors so selection, dimming,
  // and search pulses share one source of truth (the search code used to poke
  // graph.nodeColor directly, which fought with selection on its reset timer).
  const nodeColor = (node: any): string => {
    if (pulseIds.has(node.id)) return PULSE_COLOR
    if (node.id === selectedId) return SELECTED_COLOR
    const base = importanceColor(typeColors.get(node.type) ?? '#888888', node.in_degree ?? 0, maxInDegree)
    return isHighlighting() && !highlightNodes.has(node.id) ? dimColor(base) : base
  }
  const nodeVal = (node: any): number => {
    const base = sizeForDegree(node.in_degree ?? 0)
    if (pulseIds.has(node.id)) return base * 2.5 + 4
    if (node.id === selectedId) return base * 1.8
    return base
  }
  const linkColor = (link: any): string => {
    if (highlightLinks.has(link)) return HIGHLIGHT_LINK_COLOR
    const base = EDGE_KIND_COLORS[link.kind] ?? '#888'
    return isHighlighting() ? dimColor(base) : base
  }
  // Falsy widths render as cheap distance-independent 1px lines; only the
  // highlighted edges promote to solid cylinders so they stand out.
  const linkWidth = (link: any): number => (highlightLinks.has(link) ? 2.5 : 0)
  const linkParticles = (link: any): number => (highlightLinks.has(link) ? 4 : 0)

  // Re-applying the same accessor functions is how 3d-force-graph is told to
  // recompute per-object visuals after the highlight/pulse sets mutate.
  function refresh(): void {
    graph
      .nodeColor(nodeColor)
      .nodeVal(nodeVal)
      .linkColor(linkColor)
      .linkWidth(linkWidth)
      .linkDirectionalParticles(linkParticles)
  }
  refresh()

  function linkEnd(end: any): string {
    return typeof end === 'object' && end ? end.id : end
  }

  function highlight(id: string | null): void {
    selectedId = id
    highlightNodes.clear()
    highlightLinks.clear()
    if (id !== null) {
      highlightNodes.add(id)
      const { links } = graph.graphData() as { links: any[] }
      for (const link of links) {
        const source = linkEnd(link.source)
        const target = linkEnd(link.target)
        if (source === id || target === id) {
          highlightLinks.add(link)
          highlightNodes.add(source)
          highlightNodes.add(target)
        }
      }
    }
    refresh()
  }

  function focus(ids: string[]): void {
    const data = graph.graphData() as { nodes: any[] }
    const target = data.nodes.find((node) => ids.includes(node.id))
    if (!target) return
    graph.cameraPosition({ x: target.x, y: target.y, z: (target.z ?? 0) + 120 }, target, 1200)
  }

  // Controls: Alt+left-drag pans (the Blender/CAD-style modifier the user asked
  // for), left-drag still orbits, scroll zooms. 3d-force-graph uses
  // TrackballControls, whose `keys` array maps the [rotate, zoom, pan] modifier
  // key codes; pointing the pan slot at Alt makes alt+<any drag> pan while
  // leaving the default left-drag orbit untouched.
  const controls = graph.controls() as any
  if (controls && Array.isArray(controls.keys)) {
    controls.keys = ['', '', 'AltLeft']
  }

  // Keep the canvas matched to the container on every layout change (window
  // resize, panel show/hide). The library installs no resize handling itself.
  const resize = new ResizeObserver(() => {
    if (el.clientWidth && el.clientHeight) {
      graph.width(el.clientWidth).height(el.clientHeight)
    }
  })
  resize.observe(el)

  return {
    instance: graph,
    setGraph(next: Graph, nextTypeColors: Map<string, string>) {
      // Store the caller-supplied full type→color universe and renormalize the
      // brightness/size scale to the in-degree range of the rendered set.
      typeColors = nextTypeColors
      maxInDegree = next.nodes.reduce((m, node) => Math.max(m, node.in_degree ?? 0), 0)
      graph.graphData({
        nodes: next.nodes.map((node) => ({ ...node })),
        links: next.edges.map((edge) => ({ ...edge })),
      })
      // New data means new link objects, so the highlight set holds stale
      // references. Re-resolve the current selection against the fresh graph.
      if (selectedId !== null) highlight(selectedId)
    },
    focus,
    select(id: string | null) {
      highlight(id)
      if (id !== null) focus([id])
    },
    pulse(ids: string[]) {
      pulseIds = new Set(ids)
      refresh()
    },
    clearPulse() {
      pulseIds = new Set()
      refresh()
    },
  }
}
