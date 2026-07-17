import { describe, it, expect, vi, afterEach } from 'vitest'
import type { Graph } from './api'
import { subscribeGraph } from './stream'

// A single-threaded fake EventSource that records listeners and lets a test
// synchronously drive the open / error / graph lifecycle. Mirrors api.test.ts's
// vi.stubGlobal idiom; exposes the static readyState constants stream.ts reads to
// tell a permanent close (CLOSED) from a transient reconnect (CONNECTING).
class FakeEventSource {
  static readonly CONNECTING = 0
  static readonly OPEN = 1
  static readonly CLOSED = 2
  static last: FakeEventSource | null = null

  url: string
  readyState = FakeEventSource.CONNECTING
  closed = false
  private listeners = new Map<string, Array<(ev: unknown) => void>>()

  constructor(url: string) {
    this.url = url
    FakeEventSource.last = this
  }

  addEventListener(type: string, fn: (ev: unknown) => void): void {
    const arr = this.listeners.get(type) ?? []
    arr.push(fn)
    this.listeners.set(type, arr)
  }

  close(): void {
    this.closed = true
    this.readyState = FakeEventSource.CLOSED
  }

  // Test helper: synchronously dispatch to every listener registered for `type`.
  emit(type: string, ev?: unknown): void {
    for (const fn of [...(this.listeners.get(type) ?? [])]) fn(ev)
  }
}

const sample: Graph = {
  generation: 1,
  epoch: 0,
  nodes: [],
  edges: [],
  cluster: { by: 'type', huddle: false, hull: false },
}

describe('subscribeGraph', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
    FakeEventSource.last = null
  })

  it('forwards graph payloads (back-compat single-arg call)', () => {
    vi.stubGlobal('EventSource', FakeEventSource)
    const seen: Graph[] = []
    subscribeGraph((g) => seen.push(g))
    FakeEventSource.last!.emit('graph', { data: JSON.stringify(sample) })
    expect(seen).toHaveLength(1)
    expect(seen[0].generation).toBe(1)
  })

  it('fires onConnect on open', () => {
    vi.stubGlobal('EventSource', FakeEventSource)
    let connects = 0
    subscribeGraph(() => {}, {
      onConnect: () => {
        connects++
      },
    })
    FakeEventSource.last!.readyState = FakeEventSource.OPEN
    FakeEventSource.last!.emit('open')
    expect(connects).toBe(1)
  })

  it('reports a transient reconnect (readyState CONNECTING) as not-closed', () => {
    vi.stubGlobal('EventSource', FakeEventSource)
    const closedSeen: boolean[] = []
    subscribeGraph(() => {}, { onDisconnect: (closed) => closedSeen.push(closed) })
    FakeEventSource.last!.readyState = FakeEventSource.CONNECTING
    FakeEventSource.last!.emit('error')
    expect(closedSeen).toEqual([false])
  })

  it('reports a permanent close (readyState CLOSED) as closed', () => {
    vi.stubGlobal('EventSource', FakeEventSource)
    const closedSeen: boolean[] = []
    subscribeGraph(() => {}, { onDisconnect: (closed) => closedSeen.push(closed) })
    FakeEventSource.last!.readyState = FakeEventSource.CLOSED
    FakeEventSource.last!.emit('error')
    expect(closedSeen).toEqual([true])
  })

  it('disposer closes the source', () => {
    vi.stubGlobal('EventSource', FakeEventSource)
    const dispose = subscribeGraph(() => {})
    const src = FakeEventSource.last!
    expect(src.closed).toBe(false)
    dispose()
    expect(src.closed).toBe(true)
    expect(src.readyState).toBe(FakeEventSource.CLOSED)
  })
})
