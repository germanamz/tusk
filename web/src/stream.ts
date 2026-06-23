import type { Graph } from './api'

// subscribeGraph connects to the SSE stream and calls onGraph on each push.
// Returns a disposer. EventSource auto-reconnects on drop.
export function subscribeGraph(onGraph: (graph: Graph) => void): () => void {
  const source = new EventSource('./api/graph/stream')
  source.addEventListener('graph', (event) => {
    onGraph(JSON.parse((event as MessageEvent).data) as Graph)
  })
  return () => source.close()
}
