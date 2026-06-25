import { fetchGraph, type Graph } from './api'
import { createScene } from './scene'
import { subscribeGraph } from './stream'
import { applyFacets, type FacetState } from './facets'
import { fetchNodeDetail, fetchSubunits } from './nodeapi'
import { renderPanel } from './panel'
import { EDGE_KIND_COLORS, buildTypeColors, buildGroupColors } from './encode'
import { runSearch } from './search'

let rawGraph: Graph = { generation: 0, epoch: 0, nodes: [], edges: [], cluster: { by: 'type', huddle: false } }
let rawGroupColors: Map<string, string> = new Map()
let facetState: FacetState = { hiddenTypes: new Set(), hiddenKinds: new Set(), hideOrphans: false }

// colorsFor builds the type→color map for the facet bar's type-filter swatches.
// The facet bar filters by node type, so it always uses the type palette
// regardless of the active cluster lens.
const colorsFor = (g: Graph): Map<string, string> =>
  buildTypeColors([...new Set(g.nodes.map((n) => n.type))])

// groupColorsFor builds the group→color map for node colors and the legend.
// When graph.cluster.by === "type", group === type and the colors are
// pixel-identical to the old type-based coloring.
const groupColorsFor = (g: Graph): Map<string, string> =>
  buildGroupColors(g.nodes.map((n) => n.group))

function buildFacetBar(
  graph: Graph,
  scene: ReturnType<typeof createScene>,
  typeColors: Map<string, string>,
): void {
  const bar = document.getElementById('facets')!
  bar.innerHTML = ''

  // Node type checkboxes
  const types = [...new Set(graph.nodes.map((n) => n.type))].sort()
  const typeLabel = document.createElement('span')
  typeLabel.textContent = 'Types: '
  bar.appendChild(typeLabel)

  for (const t of types) {
    const label = document.createElement('label')
    const cb = document.createElement('input')
    cb.type = 'checkbox'
    cb.checked = !facetState.hiddenTypes.has(t)
    cb.addEventListener('change', () => {
      if (cb.checked) facetState.hiddenTypes.delete(t)
      else facetState.hiddenTypes.add(t)
      scene.setGraph(applyFacets(rawGraph, facetState), rawGroupColors)
    })
    label.appendChild(cb)
    // A small swatch in the type's hue makes the filter bar self-documenting.
    const swatch = document.createElement('span')
    swatch.style.cssText = `display:inline-block;width:10px;height:10px;background:${typeColors.get(t) ?? '#888888'};border-radius:2px;margin:0 4px 0 4px;vertical-align:middle`
    label.appendChild(swatch)
    label.appendChild(document.createTextNode(t))
    bar.appendChild(label)
  }

  // Edge kind checkboxes
  const kinds = [...new Set(graph.edges.map((e) => e.kind))].sort()
  const kindLabel = document.createElement('span')
  kindLabel.textContent = '  Kinds: '
  bar.appendChild(kindLabel)

  for (const k of kinds) {
    const label = document.createElement('label')
    const cb = document.createElement('input')
    cb.type = 'checkbox'
    cb.checked = !facetState.hiddenKinds.has(k)
    cb.addEventListener('change', () => {
      if (cb.checked) facetState.hiddenKinds.delete(k)
      else facetState.hiddenKinds.add(k)
      scene.setGraph(applyFacets(rawGraph, facetState), rawGroupColors)
    })
    label.appendChild(cb)
    label.appendChild(document.createTextNode(' ' + k))
    bar.appendChild(label)
  }

  // Orphans toggle
  const orphanLabel = document.createElement('label')
  const orphanCb = document.createElement('input')
  orphanCb.type = 'checkbox'
  orphanCb.checked = facetState.hideOrphans
  orphanCb.addEventListener('change', () => {
    facetState.hideOrphans = orphanCb.checked
    scene.setGraph(applyFacets(rawGraph, facetState), rawGroupColors)
  })
  orphanLabel.appendChild(orphanCb)
  orphanLabel.appendChild(document.createTextNode('  Hide orphans'))
  bar.appendChild(orphanLabel)
}

