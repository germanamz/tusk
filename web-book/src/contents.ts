// The Contents pane: every file-level node in the vault, grouped either by
// `type` (default) or by folder derived from `path`.
//
// MAINTAINER RULING (plan-corrections.md, "the Contents tree comes from
// `path`, not `parent`"): IndexNode has no `parent` field — every row
// GET /api/index returns has a NULL parent_id by construction, so a tree
// built on it would render flat while looking implemented. The folder
// grouping below derives its hierarchy from `path` instead: a node's
// directory is `path.slice(0, path.lastIndexOf('/'))` (the same rule
// reader.ts uses for `nodeDir`), an empty directory means the node sits at
// the vault root, and nested directories ("a/b/c.md") produce nested folder
// groups rather than one flat "a/b" bucket.
import type { IndexNode, IndexResponse } from './api'

export type Grouping = 'type' | 'folder'

interface FolderNode {
  entries: IndexNode[]
  folders: Map<string, FolderNode>
}

function nodeDirOf(node: IndexNode): string {
  return node.path.includes('/') ? node.path.slice(0, node.path.lastIndexOf('/')) : ''
}

function sortByTitle(nodes: IndexNode[]): IndexNode[] {
  return [...nodes].sort((a, b) => (a.title || a.id).localeCompare(b.title || b.id))
}

function buildFolderTree(nodes: IndexNode[]): FolderNode {
  const root: FolderNode = { entries: [], folders: new Map() }

  for (const node of nodes) {
    const dir = nodeDirOf(node)

    if (dir === '') {
      root.entries.push(node)
      continue
    }

    let cursor = root

    for (const segment of dir.split('/')) {
      let next = cursor.folders.get(segment)

      if (!next) {
        next = { entries: [], folders: new Map() }
        cursor.folders.set(segment, next)
      }

      cursor = next
    }

    cursor.entries.push(node)
  }

  return root
}

function renderEntry(node: IndexNode, onSelect: (id: string) => void): HTMLLIElement {
  const li = document.createElement('li')
  const button = document.createElement('button')
  button.type = 'button'
  button.className = 'contents-entry'
  button.dataset.id = node.id

  const stamp = document.createElement('span')
  stamp.className = 'contents-entry-stamp'
  stamp.textContent = node.type

  const label = document.createElement('span')
  label.className = 'contents-entry-title'
  label.textContent = node.title || node.id
  label.title = node.title || node.id

  button.append(stamp, label)
  button.addEventListener('click', () => onSelect(node.id))

  li.appendChild(button)
  return li
}

function renderFolderNode(
  node: FolderNode,
  onSelect: (id: string) => void,
  pathPrefix = '',
): HTMLUListElement {
  const list = document.createElement('ul')
  list.className = 'contents-list'

  for (const entry of sortByTitle(node.entries)) {
    list.appendChild(renderEntry(entry, onSelect))
  }

  const folderNames = Array.from(node.folders.keys()).sort((a, b) => a.localeCompare(b))

  for (const name of folderNames) {
    const child = node.folders.get(name)
    if (!child) continue

    const folderPath = pathPrefix ? `${pathPrefix}/${name}` : name

    const li = document.createElement('li')
    li.className = 'contents-folder'
    li.dataset.folder = folderPath

    const label = document.createElement('div')
    label.className = 'contents-folder-label'
    label.textContent = `${name}/`

    li.append(label, renderFolderNode(child, onSelect, folderPath))
    list.appendChild(li)
  }

  return list
}

function renderByFolder(nodes: IndexNode[], onSelect: (id: string) => void): HTMLDivElement {
  const container = document.createElement('div')
  container.className = 'contents-tree'
  container.dataset.grouping = 'folder'
  container.appendChild(renderFolderNode(buildFolderTree(nodes), onSelect))
  return container
}

function renderByType(nodes: IndexNode[], onSelect: (id: string) => void): HTMLDivElement {
  const container = document.createElement('div')
  container.className = 'contents-tree'
  container.dataset.grouping = 'type'

  const groups = new Map<string, IndexNode[]>()

  for (const node of nodes) {
    const bucket = groups.get(node.type) ?? []
    bucket.push(node)
    groups.set(node.type, bucket)
  }

  for (const type of Array.from(groups.keys()).sort((a, b) => a.localeCompare(b))) {
    const bucket = groups.get(type)
    if (!bucket) continue

    const group = document.createElement('div')
    group.className = 'contents-type-group'
    group.dataset.type = type

    const label = document.createElement('div')
    label.className = 'contents-type-label'
    label.textContent = type

    const list = document.createElement('ul')
    list.className = 'contents-list'

    for (const entry of sortByTitle(bucket)) {
      list.appendChild(renderEntry(entry, onSelect))
    }

    group.append(label, list)
    container.appendChild(group)
  }

  return container
}

function renderToggle(active: Grouping, onChange: (next: Grouping) => void): HTMLDivElement {
  const wrap = document.createElement('div')
  wrap.className = 'contents-toggle'
  wrap.setAttribute('role', 'tablist')
  wrap.setAttribute('aria-label', 'Group Contents by')

  const options: Array<[Grouping, string]> = [
    ['type', 'By type'],
    ['folder', 'By folder'],
  ]

  for (const [value, label] of options) {
    const button = document.createElement('button')
    button.type = 'button'
    button.className = 'contents-toggle-btn'
    if (value === active) button.classList.add('is-active')
    button.setAttribute('role', 'tab')
    button.setAttribute('aria-pressed', String(value === active))
    button.textContent = label
    button.addEventListener('click', () => onChange(value))
    wrap.appendChild(button)
  }

  return wrap
}

// renderContents paints the toggle plus the current grouping's tree into
// `el`, replacing its previous contents each time. Clicking an entry invokes
// onSelect with the node's raw id (never the encoded/hash form) — callers
// (main.ts) decide what selecting a node means, typically setting
// `location.hash`.
export function renderContents(
  el: HTMLElement,
  index: IndexResponse,
  onSelect: (id: string) => void,
): void {
  let grouping: Grouping = 'type'

  function paint(): void {
    el.innerHTML = ''
    el.appendChild(
      renderToggle(grouping, (next) => {
        grouping = next
        paint()
      }),
    )
    el.appendChild(
      grouping === 'type' ? renderByType(index.nodes, onSelect) : renderByFolder(index.nodes, onSelect),
    )
  }

  paint()
}
