import { fetchGraph } from './api'

async function boot(): Promise<void> {
  const graph = await fetchGraph()
  const el = document.getElementById('graph')!
  el.textContent = `tusk graph: ${graph.nodes.length} nodes, ${graph.edges.length} edges`
}

void boot()
