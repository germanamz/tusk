import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { initStatus } from './status'

// A controllable EventSource stand-in: jsdom ships none, and the test needs to
// drive open/change/error frames by hand.
class FakeEventSource {
  static instances: FakeEventSource[] = []

  url: string
  private handlers: Record<string, ((event: unknown) => void)[]> = {}

  constructor(url: string) {
    this.url = url
    FakeEventSource.instances.push(this)
  }

  addEventListener(type: string, handler: (event: unknown) => void): void {
    ;(this.handlers[type] ??= []).push(handler)
  }

  emit(type: string, data?: unknown): void {
    for (const handler of this.handlers[type] ?? []) {
      handler({ data: data === undefined ? undefined : JSON.stringify(data) })
    }
  }

  close(): void {}
}

function renderStatusChrome(): void {
  document.body.innerHTML = `
    <span id="mission-status" data-state="connecting">
      <span class="status-dot"></span>
      <span id="status-label">LINK</span>
      <span id="status-gen"></span>
    </span>
    <span id="conn"><span id="conn-label">CONNECTING</span></span>
    <span id="host"></span>
    <span id="loopback" hidden></span>
  `
}

beforeEach(() => {
  vi.stubGlobal('EventSource', FakeEventSource as unknown as typeof EventSource)
  FakeEventSource.instances = []
  renderStatusChrome()
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('initStatus', () => {
  it('subscribes to the read change stream and shows the loopback host', () => {
    initStatus()

    expect(FakeEventSource.instances).toHaveLength(1)
    expect(FakeEventSource.instances[0].url).toBe('/api/read/stream')
    expect(document.getElementById('host')?.textContent).toBe(location.host)
    expect(document.getElementById('loopback')?.hasAttribute('hidden')).toBe(false)
  })

  it('goes SYNCED on open and records the generation on the first change', () => {
    initStatus()
    const source = FakeEventSource.instances[0]

    source.emit('open')
    expect(document.getElementById('mission-status')?.dataset.state).toBe('synced')

    source.emit('change', { generation: 7, epoch: 1 })
    expect(document.getElementById('status-gen')?.textContent).toBe('GEN 7')
    expect(document.getElementById('mission-status')?.dataset.state).toBe('synced')
  })

  it('flags UPDATED when the generation advances', () => {
    initStatus()
    const source = FakeEventSource.instances[0]

    source.emit('change', { generation: 7 })
    source.emit('change', { generation: 8 })

    expect(document.getElementById('status-gen')?.textContent).toBe('GEN 8')
    expect(document.getElementById('mission-status')?.dataset.state).toBe('indexing')
    expect(document.getElementById('status-label')?.textContent).toBe('UPDATED')
  })

  it('goes OFFLINE on stream error', () => {
    initStatus()
    const source = FakeEventSource.instances[0]

    source.emit('open')
    source.emit('error')

    expect(document.getElementById('mission-status')?.dataset.state).toBe('offline')
    expect(document.getElementById('conn')?.dataset.state).toBe('offline')
    expect(document.getElementById('conn-label')?.textContent).toBe('OFFLINE')
  })
})
