import type { Graph } from './api'
import type { Scene } from './scene'
import type { FacetState } from './facets'
import { EDGE_KIND_COLORS, buildTypeColors, INTER_ALPHA_DEFAULT, HUB_STRENGTH_DEFAULT } from './encode'

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

/** Bulk set operations over a universe of string keys.
 *  Exported for unit tests. */
export function bulkAll<T>(hidden: Set<T>): void {
  hidden.clear()
}
export function bulkNone<T>(universe: T[], hidden: Set<T>): void {
  for (const item of universe) hidden.add(item)
}
export function bulkInvert<T>(universe: T[], hidden: Set<T>): void {
  for (const item of universe) {
    if (hidden.has(item)) hidden.delete(item)
    else hidden.add(item)
  }
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

  // ---- Header (collapse toggle + node search) ----
  // The node search form is already present in the DOM as a static scaffold
  // inside #controls (see index.html). The collapse button is built here.
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

  // Move the static search form (already in DOM from index.html scaffold)
  // inside the header div. This makes it part of a container that is never
  // innerHTML-cleared, fixing the wipe bug.
  const existingSearchForm = document.getElementById('search-form')
  if (existingSearchForm) {
    header.appendChild(existingSearchForm)
  }

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

  // ---- Bulk controls (groups) ----
  const bulkBar = document.createElement('div')
  bulkBar.className = 'controls-bulk'

  function syncGroupCheckboxes(): void {
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
      bulkAll(facetState.hiddenGroups)
      syncGroupCheckboxes()
      onFilterChange()
    }),
  )
  bulkBar.appendChild(
    makeBtn('None', () => {
      bulkNone([...rowMap.keys()], facetState.hiddenGroups)
      syncGroupCheckboxes()
      onFilterChange()
    }),
  )
  bulkBar.appendChild(
    makeBtn('Invert', () => {
      bulkInvert([...rowMap.keys()], facetState.hiddenGroups)
      syncGroupCheckboxes()
      onFilterChange()
    }),
  )
  bulkBar.appendChild(
    makeBtn('Reset', () => {
      bulkAll(facetState.hiddenGroups)
      committedSolo = null
      scene.focusGroup(null)
      syncGroupCheckboxes()
      onFilterChange()
    }),
  )
  body.appendChild(bulkBar)

  // ---- Scrollable group list ----
  const listEl = document.createElement('div')
  listEl.className = 'controls-group-list'
  body.appendChild(listEl)

  // ---------------------------------------------------------------------------
  // Filters disclosure section (Types / Kinds / Hide orphans)
  // ---------------------------------------------------------------------------

  const filtersSection = document.createElement('div')
  filtersSection.className = 'controls-filters-section'

  const filtersToggle = document.createElement('button')
  filtersToggle.className = 'controls-filters-toggle'
  filtersToggle.textContent = '▸ Filters'
  let filtersOpen = false
  filtersToggle.addEventListener('click', () => {
    filtersOpen = !filtersOpen
    filtersToggle.textContent = (filtersOpen ? '▾' : '▸') + ' Filters'
    filtersBody.style.display = filtersOpen ? '' : 'none'
  })
  filtersSection.appendChild(filtersToggle)

  const filtersBody = document.createElement('div')
  filtersBody.className = 'controls-filters-body'
  filtersBody.style.display = 'none'
  filtersSection.appendChild(filtersBody)

  body.appendChild(filtersSection)

  // Per-type and per-kind row maps for diff-updating
  const typeRowMap = new Map<string, HTMLDivElement>()
  const kindRowMap = new Map<string, HTMLDivElement>()

  // Orphan checkbox — built once, never rebuilt
  const orphanRow = document.createElement('div')
  orphanRow.className = 'controls-filter-row'
  const orphanCb = document.createElement('input')
  orphanCb.type = 'checkbox'
  orphanCb.checked = facetState.hideOrphans
  orphanCb.addEventListener('change', () => {
    facetState.hideOrphans = orphanCb.checked
    onFilterChange()
  })
  orphanRow.appendChild(orphanCb)
  orphanRow.appendChild(document.createTextNode(' Hide orphans'))

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

  // ---- Edges slider group (persists across update() snapshots in closure) ----
  // `interAlpha` and `hubStrength` are persisted here so slider positions survive
  // diff-update snapshots that rebuild other parts of the drawer.
  let interAlphaVal = INTER_ALPHA_DEFAULT
  let hubStrengthVal = HUB_STRENGTH_DEFAULT

  const edgesGroup = document.createElement('div')
  edgesGroup.className = 'controls-edges-group'

  const edgesLabel = document.createElement('strong')
  edgesLabel.textContent = 'Edges'
  edgesGroup.appendChild(edgesLabel)

  const makeSliderRow = (
    label: string,
    min: number,
    max: number,
    step: number,
    initial: number,
    onChange: (v: number) => void,
  ): HTMLDivElement => {
    const row = document.createElement('div')
    row.className = 'controls-slider-row'
    const lbl = document.createElement('label')
    lbl.textContent = label
    const input = document.createElement('input')
    input.type = 'range'
    input.min = String(min)
    input.max = String(max)
    input.step = String(step)
    input.value = String(initial)
    input.addEventListener('input', () => onChange(parseFloat(input.value)))
    lbl.appendChild(input)
    row.appendChild(lbl)
    return row
  }

  edgesGroup.appendChild(
    makeSliderRow('Cross-cluster', 0.02, 0.6, 0.02, interAlphaVal, (v) => {
      interAlphaVal = v
      scene.setEdgeEmphasis({ interAlpha: v })
    }),
  )

  edgesGroup.appendChild(
    makeSliderRow('Hub fade', 0, 1, 0.05, hubStrengthVal, (v) => {
      hubStrengthVal = v
      scene.setEdgeEmphasis({ hubStrength: v })
    }),
  )

  footer.appendChild(edgesGroup)

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
  // Filters section helpers
  // ---------------------------------------------------------------------------

  function buildTypeSection(types: string[], typeColors: Map<string, string>): void {
    // Header row for Types with bulk controls
    let typeHeader = filtersBody.querySelector<HTMLDivElement>('.controls-filter-type-header')
    if (!typeHeader) {
      typeHeader = document.createElement('div')
      typeHeader.className = 'controls-filter-subheader controls-filter-type-header'

      const label = document.createElement('span')
      label.textContent = 'Types'
      typeHeader.appendChild(label)

      const typeBulk = document.createElement('div')
      typeBulk.className = 'controls-filter-bulk'
      typeBulk.appendChild(
        makeBtn('All', () => {
          bulkAll(facetState.hiddenTypes)
          syncTypeCheckboxes()
          onFilterChange()
        }),
      )
      typeBulk.appendChild(
        makeBtn('None', () => {
          bulkNone([...typeRowMap.keys()], facetState.hiddenTypes)
          syncTypeCheckboxes()
          onFilterChange()
        }),
      )
      typeBulk.appendChild(
        makeBtn('Invert', () => {
          bulkInvert([...typeRowMap.keys()], facetState.hiddenTypes)
          syncTypeCheckboxes()
          onFilterChange()
        }),
      )
      typeHeader.appendChild(typeBulk)
      filtersBody.appendChild(typeHeader)
    }

    // Diff-update type rows
    const currentTypes = new Set(types)
    for (const [t, el] of typeRowMap.entries()) {
      if (!currentTypes.has(t)) {
        el.remove()
        typeRowMap.delete(t)
        facetState.hiddenTypes.delete(t)
      }
    }

    for (const t of types) {
      if (!typeRowMap.has(t)) {
        const row = document.createElement('div')
        row.className = 'controls-filter-row'
        const cb = document.createElement('input')
        cb.type = 'checkbox'
        cb.checked = !facetState.hiddenTypes.has(t)
        cb.addEventListener('change', () => {
          if (cb.checked) facetState.hiddenTypes.delete(t)
          else facetState.hiddenTypes.add(t)
          onFilterChange()
        })
        row.appendChild(cb)

        const swatch = document.createElement('span')
        swatch.className = 'controls-swatch'
        swatch.style.background = typeColors.get(t) ?? '#888888'
        row.appendChild(swatch)

        const nameEl = document.createTextNode(' ' + t)
        row.appendChild(nameEl)

        typeRowMap.set(t, row)
        // Insert after the type header but before kind rows
        const kindHeader = filtersBody.querySelector('.controls-filter-kind-header')
        if (kindHeader) filtersBody.insertBefore(row, kindHeader)
        else filtersBody.appendChild(row)
      } else {
        // Diff-update existing row
        const row = typeRowMap.get(t)!
        const cb = row.querySelector<HTMLInputElement>('input[type="checkbox"]')
        if (cb) cb.checked = !facetState.hiddenTypes.has(t)
        const swatch = row.querySelector<HTMLSpanElement>('.controls-swatch')
        if (swatch) swatch.style.background = typeColors.get(t) ?? '#888888'
      }
    }
  }

  function buildKindSection(kinds: string[]): void {
    // Header row for Kinds with bulk controls
    let kindHeader = filtersBody.querySelector<HTMLDivElement>('.controls-filter-kind-header')
    if (!kindHeader) {
      kindHeader = document.createElement('div')
      kindHeader.className = 'controls-filter-subheader controls-filter-kind-header'

      const label = document.createElement('span')
      label.textContent = 'Kinds'
      kindHeader.appendChild(label)

      const kindBulk = document.createElement('div')
      kindBulk.className = 'controls-filter-bulk'
      kindBulk.appendChild(
        makeBtn('All', () => {
          bulkAll(facetState.hiddenKinds)
          syncKindCheckboxes()
          onFilterChange()
        }),
      )
      kindBulk.appendChild(
        makeBtn('None', () => {
          bulkNone([...kindRowMap.keys()], facetState.hiddenKinds)
          syncKindCheckboxes()
          onFilterChange()
        }),
      )
      kindBulk.appendChild(
        makeBtn('Invert', () => {
          bulkInvert([...kindRowMap.keys()], facetState.hiddenKinds)
          syncKindCheckboxes()
          onFilterChange()
        }),
      )
      kindHeader.appendChild(kindBulk)

      // Insert before orphan row placeholder; append for now, orphan appended later
      filtersBody.appendChild(kindHeader)
    }

    // Diff-update kind rows
    const currentKinds = new Set(kinds)
    for (const [k, el] of kindRowMap.entries()) {
      if (!currentKinds.has(k)) {
        el.remove()
        kindRowMap.delete(k)
        facetState.hiddenKinds.delete(k)
      }
    }

    for (const k of kinds) {
      if (!kindRowMap.has(k)) {
        const row = document.createElement('div')
        row.className = 'controls-filter-row'
        const cb = document.createElement('input')
        cb.type = 'checkbox'
        cb.checked = !facetState.hiddenKinds.has(k)
        cb.addEventListener('change', () => {
          if (cb.checked) facetState.hiddenKinds.delete(k)
          else facetState.hiddenKinds.add(k)
          onFilterChange()
        })
        row.appendChild(cb)
        row.appendChild(document.createTextNode(' ' + k))
        kindRowMap.set(k, row)
        // Insert before the orphan row if it's already in the DOM
        if (orphanRow.parentElement === filtersBody) {
          filtersBody.insertBefore(row, orphanRow)
        } else {
          filtersBody.appendChild(row)
        }
      } else {
        const row = kindRowMap.get(k)!
        const cb = row.querySelector<HTMLInputElement>('input[type="checkbox"]')
        if (cb) cb.checked = !facetState.hiddenKinds.has(k)
      }
    }
  }

  function syncTypeCheckboxes(): void {
    for (const [t, row] of typeRowMap.entries()) {
      const cb = row.querySelector<HTMLInputElement>('input[type="checkbox"]')
      if (cb) cb.checked = !facetState.hiddenTypes.has(t)
    }
  }

  function syncKindCheckboxes(): void {
    for (const [k, row] of kindRowMap.entries()) {
      const cb = row.querySelector<HTMLInputElement>('input[type="checkbox"]')
      if (cb) cb.checked = !facetState.hiddenKinds.has(k)
    }
  }

  function updateFilters(graph: Graph): void {
    const types = [...new Set(graph.nodes.map((n) => n.type))].sort()
    const kinds = [...new Set(graph.edges.map((e) => e.kind))].sort()
    const typeColors = buildTypeColors(types)

    buildTypeSection(types, typeColors)
    buildKindSection(kinds)

    // Ensure orphan row is at the end of filtersBody
    if (orphanRow.parentElement !== filtersBody) {
      filtersBody.appendChild(orphanRow)
    } else {
      // Re-append to keep it last (no-op if already last)
      filtersBody.appendChild(orphanRow)
    }

    // Keep orphan checkbox in sync with facetState (in case it changed externally)
    orphanCb.checked = facetState.hideOrphans
  }

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

    // Diff-update the Filters section.
    updateFilters(graph)
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
