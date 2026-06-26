import type { Graph } from './api'
import type { Scene } from './scene'
import type { FacetState } from './facets'
import { EDGE_KIND_COLORS } from './encode'

// ---------------------------------------------------------------------------
// Pure helpers (exported for unit tests)
// ---------------------------------------------------------------------------

export interface GroupRow {
  group: string
  label: string
  count: number
}

/** Returns one entry per distinct node.group value, sorted by count desc then
 *  label asc (stable). The empty-string sentinel is mapped to the label "(none)". */
export function groupRows(graph: Graph): GroupRow[] {
  const counts = new Map<string, number>()
  for (const node of graph.nodes) {
    const g = node.group ?? ''
    counts.set(g, (counts.get(g) ?? 0) + 1)
  }
  return [...counts.entries()]
    .map(([group, count]) => ({ group, label: group === '' ? '(none)' : group, count }))
    .sort((a, b) => b.count - a.count || a.label.localeCompare(b.label))
}

/** Case-insensitive substring match. Empty query always matches. */
export function matchesSearch(label: string, query: string): boolean {
  if (query === '') return true
  return label.toLowerCase().includes(query.toLowerCase())
}

// ---------------------------------------------------------------------------
// Factory
// ---------------------------------------------------------------------------

interface ControlsDeps {
  scene: Scene
  getRawGraph: () => Graph
  getGroupColors: () => Map<string, string>
  facetState: FacetState
  onFilterChange: () => void
}

