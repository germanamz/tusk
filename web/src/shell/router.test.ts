import { beforeEach, describe, expect, it, vi } from 'vitest'

// Mock the lazy view modules so navigating never pulls in three.js or mermaid;
// the router only needs their mount/unmount shape.
vi.mock('../graph', () => ({ mount: vi.fn(), unmount: vi.fn() }))
vi.mock('../read', () => ({ mount: vi.fn(), unmount: vi.fn() }))

import * as graph from '../graph'
import * as read from '../read'
import { createRouter, viewForPath } from './router'

beforeEach(() => {
  vi.clearAllMocks()
  history.replaceState(null, '', '/')
})

describe('viewForPath', () => {
  it('maps / and unknown paths to the graph, /read to the reader', () => {
    expect(viewForPath('/')).toBe('graph')
    expect(viewForPath('')).toBe('graph')
    expect(viewForPath('/anything')).toBe('graph')
    expect(viewForPath('/read')).toBe('read')
    expect(viewForPath('/read/')).toBe('read')
  })
})

describe('createRouter', () => {
  it('mounts the target view into the viewport and pushes its path', async () => {
    const viewport = document.createElement('section')
    const onChange = vi.fn()
    const router = createRouter(viewport, onChange)

    await router.navigate('read')

    expect(read.mount).toHaveBeenCalledTimes(1)
    expect(read.mount).toHaveBeenCalledWith(viewport)
    expect(onChange).toHaveBeenLastCalledWith('read')
    expect(router.current()).toBe('read')
    expect(location.pathname).toBe('/read')
  })

  it('unmounts the current view before mounting the next', async () => {
    const viewport = document.createElement('section')
    const router = createRouter(viewport, vi.fn())

    await router.navigate('read')
    await router.navigate('graph')

    expect(read.unmount).toHaveBeenCalledTimes(1)
    expect(graph.mount).toHaveBeenCalledTimes(1)
    expect(location.pathname).toBe('/')
  })

  it('does not remount when navigating to the already-current view', async () => {
    const viewport = document.createElement('section')
    const router = createRouter(viewport, vi.fn())

    await router.navigate('read')
    expect(read.mount).toHaveBeenCalledTimes(1)

    // A popstate within the reader (Back/Forward over its own #/node hashes)
    // resolves to the same view; it must not tear the view down and rebuild it.
    await router.navigate('read', { push: false })

    expect(read.mount).toHaveBeenCalledTimes(1)
    expect(read.unmount).not.toHaveBeenCalled()
  })

  it('does not push history when push is false (initial mount / popstate)', async () => {
    history.replaceState(null, '', '/read')
    const viewport = document.createElement('section')
    const router = createRouter(viewport, vi.fn())

    const before = history.length
    await router.navigate('read', { push: false })

    expect(read.mount).toHaveBeenCalledTimes(1)
    expect(history.length).toBe(before)
    expect(location.pathname).toBe('/read')
  })
})
