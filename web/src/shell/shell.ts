// shell.ts — wires the static console chrome to the router, the theme
// controller, and the live status. index.html ships the markup; this attaches
// behavior and mounts the first view.

import { getMode, setMode, type ThemeMode } from '../theme/theme'
import { createRouter, viewForPath, type ViewKey } from './router'
import { initStatus } from './status'

const VIEW_LABEL: Record<ViewKey, string> = { graph: 'GRAPH', read: 'READ' }

export function initShell(): void {
  const viewport = document.getElementById('viewport') as HTMLElement
  const viewName = document.getElementById('view-name') as HTMLElement
  const railButtons = Array.from(document.querySelectorAll<HTMLButtonElement>('.rail-btn'))
  const themeButtons = Array.from(document.querySelectorAll<HTMLButtonElement>('.theme-toggle button'))

  function syncThemeButtons(): void {
    const mode = getMode()

    for (const button of themeButtons) {
      button.setAttribute('aria-pressed', String(button.dataset.mode === mode))
    }
  }

  for (const button of themeButtons) {
    button.addEventListener('click', () => {
      setMode(button.dataset.mode as ThemeMode)
      syncThemeButtons()
    })
  }

  syncThemeButtons()

  const router = createRouter(viewport, (view) => {
    viewName.textContent = VIEW_LABEL[view]
    document.title = view === 'read' ? 'tusk web — read' : 'tusk web — graph'

    for (const button of railButtons) {
      if (button.dataset.view === view) {
        button.setAttribute('aria-current', 'page')
      } else {
        button.removeAttribute('aria-current')
      }
    }
  })

  for (const button of railButtons) {
    button.addEventListener('click', () => {
      void router.navigate(button.dataset.view as ViewKey)
    })
  }

  initStatus()

  void router.navigate(viewForPath(location.pathname), { push: false })
}