export function createControls(deps: ControlsDeps): {
  update(graph: Graph, groupColors: Map<string, string>): void
  clearSolo(): void
} {
  const { scene, facetState, onFilterChange } = deps

  // The committed solo state — the group(s) isolated via ⦿ click (not hover).
  let committedSolo: Set<string> | null = null

  // ---------------------------------------------------------------------------
  // Build the drawer DOM once
  // ---------------------------------------------------------------------------

  const drawer = document.getElementById('controls')!

  // ---- Header (collapse toggle) ----
  const header = document.createElement('div')
  header.className = 'controls-header'

  const collapseBtn = document.createElement('button')
  collapseBtn.className = 'controls-collapse-btn'
  collapseBtn.setAttribute('aria-label', 'Toggle controls drawer')
  collapseBtn.textContent = '≡ Controls ▾'
  collapseBtn.addEventListener('click', () => {
    drawer.classList.toggle('collapsed')
  })
  header.appendChild(collapseBtn)
  drawer.appendChild(header)

  // ---- Body (everything that collapses away) ----
  const body = document.createElement('div')
  body.className = 'controls-body'
  drawer.appendChild(body)

  // ---- Group header (lens label + count) ----
  const groupHeader = document.createElement('div')
  groupHeader.className = 'controls-group-header'
  body.appendChild(groupHeader)

  // ---- Group search (hidden until > 15 groups) ----
  const groupSearch = document.createElement('input')
  groupSearch.type = 'text'
  groupSearch.placeholder = 'filter groups…'
  groupSearch.className = 'controls-group-search'
  groupSearch.style.display = 'none'
  body.appendChild(groupSearch)

  // ---- Bulk controls ----
  const bulkBar = document.createElement('div')
  bulkBar.className = 'controls-bulk'

  function syncCheckboxes(): void {
    for (const [group, row] of rowMap.entries()) {
      const cb = row.querySelector<HTMLInputElement>('input[type="checkbox"]')
      if (cb) cb.checked = !facetState.hiddenGroups.has(group)
    }
  }

  const makeBtn = (label: string, handler: () => void): HTMLButtonElement => {
    const btn = document.createElement('button')
    btn.textContent = label
    btn.addEventListener('click', handler)
    return btn
  }

  bulkBar.appendChild(
    makeBtn('All', () => {
      facetState.hiddenGroups.clear()
      syncCheckboxes()
      onFilterChange()
    }),
  )
  bulkBar.appendChild(
    makeBtn('None', () => {
      for (const group of rowMap.keys()) {
        facetState.hiddenGroups.add(group)
      }
      syncCheckboxes()
      onFilterChange()
    }),
  )
  bulkBar.appendChild(
    makeBtn('Invert', () => {
      for (const group of rowMap.keys()) {
        if (facetState.hiddenGroups.has(group)) facetState.hiddenGroups.delete(group)
        else facetState.hiddenGroups.add(group)
      }
      syncCheckboxes()
      onFilterChange()
    }),
  )
  bulkBar.appendChild(
    makeBtn('Reset', () => {
      facetState.hiddenGroups.clear()
      committedSolo = null
      scene.focusGroup(null)
      syncCheckboxes()
      onFilterChange()
    }),
  )
  body.appendChild(bulkBar)

  // ---- Scrollable group list ----
  const listEl = document.createElement('div')
  listEl.className = 'controls-group-list'
  body.appendChild(listEl)

  // ---- Footer (fixed, non-scrolling) ----
  const footer = document.createElement('div')
  footer.className = 'controls-footer'

  const edgeHeader = document.createElement('strong')
  edgeHeader.textContent = 'Edge kinds: '
  footer.appendChild(edgeHeader)

  const swatchSpan = (color: string): HTMLSpanElement => {
    const s = document.createElement('span')
    s.className = 'controls-swatch'
    s.style.background = color
    return s
  }

  for (const [kind, color] of Object.entries(EDGE_KIND_COLORS)) {
    const item = document.createElement('span')
    item.className = 'controls-footer-item'
    item.appendChild(swatchSpan(color))
    item.appendChild(document.createTextNode(kind))
    footer.appendChild(item)
  }

  const hintSize = document.createElement('span')
  hintSize.className = 'controls-footer-hint'
  hintSize.textContent = 'size & brightness = connections (in + out)'
  footer.appendChild(hintSize)

  const hintControls = document.createElement('span')
  hintControls.className = 'controls-footer-hint'
  hintControls.textContent = 'drag rotate · alt+drag pan · scroll zoom'
  footer.appendChild(hintControls)

  drawer.appendChild(footer)

  // ---------------------------------------------------------------------------
  // Per-row DOM map (the diff-update key)
  // ---------------------------------------------------------------------------

  // Maps group value → the row <div> element. Never cleared; only added to or
  // pruned when groups disappear from the graph.
  const rowMap = new Map<string, HTMLDivElement>()

  function buildRow(group: string, label: string, count: number, groupColors: Map<string, string>): HTMLDivElement {
    const row = document.createElement('div')
    row.className = 'controls-row'
    row.dataset.group = group

    const cb = document.createElement('input')
    cb.type = 'checkbox'
    cb.checked = !facetState.hiddenGroups.has(group)
    cb.addEventListener('change', () => {
      if (cb.checked) facetState.hiddenGroups.delete(group)
      else facetState.hiddenGroups.add(group)
      onFilterChange()
    })
    row.appendChild(cb)

    const swatch = document.createElement('span')
    swatch.className = 'controls-swatch'
    swatch.style.background = groupColors.get(group) ?? '#888888'
    row.appendChild(swatch)

    const labelEl = document.createElement('span')
    labelEl.className = 'controls-row-label'
    labelEl.textContent = label
    row.appendChild(labelEl)

    const countEl = document.createElement('span')
    countEl.className = 'controls-row-count'
    countEl.textContent = String(count)
    row.appendChild(countEl)

    // The (none) group cannot be soloed (no group to focus)
    if (group !== '') {
      const soloBtn = document.createElement('button')
      soloBtn.className = 'controls-solo-btn'
      soloBtn.textContent = '⦿'
      soloBtn.setAttribute('aria-label', `Solo group ${label}`)
      soloBtn.addEventListener('click', (e) => {
        e.stopPropagation()
        const solo = new Set([group])
        // Toggle: if this group is already the sole focus, clear it.
        if (committedSolo !== null && committedSolo.size === 1 && committedSolo.has(group)) {
          committedSolo = null
          scene.focusGroup(null)
        } else {
          committedSolo = solo
          scene.focusGroup(solo)
        }
      })
      row.appendChild(soloBtn)
    }

    // Hover preview: temporarily dim others without committing the solo.
    row.addEventListener('mouseenter', () => {
      if (group !== '') {
        scene.focusGroup(new Set([group]))
      }
    })
    row.addEventListener('mouseleave', () => {
      // Restore the committed solo (or clear if none).
      scene.focusGroup(committedSolo)
    })

    return row
  }

  // ---------------------------------------------------------------------------
  // Search filtering (on the existing DOM rows — no rebuild)
  // ---------------------------------------------------------------------------

  groupSearch.addEventListener('input', () => {
    const query = groupSearch.value
    for (const [, row] of rowMap.entries()) {
      const labelEl = row.querySelector<HTMLSpanElement>('.controls-row-label')
      const label = labelEl?.textContent ?? ''
      row.hidden = !matchesSearch(label, query)
    }
  })

  // ---------------------------------------------------------------------------
  // update() — diff-update on each snapshot
  // ---------------------------------------------------------------------------

  function update(graph: Graph, groupColors: Map<string, string>): void {
    // Recompute the sorted rows for this snapshot.
    const rows = groupRows(graph)
    const currentGroups = new Set(rows.map((r) => r.group))

    // Remove rows for groups that have vanished from the graph.
    for (const [group, el] of rowMap.entries()) {
      if (!currentGroups.has(group)) {
        el.remove()
        rowMap.delete(group)
        facetState.hiddenGroups.delete(group)
      }
    }

    // Add new rows; update surviving rows in place.
    for (const { group, label, count } of rows) {
      if (!rowMap.has(group)) {
        const row = buildRow(group, label, count, groupColors)
        rowMap.set(group, row)
        listEl.appendChild(row)
      } else {
        // Diff-update: only touch what may have changed.
        const row = rowMap.get(group)!
        const countEl = row.querySelector<HTMLSpanElement>('.controls-row-count')
        if (countEl) countEl.textContent = String(count)
        const swatch = row.querySelector<HTMLSpanElement>('.controls-swatch')
        if (swatch) swatch.style.background = groupColors.get(group) ?? '#888888'
        const cb = row.querySelector<HTMLInputElement>('input[type="checkbox"]')
        if (cb) cb.checked = !facetState.hiddenGroups.has(group)
        // Re-apply search filter to this row.
        const labelEl = row.querySelector<HTMLSpanElement>('.controls-row-label')
        const rowLabel = labelEl?.textContent ?? ''
        row.hidden = !matchesSearch(rowLabel, groupSearch.value)
      }
    }

    // Re-sort the list by the new ordering (reorder existing DOM nodes).
    for (const { group } of rows) {
      const el = rowMap.get(group)
      if (el) listEl.appendChild(el)
    }

    // Show/hide the group search box based on group count.
    groupSearch.style.display = rows.length > 15 ? '' : 'none'

    // Update the group header (lens label + count).
    const by = graph.cluster?.by ?? 'type'
    const lensLabel =
      by === 'type'
        ? 'Color: '
        : `Color (${by === 'property' ? (graph.cluster.property ?? 'property') : by}): `
    groupHeader.textContent = `${lensLabel}${rows.length} group${rows.length !== 1 ? 's' : ''}`
  }

  // ---------------------------------------------------------------------------
  // clearSolo — called by onBackgroundClick
  // ---------------------------------------------------------------------------

  function clearSolo(): void {
    committedSolo = null
    scene.focusGroup(null)
  }

  return { update, clearSolo }
}
