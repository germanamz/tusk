import { afterEach, expect, test, vi } from 'vitest'
import { subscribeChanges } from './stream'

// jsdom has no real EventSource implementation, so every test here installs
// a small fake on globalThis before calling subscribeChanges — the same
// approach the task brief's own example uses.
afterEach(() => {
  delete (globalThis as { EventSource?: unknown }).EventSource
})

test('a "change" event invokes the callback', () => {
  const listeners: Record<string, (e: unknown) => void> = {}
  ;(globalThis as unknown as { EventSource: unknown }).EventSource = class {
    addEventListener(type: string, handler: (e: unknown) => void) {
      listeners[type] = handler
    }
    close() {}
  }

  const cb = vi.fn()
  subscribeChanges(cb)
  listeners['change']({ data: '{"generation":2}' })

  expect(cb).toHaveBeenCalledTimes(1)
})

test('subscribeChanges opens against ./api/stream', () => {
  const opened: string[] = []
  ;(globalThis as unknown as { EventSource: unknown }).EventSource = class {
    constructor(url: string) {
      opened.push(url)
    }
    addEventListener() {}
    close() {}
  }

  subscribeChanges(() => {})
  expect(opened).toEqual(['./api/stream'])
})

test('the returned unsubscribe closes the underlying EventSource', () => {
  const close = vi.fn()
  ;(globalThis as unknown as { EventSource: unknown }).EventSource = class {
    addEventListener() {}
    close = close
  }

  const unsubscribe = subscribeChanges(() => {})
  expect(close).not.toHaveBeenCalled()

  unsubscribe()
  expect(close).toHaveBeenCalledTimes(1)
})
