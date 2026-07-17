import './graph.css'
import { fetchGraph, fetchEmbeddings, type Graph, type EmbeddingsResponse, type SubunitGraph } from './api'
import { createScene, type Scene } from './scene'
import { subscribeGraph } from './stream'
import { applyFacets, type FacetState } from './facets'
import { fetchNodeDetail, fetchSubunits } from './nodeapi'
import { mergeSubunits, reapplyExpanded } from './subunits'
import { renderPanel } from './panel'
import { buildGroupColors } from './encode'
import { runSearch } from './search'
import { createControls } from './controls'
import { onThemeChange } from '../theme/theme'

// The graph scaffold — the #main-area subtree lifted from the standalone graph
// page. mount() injects this into its container and the boot code below looks
// each piece up by id (scoped to the container). #search-form ships inside
// #controls because controls.ts relocates it into the drawer header on boot;
// keeping it here means a container-clear never wipes it out from under that.
const SCAFFOLD = `
  <div id="main-area">
    <div id="controls">
      <form id="search-form">
        <input id="search" type="search" placeholder="search nodes…" autocomplete="off" />
        <button type="submit">Search</button>
      </form>
    </div>
    <div id="search-msg"></div>
    <div id="banner">large graph — apply a filter</div>
    <div id="layout-status">computing layout…</div>
    <div id="conn-status"></div>
    <div id="graph"></div>
    <aside id="panel"></aside>
  </div>
`

// groupColorsFor builds the group→color map for node colors and the legend.
// When graph.cluster.by === "type", group === type and the colors are
// pixel-identical to the old type-based coloring.
const groupColorsFor = (g: Graph): Map<string, string> =>
  buildGroupColors(g.nodes.map((n) => n.group))

// Teardown handles captured by mount() and released by unmount(). Only one view
// is live at a time (the router serializes navigations), so module scope holds
// exactly one graph's worth of state.
let activeScene: Scene | null = null
let activeWorker: Worker | null = null
let unsubscribeStream: (() => void) | null = null
let unsubscribeTheme: (() => void) | null = null
let activeContainer: HTMLElement | null = null

// Per-mount token. Bumped on every mount and every unmount so an in-flight boot
// (awaiting the first fetch) can tell it has been superseded and bail before
// touching a torn-down scene.
let mountToken = 0

