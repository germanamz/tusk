import { JSDOM } from 'jsdom'
import { beforeEach, describe, expect, test, vi } from 'vitest'
import Panzoom from '@panzoom/panzoom'
import { applyDiagramZoom } from './diagramzoom'

// Panzoom's real transforms depend on layout (getBoundingClientRect), which jsdom
// does not compute — so we mock the library and assert the GLUE: that our module
// wires the controls, the ctrl-gated wheel, the dblclick, and the teardown to the
// right panzoom methods. The actual zoom/pan behavior is covered by the Playwright
// e2e in a real browser.
vi.mock('@panzoom/panzoom', () => ({
  default: vi.fn(() => ({
    zoomIn: vi.fn(),
    zoomOut: vi.fn(),
    reset: vi.fn(),
    zoomWithWheel: vi.fn(),
    destroy: vi.fn(),
  })),
}))

const PanzoomMock = vi.mocked(Panzoom)

// A rendered mermaid block: a <pre class="mermaid"> whose fence was replaced by an
// <svg> — the state applyDiagramZoom is called against, after mermaid.run().
function renderedBlock(): HTMLElement {
  const dom = new JSDOM('<!doctype html><body></body>')
  const doc = dom.window.document
  // Make WheelEvent / the document globally available for the module under test.
  ;(globalThis as unknown as { document: Document }).document = doc as unknown as Document
  const block = doc.createElement('pre')
  block.className = 'mermaid'
  block.appendChild(doc.createElementNS('http://www.w3.org/2000/svg', 'svg'))
  doc.body.appendChild(block)
  return block as unknown as HTMLElement
}

function lastInstance() {
  return PanzoomMock.mock.results[PanzoomMock.mock.results.length - 1].value
}

beforeEach(() => {
  PanzoomMock.mockClear()
})

describe('applyDiagramZoom', () => {
  test('marks the block zoomable and constructs panzoom on its svg', () => {
    const block = renderedBlock()
    const svg = block.querySelector('svg')

    applyDiagramZoom(block)

    expect(block.classList.contains('zoomable-diagram')).toBe(true)
    expect(PanzoomMock).toHaveBeenCalledTimes(1)
    expect(PanzoomMock.mock.calls[0][0]).toBe(svg)
    // contain:'outside' keeps the diagram covering its viewport so it can't be
    // panned fully out of view.
    expect(PanzoomMock.mock.calls[0][1]).toMatchObject({ contain: 'outside' })
  })

  test('renders a control cluster whose buttons drive zoomIn / zoomOut / reset', () => {
    const block = renderedBlock()
    applyDiagramZoom(block)
    const instance = lastInstance()

    const buttons = block.querySelectorAll<HTMLButtonElement>('.diagram-zoom-controls button')
    expect(buttons.length).toBe(3)

    const byLabel = (label: string) =>
      Array.from(buttons).find((button) => button.getAttribute('aria-label') === label)

    byLabel('Zoom in')!.click()
    expect(instance.zoomIn).toHaveBeenCalledTimes(1)

    byLabel('Zoom out')!.click()
    expect(instance.zoomOut).toHaveBeenCalledTimes(1)

    byLabel('Reset zoom')!.click()
    expect(instance.reset).toHaveBeenCalledTimes(1)
  })

  test('double-click resets the zoom', () => {
    const block = renderedBlock()
    applyDiagramZoom(block)
    const instance = lastInstance()

    block.dispatchEvent(new block.ownerDocument.defaultView!.MouseEvent('dblclick', { bubbles: true }))
    expect(instance.reset).toHaveBeenCalledTimes(1)
  })

  test('ctrl-wheel zooms and is prevented; a plain wheel is left to scroll the page', () => {
    const block = renderedBlock()
    applyDiagramZoom(block)
    const instance = lastInstance()
    const view = block.ownerDocument.defaultView!

    const plain = new view.WheelEvent('wheel', { ctrlKey: false, cancelable: true, bubbles: true })
    block.dispatchEvent(plain)
    expect(instance.zoomWithWheel).not.toHaveBeenCalled()
    expect(plain.defaultPrevented).toBe(false)

    const pinch = new view.WheelEvent('wheel', { ctrlKey: true, cancelable: true, bubbles: true })
    block.dispatchEvent(pinch)
    expect(instance.zoomWithWheel).toHaveBeenCalledTimes(1)
    expect(pinch.defaultPrevented).toBe(true)
  })

  test('a double-click on a control does not bubble to the block reset', () => {
    const block = renderedBlock()
    applyDiagramZoom(block)
    const instance = lastInstance()
    const cluster = block.querySelector('.diagram-zoom-controls')!

    cluster.dispatchEvent(new block.ownerDocument.defaultView!.MouseEvent('dblclick', { bubbles: true }))

    // The block's dblclick→reset must not fire for a dblclick inside the controls.
    expect(instance.reset).not.toHaveBeenCalled()
  })

  test('is idempotent — a second call does not create a second instance', () => {
    const block = renderedBlock()
    applyDiagramZoom(block)
    applyDiagramZoom(block)

    expect(PanzoomMock).toHaveBeenCalledTimes(1)
    expect(block.querySelectorAll('.diagram-zoom-controls').length).toBe(1)
  })

  test('a block with no rendered svg is a no-op', () => {
    const dom = new JSDOM('<!doctype html><body></body>')
    ;(globalThis as unknown as { document: Document }).document = dom.window.document as unknown as Document
    const block = dom.window.document.createElement('pre')
    block.className = 'mermaid'

    const teardown = applyDiagramZoom(block as unknown as HTMLElement)

    expect(PanzoomMock).not.toHaveBeenCalled()
    expect(block.classList.contains('zoomable-diagram')).toBe(false)
    expect(() => teardown()).not.toThrow()
  })

  test('teardown destroys panzoom, removes controls, and un-marks the block', () => {
    const block = renderedBlock()
    const teardown = applyDiagramZoom(block)
    const instance = lastInstance()

    teardown()

    expect(instance.destroy).toHaveBeenCalledTimes(1)
    expect(block.querySelector('.diagram-zoom-controls')).toBeNull()
    expect(block.classList.contains('zoomable-diagram')).toBe(false)
    // and it can be re-zoomed afterward (dataset flag cleared)
    applyDiagramZoom(block)
    expect(PanzoomMock).toHaveBeenCalledTimes(2)
  })
})
