import type { NodeDetail } from './nodeapi'

export function renderPanel(el: HTMLElement, detail: NodeDetail, onNeighbor: (id: string) => void): void {
  el.innerHTML = ''
  const title = document.createElement('h2')
  title.textContent = detail.title || detail.id
  el.appendChild(title)

  const meta = document.createElement('div')
  meta.textContent = `${detail.type} · ${detail.path}`
  el.appendChild(meta)

  const body = document.createElement('pre')
  body.textContent = detail.rendered
  el.appendChild(body)

  const heading = document.createElement('h3')
  heading.textContent = `Neighbors (${detail.neighbors.length})`
  el.appendChild(heading)

  for (const neighbor of detail.neighbors) {
    const link = document.createElement('button')
    link.textContent = `${neighbor.direction === 'out' ? '→' : '←'} ${neighbor.title || neighbor.id} [${neighbor.edge_type}]`
    link.addEventListener('click', () => onNeighbor(neighbor.id))
    el.appendChild(link)
  }
}
