import ForceGraph3D from '3d-force-graph'
import { forceX, forceY, forceZ, forceCollide } from 'd3-force-3d'
import type { Graph } from './api'
import {
  importanceColor,
  sizeForDegree,
  dimColor,
  rgba,
  edgeAlpha,
  EDGE_KIND_COLORS,
  SELECTED_COLOR,
  HIGHLIGHT_LINK_COLOR,
  PULSE_COLOR,
  ANCHOR_RADIUS,
  ANCHOR_PULL_STRENGTH,
  COLLIDE_RADIUS,
  SOFT_CHARGE_STRENGTH,
  DEFAULT_CHARGE_STRENGTH,
  INTRA_ALPHA_DEFAULT,
  INTER_ALPHA_DEFAULT,
  HUB_STRENGTH_DEFAULT,
  DIM_FACTOR,
} from './encode'
import { carryPositions, fibonacciSphereAnchors } from './layout'
import { createHullOverlay } from './hulls'

export interface Scene {
  setGraph(graph: Graph, groupColors: Map<string, string>): void
  focus(ids: string[]): void
  /** Highlight a node + its incident edges and fly the camera to it. Pass null to clear. */
  select(id: string | null): void
  /** Transiently emphasize a set of nodes (search matches) without changing the selection. */
  pulse(ids: string[]): void
  /** Drop the transient pulse emphasis. */
  clearPulse(): void
  /** Dim every node/link whose group is not in `focus`; null clears the dimming.
   *  Composes with select() — a node is bright only if neither layer dims it. */
  focusGroup(focus: Set<string> | null): void
  /** Update edge-emphasis parameters (inter-cluster alpha + hub-fade strength)
   *  and repaint edges immediately. Partial merge; unspecified keys are kept. */
  setEdgeEmphasis(partial: { intraAlpha?: number; interAlpha?: number; hubStrength?: number }): void
  /** Switch layout mode. 'semantic' pins nodes at coords set via setSemanticCoords
   *  and suppresses huddle; 'structure' restores the force layout. */
  setLayoutMode(mode: 'structure' | 'semantic'): void
  /** Supply pinned coordinates for semantic mode (nodeId -> position). Stored and
   *  re-applied on every setGraph so live re-snapshots keep the pins. */
  setSemanticCoords(coords: Map<string, { x: number; y: number; z: number }>): void
  instance: InstanceType<typeof ForceGraph3D>
}

