// SSE live reload: subscribeChanges wires an EventSource against
// GET /api/read/stream and invokes `onChange` whenever the server emits a
// "change" event. The event name is a locked cross-phase contract — the
// graph view's identical SSE mechanism (internal/webui.Hub) uses "graph" for
// its own stream, bookview's is "change" (internal/bookview/server.go:46,
// `EventName: "change"`) — the two are deliberately not unified.
//
// The payload (`event: change\ndata: {"generation":N,"epoch":M}`,
// internal/webui.Signal) is deliberately never parsed here: `onChange` is a
// bare signal ("something changed, go refetch"), not a diff callers apply
// against a generation number. main.ts always refetches Contents/the open
// node wholesale in response.
//
// Reconnection is the browser's job: a native EventSource retries on its own
// after a drop, using the server's default retry interval — there is
// deliberately no hand-rolled backoff/retry loop here.
export function subscribeChanges(onChange: () => void): () => void {
  const source = new EventSource('/api/read/stream')

  source.addEventListener('change', () => {
    onChange()
  })

  return () => {
    source.close()
  }
}
