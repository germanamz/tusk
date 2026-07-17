import { describe, expect, it, vi } from 'vitest'
import { renderContents } from './contents'
import type { IndexResponse } from './api'

// Mixed types AND a folder-vs-root split so the two grouping modes actually
// diverge: if folder grouping were secretly flat, 'specs/x' and 'root-note'
// would render identically to type grouping's output.
const index: IndexResponse = {
  nodes: [
    { id: 'specs/x', type: 'spec', title: 'X Spec', path: 'specs/x.md' },
    { id: 'root-note', type: 'note', title: 'Root Note', path: 'root-note.md' },
    { id: 'notes/a', type: 'note', title: 'A Note', path: 'notes/a.md' },
  ],
}

describe('by-type grouping (default)', () => {
  it('groups entries into type buckets', () => {
    const el = document.createElement('div')
    renderContents(el, index, () => {})

    const groups = Array.from(el.querySelectorAll<HTMLElement>('.contents-type-group'))
    expect(groups.map((group) => group.dataset.type)).toEqual(['note', 'spec'])

    const noteGroup = el.querySelector('.contents-type-group[data-type="note"]')
    expect(noteGroup?.querySelectorAll('.contents-entry')).toHaveLength(2)

    const specGroup = el.querySelector('.contents-type-group[data-type="spec"]')
    expect(specGroup?.querySelectorAll('.contents-entry')).toHaveLength(1)
  })

  it('defaults to the type toggle marked active', () => {
    const el = document.createElement('div')
    renderContents(el, index, () => {})
    const typeBtn = el.querySelector('[role="tab"]')
    expect(typeBtn?.getAttribute('aria-pressed')).toBe('true')
    expect(typeBtn?.textContent).toBe('By type')
  })
})

describe('by-folder grouping', () => {
  // This is the test that must fail against a flat list: a flat rendering
  // would put `specs/x` as a direct sibling of `root-note` in the same
  // top-level list, with no `.contents-folder[data-folder="specs"]`
  // ancestor to distinguish it. Only a real path-derived tree nests it.
  it('nests specs/x.md under a specs/ folder while root-note stays at the top level', () => {
    const el = document.createElement('div')
    renderContents(el, index, () => {})

    const folderToggle = el.querySelectorAll<HTMLButtonElement>('[role="tab"]')[1]
    folderToggle.click()

    const tree = el.querySelector('.contents-tree')
    expect(tree?.getAttribute('data-grouping')).toBe('folder')

    const topList = tree?.querySelector(':scope > ul.contents-list')
    expect(topList).not.toBeNull()

    // root-note is a direct child of the top-level list.
    expect(topList?.querySelector(':scope > li > button[data-id="root-note"]')).not.toBeNull()

    // specs/x is NOT a direct child of the top-level list — this is exactly
    // the assertion a flat rendering would fail.
    expect(topList?.querySelector(':scope > li > button[data-id="specs/x"]')).toBeNull()

    // specs/x IS nested inside a specs/ folder group.
    const specsFolder = tree?.querySelector('li.contents-folder[data-folder="specs"]')
    expect(specsFolder).not.toBeNull()
    expect(specsFolder?.querySelector('.contents-folder-label')?.textContent).toBe('specs/')
    expect(specsFolder?.querySelector('button[data-id="specs/x"]')).not.toBeNull()

    // notes/a is nested under its own notes/ folder, not folded into specs/.
    const notesFolder = tree?.querySelector('li.contents-folder[data-folder="notes"]')
    expect(notesFolder).not.toBeNull()
    expect(notesFolder?.querySelector('button[data-id="notes/a"]')).not.toBeNull()
    expect(specsFolder?.querySelector('button[data-id="notes/a"]')).toBeNull()
  })

  it('re-renders when the toggle is clicked back to type grouping', () => {
    const el = document.createElement('div')
    renderContents(el, index, () => {})

    el.querySelectorAll<HTMLButtonElement>('[role="tab"]')[1].click()
    expect(el.querySelector('.contents-tree')?.getAttribute('data-grouping')).toBe('folder')

    el.querySelectorAll<HTMLButtonElement>('[role="tab"]')[0].click()
    expect(el.querySelector('.contents-tree')?.getAttribute('data-grouping')).toBe('type')
  })
})

describe('selection', () => {
  it('calls onSelect with the clicked node id (not the display title)', () => {
    const el = document.createElement('div')
    const onSelect = vi.fn()
    renderContents(el, index, onSelect)

    const button = el.querySelector<HTMLButtonElement>('button[data-id="notes/a"]')
    expect(button).not.toBeNull()
    button?.click()

    expect(onSelect).toHaveBeenCalledTimes(1)
    expect(onSelect).toHaveBeenCalledWith('notes/a')
  })
})
