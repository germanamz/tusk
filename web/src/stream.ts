import type { Graph } from './api'

// subscribeGraph connects to the SSE stream and calls onGraph on each push.
// Returns a disposer. EventSource auto-reconnects on drop. Optional lifecycle
// callbacks surface connection state without coupling this module to the DOM:
// onConnect fires on (re)open; onDisconnect fires on error with `closed` true
// when the source is permanently shut (CLOSED) vs transiently reconnecting.
export function subscribeGraph(
  onGraph: (graph: Graph) => void,
  handlers: { onConnect?: () => void; onDisconnect?: (closed: boolean) => void } = {},
): () => void {
  const source = new EventSource('./api/graph/stream')
  source.addEventListener('graph', (event) => {
    onGraph(JSON.parse((event as MessageEvent).data) as Graph)
  })
  source.addEventListener('open', () => handlers.onConnect?.())
  source.addEventListener('error', () => {
    handlers.onDisconnect?.(source.readyState === EventSource.CLOSED)
  })
  return () => source.close()
}
