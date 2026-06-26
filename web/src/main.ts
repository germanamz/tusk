import { fetchGraph, fetchEmbeddings, type Graph, type EmbeddingsResponse } from './api'
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

// Layout-mode orchestration state. Structure is the default; Semantic is reached
// via the temporary console seam below (the real UI toggle lands in Phase 3).
// embeddingsCache holds the one-time /api/embeddings fetch (its signature keys a
// Phase 3 cache).
let layoutMode: 'structure' | 'semantic' = 'structure'
let embeddingsCache: EmbeddingsResponse | null = null

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
  const layoutStatus = document.getElementById('layout-status')!
  const scene = createScene(el)

  // Transient layout-status overlay helpers. Shows "computing layout…" while the
  // worker runs and surfaces hints/errors; hidden by default.
  const showStatus = (text: string): void => {
    layoutStatus.textContent = text
    layoutStatus.style.display = 'block'
  }
  const hideStatus = (): void => {
    layoutStatus.style.display = 'none'
  }

  // The UMAP projection runs off the main thread. Spawned once; reused for every
  // semantic-layout request. Vite bundles the worker chunk from this new URL.
  const layoutWorker = new Worker(new URL('./layout.worker.ts', import.meta.url), { type: 'module' })

  // Re-apply the current data through the scene. This is the former inline
  // onFilterChange body, factored out so a mode flip can re-run setGraph (which
  // re-evaluates huddle gating + pins for the active mode).
  const rerender = (): void => scene.setGraph(applyFacets(rawGraph, facetState), rawGroupColors)

  // Build the controls drawer once; diff-update it on each snapshot.
  const controls = createControls({
    scene,
    getRawGraph: () => rawGraph,
    getGroupColors: () => rawGroupColors,
    facetState,
    onFilterChange: rerender,
  })

  // projectInWorker posts one request to the layout worker and resolves with its
  // reply (or rejects on a worker-reported error). A fresh per-request listener
  // pair is registered and torn down so replies never cross requests.
  const projectInWorker = (
    ids: string[],
    vectors: number[][],
  ): Promise<{ ids: string[]; positions: { x: number; y: number; z: number }[] }> =>
    new Promise((resolve, reject) => {
      const cleanup = (): void => {
        layoutWorker.removeEventListener('message', onMessage)
        layoutWorker.removeEventListener('error', onError)
      }
      const onMessage = (ev: MessageEvent): void => {
        cleanup()
        const data = ev.data as {
          ids?: string[]
          positions?: { x: number; y: number; z: number }[]
          error?: string
        }
        if (data.error) {
          reject(new Error(data.error))
          return
        }
        resolve({ ids: data.ids ?? [], positions: data.positions ?? [] })
      }
      const onError = (ev: ErrorEvent): void => {
        cleanup()
        reject(new Error(ev.message || 'layout worker error'))
      }
      layoutWorker.addEventListener('message', onMessage)
      layoutWorker.addEventListener('error', onError)
      layoutWorker.postMessage({ ids, vectors })
    })

  // Single in-flight guard so repeated triggers don't stack worker runs.
  let semanticInFlight = false

  // applySemanticLayout: fetch embeddings (once), project them in the worker,
  // pin the result, and switch the scene to semantic mode. Stays in Structure on
  // empty embeddings or any failure.
  async function applySemanticLayout(): Promise<void> {
    if (semanticInFlight) return
    semanticInFlight = true
    showStatus('computing layout…')
    try {
      if (embeddingsCache === null) embeddingsCache = await fetchEmbeddings()
      const vectors = embeddingsCache.vectors
      const ids = Object.keys(vectors)
      if (ids.length === 0) {
        showStatus('no embeddings — run reindex with an embedding provider')
        setTimeout(hideStatus, 5000)
        return // stay in Structure
      }
      const reply = await projectInWorker(
        ids,
        ids.map((id) => vectors[id]),
      )
      const coords = new Map<string, { x: number; y: number; z: number }>()
      reply.ids.forEach((id, i) => coords.set(id, reply.positions[i]))
      scene.setSemanticCoords(coords)
      scene.setLayoutMode('semantic')
      layoutMode = 'semantic'
      rerender()
      hideStatus()
    } catch (err) {
      showStatus(`semantic layout failed: ${String(err)}`)
      setTimeout(hideStatus, 5000)
      // stay in Structure
    } finally {
      semanticInFlight = false
    }
  }

  // applyStructureLayout: restore the force layout. The scene drops the pins and
  // rerender() re-registers huddle (if enabled) via setGraph's Structure gate.
  function applyStructureLayout(): void {
    scene.setLayoutMode('structure')
    layoutMode = 'structure'
    rerender()
  }

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

  // Temporary debug trigger [BRIDGE — removed in Phase 3]. Lets Semantic mode be
  // exercised from the console before the Phase 3 drawer toggle exists; Phase 3
  // deletes both lines and drives the same two functions from the toggle.
  ;(window as any).tuskApplySemantic = () => {
    void applySemanticLayout()
  }
  ;(window as any).tuskApplyStructure = () => {
    applyStructureLayout()
  }
}

void boot()