function buildLegend(graph: Graph, groupColors: Map<string, string>): void {
  const legend = document.getElementById('legend')!
  legend.innerHTML = ''

  const swatchSpan = (color: string): HTMLSpanElement => {
    const swatch = document.createElement('span')
    swatch.style.cssText = `display:inline-block;width:12px;height:12px;background:${color};border-radius:2px;margin-right:4px;vertical-align:middle`
    return swatch
  }

  // Color section: one colored swatch + label per distinct group value, matching
  // the hues the scene paints. The header reflects the active lens dimension so
  // "Color (team):" appears when by = "property" and property = "team".
  // When by = "type", this reproduces today's type legend exactly.
  const by = graph.cluster?.by ?? 'type'
  const colorLabel = by === 'type' ? 'Color: ' : `Color (${by === 'property' ? (graph.cluster.property ?? 'property') : by}): `
  const colorHeader = document.createElement('strong')
  colorHeader.textContent = colorLabel
  legend.appendChild(colorHeader)
  for (const group of [...new Set(graph.nodes.map((n) => n.group))].sort()) {
    const item = document.createElement('span')
    item.appendChild(swatchSpan(groupColors.get(group) ?? '#888888'))
    item.appendChild(document.createTextNode(group + '  '))
    legend.appendChild(item)
  }

  // Edge kinds section (unchanged contents).
  const kindsHeader = document.createElement('strong')
  kindsHeader.style.cssText = 'border-left:1px solid #2a2d3a;padding-left:8px;margin-left:4px'
  kindsHeader.textContent = 'Edge kinds: '
  legend.appendChild(kindsHeader)
  for (const [kind, color] of Object.entries(EDGE_KIND_COLORS)) {
    const item = document.createElement('span')
    item.appendChild(swatchSpan(color))
    item.appendChild(document.createTextNode(kind + '  '))
    legend.appendChild(item)
  }

  // Importance hint: explains the size/brightness channel.
  const importance = document.createElement('span')
  importance.style.cssText = 'color:#888;border-left:1px solid #2a2d3a;padding-left:8px;margin-left:4px'
  importance.textContent = 'size & brightness = connections (in + out)'
  legend.appendChild(importance)

  // Controls hint (replaces the library's hidden nav info). Advertises the
  // Alt+drag pan alongside the default rotate/zoom.
  const controls = document.createElement('span')
  controls.style.cssText = 'color:#888;border-left:1px solid #2a2d3a;padding-left:8px;margin-left:4px'
  controls.textContent = 'drag rotate · alt+drag pan · scroll zoom'
  legend.appendChild(controls)
}

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

  // Search form
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
              const typeColors = colorsFor(rawGraph)
              rawGroupColors = groupColorsFor(rawGraph)
              scene.setGraph(applyFacets(rawGraph, facetState), rawGroupColors)
              buildFacetBar(rawGraph, scene, typeColors)
              buildLegend(rawGraph, rawGroupColors)
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
        const typeColors = colorsFor(rawGraph)
        rawGroupColors = groupColorsFor(rawGraph)
        scene.setGraph(applyFacets(rawGraph, facetState), rawGroupColors)
        buildFacetBar(rawGraph, scene, typeColors)
        buildLegend(rawGraph, rawGroupColors)
      })
      .catch(console.error)
  })

  // Click empty space → clear the selection highlight and the panel.
  scene.instance.onBackgroundClick(() => {
    scene.select(null)
    panelEl.innerHTML = ''
  })

  function applyAndRender(graph: Graph): void {
    rawGraph = graph
    const typeColors = colorsFor(graph)
    rawGroupColors = groupColorsFor(graph)
    buildFacetBar(graph, scene, typeColors)
    buildLegend(graph, rawGroupColors)
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
