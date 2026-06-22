import { fetchGraph, type Graph } from './api'
import { createScene } from './scene'
import { subscribeGraph } from './stream'
import { applyFacets, type FacetState } from './facets'
import { fetchNodeDetail, fetchSubunits } from './nodeapi'
import { renderPanel } from './panel'
import { EDGE_KIND_COLORS } from './encode'

let rawGraph: Graph = { generation: 0, epoch: 0, nodes: [], edges: [] }
let facetState: FacetState = { hiddenTypes: new Set(), hiddenKinds: new Set(), hideOrphans: false }

function buildFacetBar(graph: Graph, scene: ReturnType<typeof createScene>): void {
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
      scene.setGraph(applyFacets(rawGraph, facetState))
    })
    label.appendChild(cb)
    label.appendChild(document.createTextNode(' ' + t))
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
      scene.setGraph(applyFacets(rawGraph, facetState))
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
    scene.setGraph(applyFacets(rawGraph, facetState))
  })
  orphanLabel.appendChild(orphanCb)
  orphanLabel.appendChild(document.createTextNode('  Hide orphans'))
  bar.appendChild(orphanLabel)
}

function buildLegend(): void {
  const legend = document.getElementById('legend')!
  legend.innerHTML = '<strong>Edge kinds:</strong> '
  for (const [kind, color] of Object.entries(EDGE_KIND_COLORS)) {
    const item = document.createElement('span')
    const swatch = document.createElement('span')
    swatch.style.cssText = `display:inline-block;width:12px;height:12px;background:${color};border-radius:2px;margin-right:4px;vertical-align:middle`
    item.appendChild(swatch)
    item.appendChild(document.createTextNode(kind + '  '))
    legend.appendChild(item)
  }
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
  const scene = createScene(el)

  // Build static legend once
  buildLegend()

  // Node click → fetch detail, render panel
  scene.instance.onNodeClick((node: any) => {
    fetchNodeDetail(node.id)
      .then((detail) => {
        renderPanel(panelEl, detail, (neighborId) => {
          // Navigate to neighbor: fetch its detail
          fetchNodeDetail(neighborId)
            .then((nd) => renderPanel(panelEl, nd, () => {}))
            .catch(console.error)
          scene.focus([neighborId])
        })

        // Add expand button for sub-units
        const expandBtn = document.createElement('button')
        expandBtn.textContent = 'Expand sub-units'
        expandBtn.style.cssText = 'margin-top:8px;display:block'
        expandBtn.addEventListener('click', () => {
          fetchSubunits(node.id)
            .then((sub) => {
              rawGraph = mergeSubunits(rawGraph, sub)
              scene.setGraph(applyFacets(rawGraph, facetState))
              buildFacetBar(rawGraph, scene)
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
        scene.setGraph(applyFacets(rawGraph, facetState))
        buildFacetBar(rawGraph, scene)
      })
      .catch(console.error)
  })

  function applyAndRender(graph: Graph): void {
    rawGraph = graph
    buildFacetBar(graph, scene)
    scene.setGraph(applyFacets(graph, facetState))
  }

  applyAndRender(await fetchGraph())
  subscribeGraph((graph) => applyAndRender(graph))
}

void boot()