export function mount(container: HTMLElement): void {
  const myToken = ++mountToken

  container.innerHTML = SCAFFOLD
  activeContainer = container

  // State for this mount. Lives in the closure so a re-mount starts fresh.
  let rawGraph: Graph = { generation: 0, epoch: 0, nodes: [], edges: [], cluster: { by: 'type', huddle: false, hull: false } }
  let rawGroupColors: Map<string, string> = new Map()
  const facetState: FacetState = { hiddenTypes: new Set(), hiddenKinds: new Set(), hideOrphans: false, hiddenGroups: new Set() }

  // Sub-unit expansions, keyed by the expanded parent's node id. SSE snapshots
  // never carry sub-units, so applyAndRender re-folds these into each fresh
  // snapshot (reapplyExpanded) to keep expansions visible across pushes.
  const expandedSubunits = new Map<string, SubunitGraph>()

  // Generation counter for layout requests. Incremented at the start of every
  // applySemanticLayout() and applyStructureLayout() call. Checked after each await
  // in the async semantic path so a superseded in-flight projection bails out before
  // touching the scene, keeping the radio and layout consistent with the last action.
  let layoutRequestGen = 0
  // embeddingsCache holds the latest /api/graph/embeddings fetch.
  let embeddingsCache: EmbeddingsResponse | null = null

  // Whether the vault has any embeddings — drives the Semantic toggle's enabled
  // state. Set by the boot-time prefetch and refreshed on each Semantic toggle.
  let embeddingsAvailable = false

  // Projection cache keyed by EmbeddingsResponse.signature. While the embedded
  // content is unchanged, re-entering Semantic reuses projectedCoords and skips
  // the UMAP worker entirely; the ~2s SSE re-snapshots never reproject.
  let projectedSignature: string | null = null
  let projectedCoords: Map<string, { x: number; y: number; z: number }> | null = null

  const el = container.querySelector<HTMLElement>('#graph')!
  const panelEl = container.querySelector<HTMLElement>('#panel')!
  const searchMsg = container.querySelector<HTMLElement>('#search-msg')!
  const banner = container.querySelector<HTMLElement>('#banner')!
  const layoutStatus = container.querySelector<HTMLElement>('#layout-status')!
  const connStatus = container.querySelector<HTMLElement>('#conn-status')!
  const scene = createScene(el)
  activeScene = scene

  // Transient layout-status overlay helpers. Shows "computing layout…" while the
  // worker runs and surfaces hints/errors; hidden by default.
  const showStatus = (text: string): void => {
    layoutStatus.textContent = text
    layoutStatus.style.display = 'block'
  }
  const hideStatus = (): void => {
    layoutStatus.style.display = 'none'
  }

  // Connection-status overlay helpers — mirror showStatus/hideStatus but drive the
  // bottom-center #conn-status pill. Surface when the SSE stream drops so a stale
  // graph isn't mistaken for a live one; hidden once the stream (re)connects.
  const showConn = (text: string): void => {
    connStatus.textContent = text
    connStatus.style.display = 'block'
  }
  const hideConn = (): void => {
    connStatus.style.display = 'none'
  }

  // The UMAP projection runs off the main thread. Spawned once; reused for every
  // semantic-layout request. Vite bundles the worker chunk from this new URL.
  const layoutWorker = new Worker(new URL('./layout.worker.ts', import.meta.url), { type: 'module' })
  activeWorker = layoutWorker

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
    onLayoutModeChange: (mode) => {
      if (mode === 'semantic') void applySemanticLayout()
      else applyStructureLayout()
    },
    hasEmbeddings: () => embeddingsAvailable,
  })

  // Live theming: on a light/dark flip, re-read the scene's graph tokens and
  // re-run the color encode so nodes, links, and the canvas background all
  // update. rerender() re-pushes the (theme-refreshed) node/link colors through
  // setGraph; refreshTheme() re-tints the background and cached hues first.
  unsubscribeTheme = onThemeChange(() => {
    scene.refreshTheme()
    rerender()
  })

  // Prefetch embeddings to learn whether Semantic layout is available before the
  // user opens the drawer. Only the availability boolean is used here; the cache
  // store is omitted because applySemanticLayout() always re-fetches before
  // reading embeddingsCache. A failure just leaves Semantic disabled.
  fetchEmbeddings()
    .then((resp) => {
      embeddingsAvailable = Object.keys(resp.vectors).length > 0
      controls.refreshLayoutAvailability()
    })
    .catch((err) => console.error('embeddings prefetch failed:', err))

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

  // enterSemantic pins the given coords, flips the scene to semantic mode, and
  // reframes the camera onto the new cloud. setLayoutMode no longer reheats, so
  // the trailing rerender() owns the single reheat; zoomToFit fits the freshly
  // pinned (final) positions so the cloud fills the view rather than sitting
  // off-center and tiny.
  const enterSemantic = (coords: Map<string, { x: number; y: number; z: number }>): void => {
    scene.setSemanticCoords(coords)
    scene.setLayoutMode('semantic')
    rerender()
    scene.instance.zoomToFit(600, 40)
    hideStatus()
  }

  // applySemanticLayout: refresh embeddings, project them (or reuse a cached
  // projection when the content signature is unchanged), pin the result, and
  // switch the scene to semantic mode. Stays in Structure on empty embeddings or
  // any failure.
  async function applySemanticLayout(): Promise<void> {
    const myGen = ++layoutRequestGen
    if (semanticInFlight) return
    semanticInFlight = true
    try {
      // Re-fetch so the signature reflects the current vault content (cheap over
      // loopback). The signature is the projection-cache key below.
      embeddingsCache = await fetchEmbeddings()
      // Bail if a Structure click (or newer Semantic) superseded this request while
      // we were waiting for the fetch. The finally block still clears semanticInFlight.
      if (myGen !== layoutRequestGen) return
      embeddingsAvailable = Object.keys(embeddingsCache.vectors).length > 0
      controls.refreshLayoutAvailability()

      const vectors = embeddingsCache.vectors
      const ids = Object.keys(vectors)
      if (ids.length === 0) {
        controls.resetLayoutToggle()
        showStatus('no embeddings — run reindex with an embedding provider')
        setTimeout(hideStatus, 5000)
        return // stay in Structure
      }

      // Cache hit: the embedded content is unchanged since the last projection,
      // so reuse the stored coords and skip the UMAP worker — re-entering
      // Semantic is instant. No status banner on a cache hit.
      if (embeddingsCache.signature === projectedSignature && projectedCoords !== null) {
        enterSemantic(projectedCoords)
        return
      }

      // Cache miss — projection is needed. Show the computing banner only now.
      showStatus('computing layout…')
      const reply = await projectInWorker(
        ids,
        ids.map((id) => vectors[id]),
      )
      // Bail again after the (potentially long) worker projection — a Structure
      // click during UMAP must not let the result overwrite the current layout.
      if (myGen !== layoutRequestGen) return
      const coords = new Map<string, { x: number; y: number; z: number }>()
      reply.ids.forEach((id, i) => coords.set(id, reply.positions[i]))
      projectedSignature = embeddingsCache.signature
      projectedCoords = coords
      enterSemantic(coords)
    } catch (err) {
      controls.resetLayoutToggle()
      showStatus(`semantic layout failed: ${String(err)}`)
      setTimeout(hideStatus, 5000)
      // stay in Structure
    } finally {
      semanticInFlight = false
    }
  }

  // applyStructureLayout: restore the force layout. The scene drops the pins and
  // rerender() re-registers huddle (if enabled) via setGraph's Structure gate;
  // zoomToFit reframes the camera back onto the force layout after the flip.
  // Bumping layoutRequestGen cancels any in-flight semantic projection so it
  // cannot flip the scene back to Semantic after this call returns.
  function applyStructureLayout(): void {
    ++layoutRequestGen
    hideStatus()
    scene.setLayoutMode('structure')
    rerender()
    scene.instance.zoomToFit(600, 40)
  }

  // Search form — binds to #search-form / #search which live in the #controls
  // drawer header (a stable container never innerHTML-cleared). This binding
  // holds across all live snapshots because controls.ts never rebuilds the header.
  const searchForm = container.querySelector<HTMLFormElement>('#search-form')!
  const searchInput = container.querySelector<HTMLInputElement>('#search')!
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

  // navigate selects a node (highlight + camera fly-in) and renders its detail
  // panel. The neighbor buttons are wired back to navigate itself so neighbor
  // hops recurse indefinitely — the old handler rendered the next panel with a
  // no-op onNeighbor and dead-ended after one hop. The expand-sub-units button is
  // re-added on every panel (keyed on the current id) so it is reachable from any
  // node, not just the first click.
  function navigate(id: string): void {
    scene.select(id)
    fetchNodeDetail(id)
      .then((detail) => {
        renderPanel(panelEl, detail, navigate)

        // Add expand button for sub-units
        const expandBtn = document.createElement('button')
        expandBtn.textContent = 'Expand sub-units'
        expandBtn.style.cssText = 'margin-top:8px;display:block'
        expandBtn.addEventListener('click', () => {
          fetchSubunits(id)
            .then((sub) => {
              expandedSubunits.set(id, sub)
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
  }

  // Node click → navigate to it (select + render detail panel).
  scene.instance.onNodeClick((node: any) => navigate(node.id))

  // Right-click → expand sub-units inline
  scene.instance.onNodeRightClick((node: any) => {
    fetchSubunits(node.id)
      .then((sub) => {
        expandedSubunits.set(node.id, sub)
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
    // Re-fold any cached sub-unit expansions back in — SSE snapshots never carry
    // sub-units, so without this each push would wipe the user's expansions.
    rawGraph = reapplyExpanded(graph, expandedSubunits)
    rawGroupColors = groupColorsFor(rawGraph)
    controls.update(rawGraph, rawGroupColors)
    scene.setGraph(applyFacets(rawGraph, facetState), rawGroupColors)
    // Scale guardrail: warn when graph is very large but never drop nodes
    if (rawGraph.nodes.length > 5000) {
      banner.style.display = 'block'
    } else {
      banner.style.display = 'none'
    }
  }

  // Initial paint + live subscribe. Fire-and-forget (mount is synchronous, like
  // the standalone page's boot); the token guard bails if unmount ran while the
  // first fetch was in flight so a torn-down scene is never touched.
  void (async () => {
    let initial: Graph
    try {
      initial = await fetchGraph()
    } catch (err) {
      console.error('initial graph fetch failed:', err)
      return
    }
    if (myToken !== mountToken) return
    applyAndRender(initial)
    if (myToken !== mountToken) return
    unsubscribeStream = subscribeGraph((graph) => applyAndRender(graph), {
      onConnect: hideConn,
      onDisconnect: (closed) =>
        showConn(closed ? 'disconnected from tusk graph' : 'connection lost — reconnecting…'),
    })
  })()

  // Debug/e2e seam: the scene wraps a WebGL canvas, so interactions and styling
  // can't be asserted through the DOM. Expose the live graph + scene so the
  // localhost-only graph view can be inspected from the console or driven by
  // Playwright (read controls/accessors, project node coords, etc.).
  ;(window as unknown as { tuskScene?: unknown }).tuskScene = scene
  ;(window as unknown as { tuskGraph?: unknown }).tuskGraph = scene.instance
  ;(window as unknown as { tuskNavigate?: (id: string) => void }).tuskNavigate = navigate
}

export function unmount(): void {
  // Invalidate any in-flight boot so its post-fetch continuation bails.
  mountToken++

  if (unsubscribeStream) {
    unsubscribeStream()
    unsubscribeStream = null
  }
  if (unsubscribeTheme) {
    unsubscribeTheme()
    unsubscribeTheme = null
  }
  if (activeWorker) {
    activeWorker.terminate()
    activeWorker = null
  }
  if (activeScene) {
    activeScene.dispose()
    activeScene = null
  }

  const globals = window as unknown as {
    tuskScene?: unknown
    tuskGraph?: unknown
    tuskNavigate?: unknown
  }
  delete globals.tuskScene
  delete globals.tuskGraph
  delete globals.tuskNavigate

  if (activeContainer) {
    activeContainer.replaceChildren()
    activeContainer = null
  }
}
