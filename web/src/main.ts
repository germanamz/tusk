import { fetchGraph, type Graph } from './api'
import { createScene } from './scene'
import { subscribeGraph } from './stream'
import { applyFacets, type FacetState } from './facets'
import { fetchNodeDetail, fetchSubunits } from './nodeapi'
import { renderPanel } from './panel'
import { buildGroupColors } from './encode'
import { runSearch } from './search'
import { createControls } from './controls'

let rawGraph: Graph = { generation: 0, epoch: 0, nodes: [], edges: [], cluster: { by: 'type', huddle: false, hull: false } }
let rawGroupColors: Map<string, string> = new Map()
let facetState: FacetState = { hiddenTypes: new Set(), hiddenKinds: new Set(), hideOrphans: false, hiddenGroups: new Set() }

// groupColorsFor builds the group→color map for node colors and the legend.
// When graph.cluster.by === "type", group === type and the colors are
// pixel-identical to the old type-based coloring.
const groupColorsFor = (g: Graph): Map<string, string> =>
  buildGroupColors(g.nodes.map((n) => n.group))

function mergeSubunits(base: Graph, subunits: { nodes: any[]; edges: any[] }): Graph {
  const existingIds = new Set(base.nodes.map((n) => n.id))
  const newNodes = subunits.nodes.filter((n) => !existingIds.has(n.id))
  const existingEdgeKeys = new Set(base.edges.map((e) => `${e.source}|${e.target}|${e.type}`))
  const newEdges = subunits.edges.filter(
    (e) => !existingEdgeKeys.has(`${e.source}|${e.target}|${e.type}`),
  )
  return {
    ...base,
    nodes: [...base.nodes, ...newNodes],
    edges: [...base.edges, ...newEdges],
  }
}

async function boot(): Promise<void> {
  const el = document.getElementById('graph')!
  const panelEl = document.getElementById('panel')!
  const searchMsg = document.getElementById('search-msg')!
  const banner = document.getElementById('banner')!
  const scene = createScene(el)

  // Build the controls drawer once; diff-update it on each snapshot.
  const controls = createControls({
    scene,
    getRawGraph: () => rawGraph,
    getGroupColors: () => rawGroupColors,
    facetState,
    onFilterChange: () => scene.setGraph(applyFacets(rawGraph, facetState), rawGroupColors),
  })

  // Search form — binds to #search-form / #search which live in the #controls
  // drawer header (a stable container never innerHTML-cleared). This binding
  // holds across all live snapshots because controls.ts never rebuilds the header.
  const searchForm = document.getElementById('search-form') as HTMLFormElement
  const searchInput = document.getElementById('search') as HTMLInputElement
  searchForm.addEventListener('submit', (e) => {
    e.preventDefault()
    const text = searchInput.value.trim()
    if (!text) return
    runSearch(text)
      .then(({ matches, unavailable }) => {
        if (unavailable) {
          searchMsg.textContent = 'semantic search needs an embedding provider (structural filter + facets still work)'
          searchMsg.style.display = 'block'
          return
        }
        searchMsg.style.display = 'none'
        if (matches.length === 0) {
          searchMsg.textContent = 'no matches'
          searchMsg.style.display = 'block'
          return
        }
        const ids = matches.map((m) => m.id)
        // Transiently emphasize matched nodes, then fly to the first one.
        scene.pulse(ids)
        scene.focus(ids)
        // Drop the emphasis after 3 s (the selection highlight, if any, stays).
        setTimeout(() => scene.clearPulse(), 3000)
      })
      .catch((err) => {
        searchMsg.textContent = `search error: ${String(err)}`
        searchMsg.style.display = 'block'
      })
  })

  // Node click → select (highlight node + its edges, fly the camera in) and
  // render its detail panel.
  scene.instance.onNodeClick((node: any) => {
    scene.select(node.id)
    fetchNodeDetail(node.id)
      .then((detail) => {
        renderPanel(panelEl, detail, (neighborId) => {
          // Navigate to neighbor: select it (re-highlights + focuses) and
          // fetch its detail.
          scene.select(neighborId)
          fetchNodeDetail(neighborId)
            .then((nd) => renderPanel(panelEl, nd, () => {}))
            .catch(console.error)
        })

        // Add expand button for sub-units
        const expandBtn = document.createElement('button')
        expandBtn.textContent = 'Expand sub-units'
        expandBtn.style.cssText = 'margin-top:8px;display:block'
        expandBtn.addEventListener('click', () => {
          fetchSubunits(node.id)
            .then((sub) => {
              rawGraph = mergeSubunits(rawGraph, sub)
              rawGroupColors = groupColorsFor(rawGraph)
              scene.setGraph(applyFacets(rawGraph, facetState), rawGroupColors)
              controls.update(rawGraph, rawGroupColors)
            })
            .catch(console.error)
        })
        panelEl.appendChild(expandBtn)
      })
      .catch(console.error)
  })

  // Right-click → expand sub-units inline
  scene.instance.onNodeRightClick((node: any) => {
    fetchSubunits(node.id)
      .then((sub) => {
        rawGraph = mergeSubunits(rawGraph, sub)
        rawGroupColors = groupColorsFor(rawGraph)
        scene.setGraph(applyFacets(rawGraph, facetState), rawGroupColors)
        controls.update(rawGraph, rawGroupColors)
      })
      .catch(console.error)
  })

  // Click empty space → clear the selection highlight, the panel, and the group solo.
  scene.instance.onBackgroundClick(() => {
    scene.select(null)
    controls.clearSolo()
    panelEl.innerHTML = ''
  })

  function applyAndRender(graph: Graph): void {
    rawGraph = graph
    rawGroupColors = groupColorsFor(graph)
    controls.update(graph, rawGroupColors)
    scene.setGraph(applyFacets(graph, facetState), rawGroupColors)
    // Scale guardrail: warn when graph is very large but never drop nodes
    if (graph.nodes.length > 5000) {
      banner.style.display = 'block'
    } else {
      banner.style.display = 'none'
    }
  }

  applyAndRender(await fetchGraph())
  subscribeGraph((graph) => applyAndRender(graph))

  // Debug/e2e seam: the scene wraps a WebGL canvas, so interactions and styling
  // can't be asserted through the DOM. Expose the live graph + scene so the
  // localhost-only graph view can be inspected from the console or driven by
  // Playwright (read controls/accessors, project node coords, etc.).
  ;(window as unknown as { tuskScene?: unknown }).tuskScene = scene
  ;(window as unknown as { tuskGraph?: unknown }).tuskGraph = scene.instance
}

void boot()