export function createScene(el: HTMLElement): Scene {
  // 3d-force-graph 1.77+ moved from the factory form `ForceGraph3D()(el)` to a
  // constructor: `new ForceGraph3D(el)`.
  const graph = new ForceGraph3D(el)
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

  // Group-dimming state. When non-null, nodes whose group is NOT in this set
  // are dimmed by `nodeColor`/`linkColor`. Composes with the selection layer —
  // a node is bright only if neither the selection layer nor the group layer dims it.
  let focusedGroups: Set<string> | null = null

  // Edge-emphasis state. Controls per-edge alpha: intra-cluster edges keep a
  // high base alpha; inter-cluster edges are faded to `interAlpha`; hub-incident
  // edges are further damped by `hubStrength`. `intraAlpha` is not user-exposed.
  let edgeEmphasis = {
    intraAlpha: INTRA_ALPHA_DEFAULT,
    interAlpha: INTER_ALPHA_DEFAULT,
    hubStrength: HUB_STRENGTH_DEFAULT,
  }

  // Hull overlay: one translucent convex-hull mesh per group, recomputed on a
  // throttled tick and once on engine stop. Created once here; setGraph drives
  // enable/disable via next.cluster.hull.
  const hulls = createHullOverlay(graph.scene())

  // Throttle state for hull recomputes during the engine tick. The hull
  // geometry is expensive; ~250ms between recomputes is sufficient to track
  // a settling layout without burning the frame budget.
  const HULL_THROTTLE_MS = 250
  let lastHullUpdateMs = 0

  // Per-graph visual-encoding state. `groupColors` maps group value → base hue
  // and is supplied by the caller from the FULL group universe (never recomputed
  // from a filtered subset, or hiding one group would shift every other group's
  // color). `maxDegree` normalizes the brightness/size scales over the rendered
  // set. `groupAnchors` holds the deterministic per-group Fibonacci sphere
  // positions used by the huddle forces; it is recomputed each snapshot and
  // read by the force closures registered below.
  let groupColors = new Map<string, string>()
  let hullEnabled = false
  let maxDegree = 0
  let groupAnchors = new Map<string, { x: number; y: number; z: number }>()

  // Layout-mode state. In 'semantic' mode each embedded file node is pinned
  // (fx/fy/fz) at its UMAP coordinate from `semanticCoords` and huddle is
  // suppressed; 'structure' is the default force layout. carryPositions drops
  // fx/fy/fz across snapshots, so applyPins re-applies the pins on every setGraph.
  let layoutMode: 'structure' | 'semantic' = 'structure'
  let semanticCoords = new Map<string, { x: number; y: number; z: number }>()

  // applyPins pins (semantic) or clears (structure) fx/fy/fz on the live graph
  // node objects. Must run AFTER graph.graphData({...}) so it operates on the
  // freshly carried nodes.
  //
  // In semantic mode, a node WITH a projected coord is pinned at its UMAP
  // position; a node WITHOUT one is left unpinned (fx/fy/fz cleared) so the force
  // simulation places it. The coordless nodes are drill-down sub-units (embedded
  // per sub-unit, no file-level vector) and any file added since the last
  // projection — leaving them free lets the link force settle a sub-unit next to
  // its pinned parent rather than freezing it far from its meaning, and a
  // signature-changing reproject folds newly-embedded files into the cloud.
  function applyPins(): void {
    const nodes = (graph.graphData() as { nodes: any[] }).nodes
    if (layoutMode === 'semantic') {
      for (const nd of nodes) {
        const c = semanticCoords.get(nd.id)
        nd.fx = c?.x
        nd.fy = c?.y
        nd.fz = c?.z
      }
    } else {
      for (const nd of nodes) {
        nd.fx = undefined
        nd.fy = undefined
        nd.fz = undefined
      }
    }
  }

  // All node/link styling flows through these accessors so selection, dimming,
  // and search pulses share one source of truth (the search code used to poke
  // graph.nodeColor directly, which fought with selection on its reset timer).
  const nodeColor = (node: any): string => {
    if (pulseIds.has(node.id)) return PULSE_COLOR
    if (node.id === selectedId) return SELECTED_COLOR
    const base = importanceColor(groupColors.get(node.group) ?? '#888888', node.degree ?? 0, maxDegree)
    const selectionDim = isHighlighting() && !highlightNodes.has(node.id)
    const groupDim = focusedGroups !== null && !focusedGroups.has(node.group)
    return selectionDim || groupDim ? dimColor(base) : base
  }
  const nodeVal = (node: any): number => {
    const base = sizeForDegree(node.degree ?? 0, maxDegree)
    if (pulseIds.has(node.id)) return base * 2.5 + 4
    if (node.id === selectedId) return base * 1.8
    return base
  }
  const linkColor = (link: any): string => {
    if (highlightLinks.has(link)) return HIGHLIGHT_LINK_COLOR
    const kind: string = EDGE_KIND_COLORS[link.kind] ?? '#888'
    const sourceGroup: string = (typeof link.source === 'object' && link.source ? link.source.group : undefined) ?? ''
    const targetGroup: string = (typeof link.target === 'object' && link.target ? link.target.group : undefined) ?? ''
    const srcDeg: number = (typeof link.source === 'object' && link.source ? link.source.degree : undefined) ?? 0
    const tgtDeg: number = (typeof link.target === 'object' && link.target ? link.target.degree : undefined) ?? 0
    const sameGroup = sourceGroup !== '' && sourceGroup === targetGroup
    const base = edgeAlpha(sameGroup, srcDeg, tgtDeg, maxDegree, edgeEmphasis)
    const dimmed = isHighlighting() || (focusedGroups !== null && !(focusedGroups.has(sourceGroup) && focusedGroups.has(targetGroup)))
    const alpha = dimmed ? base * DIM_FACTOR : base
    return rgba(kind, alpha)
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

  // Hull recompute hooks. The tick hook is throttled (~250ms) so ConvexGeometry
  // is not rebuilt on every animation frame; the stop hook fires once when the
  // simulation settles so the final positions always get a clean hull.
  graph.onEngineTick(() => {
    if (!hullEnabled) return
    const now = Date.now()
    if (now - lastHullUpdateMs < HULL_THROTTLE_MS) return
    lastHullUpdateMs = now
    hulls.update((graph.graphData() as { nodes: any[] }).nodes, groupColors)
  })
  graph.onEngineStop(() => {
    if (!hullEnabled) return
    lastHullUpdateMs = Date.now()
    hulls.update((graph.graphData() as { nodes: any[] }).nodes, groupColors)
  })

  return {
    instance: graph,
    setGraph(next: Graph, nextGroupColors: Map<string, string>) {
      // Store the caller-supplied full group→color universe and renormalize the
      // brightness/size scale to the degree range of the rendered set.
      groupColors = nextGroupColors
      hullEnabled = next.cluster?.hull ?? false
      maxDegree = next.nodes.reduce((m, node) => Math.max(m, node.degree ?? 0), 0)
      // Carry positions forward from the current frame so d3-force-3d does not
      // re-seed known nodes into the phyllotaxis spiral on every live re-snapshot.
      // New ids (no prior position) get a plain spread and are seeded normally.
      const prevById = new Map<string, any>()
      for (const nd of (graph.graphData() as { nodes: any[] }).nodes) {
        prevById.set(nd.id, nd)
      }
      graph.graphData({
        nodes: carryPositions(prevById, next.nodes),
        links: next.edges.map((edge) => ({ ...edge })),
      })
      // New data means new link objects, so the highlight set holds stale
      // references. Re-resolve the current selection against the fresh graph.
      if (selectedId !== null) highlight(selectedId)

      // Recompute anchors from this snapshot's group set. The closures below
      // read groupAnchors by reference, so updating the map here is sufficient
      // whether or not the forces are re-registered this snapshot.
      groupAnchors = fibonacciSphereAnchors(
        next.nodes.map((nd) => nd.group ?? ''),
        ANCHOR_RADIUS,
      )

      // anchorStrength returns 0 for ungrouped nodes (empty group or no anchor)
      // so they feel no pull and float free. Grouped nodes get ANCHOR_PULL_STRENGTH.
      const anchorStrength = (nd: any): number => {
        const grp: string = nd.group ?? ''
        if (grp === '' || !groupAnchors.has(grp)) return 0
        return ANCHOR_PULL_STRENGTH
      }

      if (layoutMode === 'structure' && next.cluster?.huddle) {
        // Register/refresh the four huddle forces. Calling the setter each
        // snapshot replaces any prior registration idempotently. Suppressed in
        // semantic mode — pins and huddle anchors pull in conflicting directions.
        ;(graph as any).d3Force(
          'groupX',
          forceX((nd: any) => groupAnchors.get(nd.group ?? '')?.x ?? (nd.x ?? 0)).strength(
            (nd: any) => anchorStrength(nd),
          ),
        )
        ;(graph as any).d3Force(
          'groupY',
          forceY((nd: any) => groupAnchors.get(nd.group ?? '')?.y ?? (nd.y ?? 0)).strength(
            (nd: any) => anchorStrength(nd),
          ),
        )
        ;(graph as any).d3Force(
          'groupZ',
          forceZ((nd: any) => groupAnchors.get(nd.group ?? '')?.z ?? (nd.z ?? 0)).strength(
            (nd: any) => anchorStrength(nd),
          ),
        )
        ;(graph as any).d3Force('collide', forceCollide(COLLIDE_RADIUS))
        ;(graph as any).d3Force('charge').strength(SOFT_CHARGE_STRENGTH)
      } else {
        // Remove any previously registered huddle forces and restore the
        // default charge so toggling huddle off is fully reversible.
        ;(graph as any).d3Force('groupX', null)
        ;(graph as any).d3Force('groupY', null)
        ;(graph as any).d3Force('groupZ', null)
        ;(graph as any).d3Force('collide', null)
        ;(graph as any).d3Force('charge').strength(DEFAULT_CHARGE_STRENGTH)
      }

      // Pin (semantic) or clear (structure) coords on the freshly carried nodes,
      // AFTER graphData() is set and BEFORE the reheat. Pinned nodes ignore
      // link/charge forces, so leaving those at defaults in semantic mode is fine.
      applyPins()

      // Reheat the simulation so the engine re-settles into (or out of) the
      // clustered layout rather than waiting for the next structural data change.
      ;(graph as any).d3ReheatSimulation()

      // Hull overlay: enable/disable and trigger an immediate recompute so a
      // snapshot that changes membership redraws without waiting for the next
      // tick. Read positions from the live graph data (the carried/simulated
      // node objects) rather than `next.nodes` — the raw payload carries no
      // x/y/z, so using it here would drop every hull until the first tick.
      if (hullEnabled) {
        hulls.setEnabled(true)
        lastHullUpdateMs = Date.now()
        hulls.update((graph.graphData() as { nodes: any[] }).nodes, groupColors)
      } else {
        hulls.setEnabled(false)
      }
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
    focusGroup(focus: Set<string> | null) {
      focusedGroups = focus
      refresh()
    },
    setEdgeEmphasis(partial: { intraAlpha?: number; interAlpha?: number; hubStrength?: number }) {
      edgeEmphasis = { ...edgeEmphasis, ...partial }
      refresh()
    },
    setLayoutMode(mode: 'structure' | 'semantic') {
      // Only flip the flag and re-pin. Huddle (de)registration is owned by
      // setGraph's Structure-gated block; the caller follows every mode flip with
      // a setGraph re-render, so huddle is correctly re-evaluated there. The reheat
      // is intentionally NOT done here: the trailing rerender()'s setGraph already
      // reheats, and reheating twice caused a one-frame jitter on the flip.
      layoutMode = mode
      applyPins()
    },
    setSemanticCoords(coords: Map<string, { x: number; y: number; z: number }>) {
      semanticCoords = coords
      if (layoutMode === 'semantic') {
        applyPins()
        ;(graph as any).d3ReheatSimulation()
      }
    },
  }
}
