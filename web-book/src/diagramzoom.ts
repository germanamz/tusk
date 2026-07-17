import Panzoom from '@panzoom/panzoom'

// Mermaid diagrams render to inline SVG at their in-column size, which is small
// for anything complex. applyDiagramZoom makes a rendered diagram pinch/pan/zoom
// in place — no overlay — so a trackpad or touch pinch, Ctrl+wheel, and drag all
// explore it without leaving the reading column.
//
// The `pre.mermaid` host is the zoom viewport (clipped via `overflow: hidden` in
// styles.css); Panzoom transforms the <svg> inside it. At rest the diagram looks
// exactly as it did before.

// applyDiagramZoom wires zoom onto an already-rendered mermaid block (a
// `pre.mermaid` whose fence has been replaced by an <svg> by mermaid.run()).
// It is idempotent per block. Returns a teardown that destroys the Panzoom
// instance and removes the controls + listeners — called on the next node render
// so instances never accumulate across navigations.
export function applyDiagramZoom(block: HTMLElement): () => void {
  const noop = () => {}

  const svg = block.querySelector('svg')

  if (svg === null || block.dataset.zoomable === 'true') {
    return noop
  }

  block.dataset.zoomable = 'true'
  block.classList.add('zoomable-diagram')

  const panzoom = Panzoom(svg, {
    // 'outside' keeps the diagram covering the viewport, so it can't be dragged
    // fully out of view. minScale 1 stops it shrinking below its natural size.
    contain: 'outside',
    minScale: 1,
    maxScale: 8,
    step: 0.3,
    cursor: 'grab',
  })

  // Ctrl-gated wheel zoom. Browsers deliver a trackpad pinch as a wheel event with
  // ctrlKey true; an ordinary two-finger scroll has ctrlKey false. Gating on it
  // means a pinch zooms the diagram while a plain scroll passes through to the page
  // untouched — so the diagram never hijacks reading scroll. (Ctrl+wheel would
  // otherwise be browser page-zoom; preventDefault claims it for the diagram only
  // while the pointer is over it.)
  const onWheel = (event: WheelEvent) => {
    if (!event.ctrlKey) {
      return
    }

    event.preventDefault()
    panzoom.zoomWithWheel(event)
  }

  block.addEventListener('wheel', onWheel, { passive: false })

  const onDoubleClick = () => panzoom.reset()

  block.addEventListener('dblclick', onDoubleClick)

  const controls = buildControls(block.ownerDocument, panzoom)

  block.appendChild(controls)

  return () => {
    block.removeEventListener('wheel', onWheel)
    block.removeEventListener('dblclick', onDoubleClick)
    controls.remove()
    panzoom.destroy()
    block.classList.remove('zoomable-diagram')
    delete block.dataset.zoomable
  }
}

interface ZoomTarget {
  zoomIn: () => void
  zoomOut: () => void
  reset: () => void
}

// buildControls returns the corner ＋ − ⟳ cluster. It lives on the block (not the
// svg Panzoom drags), so a control click never starts a pan. Buttons are the
// mouse-user path and the discoverability hint; pinch/drag are the primary gesture.
function buildControls(doc: Document, panzoom: ZoomTarget): HTMLElement {
  const cluster = doc.createElement('div')
  cluster.className = 'diagram-zoom-controls'

  // A fast double-click on any control would otherwise bubble a dblclick up to the
  // block and fire its reset. Contain dblclick within the cluster.
  cluster.addEventListener('dblclick', (event) => event.stopPropagation())

  const button = (label: string, glyph: string, action: () => void): HTMLButtonElement => {
    const element = doc.createElement('button')
    element.type = 'button'
    element.setAttribute('aria-label', label)
    element.textContent = glyph
    element.addEventListener('click', (event) => {
      event.preventDefault()
      action()
    })
    return element
  }

  cluster.appendChild(button('Zoom out', '−', () => panzoom.zoomOut()))
  cluster.appendChild(button('Reset zoom', '↺', () => panzoom.reset()))
  cluster.appendChild(button('Zoom in', '+', () => panzoom.zoomIn()))

  return cluster
}
