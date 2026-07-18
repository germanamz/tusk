// router.ts — the view router for the single-page shell.
//
// Two client routes back the two views: "/" is the graph, "/read" is the
// reader. Each view is a lazily-imported module exposing mount(container) and
// unmount(); only the active view's code (three.js for the graph,
// mermaid/katex for the reader) is ever downloaded. Navigations are serialized
// so rapid switches can't interleave a mount with a prior unmount.

export type ViewKey = 'graph' | 'read'

interface ViewModule {
  mount: (container: HTMLElement) => Promise<void> | void
  unmount: () => void
}

const LOADERS: Record<ViewKey, () => Promise<ViewModule>> = {
  graph: () => import('../graph'),
  read: () => import('../read'),
}

const PATHS: Record<ViewKey, string> = { graph: '/', read: '/read' }

function normalize(pathname: string): string {
  const trimmed = pathname.replace(/\/+$/, '')

  return trimmed === '' ? '/' : trimmed
}

// viewForPath maps a URL path to its view. Anything that is not the reader's
// "/read" resolves to the graph, so an unknown deep link lands somewhere valid.
export function viewForPath(pathname: string): ViewKey {
  return normalize(pathname) === '/read' ? 'read' : 'graph'
}

export interface Router {
  navigate(view: ViewKey, opts?: { push?: boolean }): Promise<void>
  current(): ViewKey
}

export function createRouter(viewport: HTMLElement, onChange: (view: ViewKey) => void): Router {
  let currentKey: ViewKey = 'graph'
  // pendingKey is the view the latest navigate() intends to settle on; currentKey
  // only advances once apply() runs, so a same-view guard must compare against
  // pendingKey to survive rapid queued navigations.
  let pendingKey: ViewKey = 'graph'
  let currentModule: ViewModule | null = null
  let chain: Promise<void> = Promise.resolve()

  async function apply(view: ViewKey): Promise<void> {
    if (currentModule) {
      try {
        currentModule.unmount()
      } catch (unmountErr) {
        console.error('view unmount failed', unmountErr)
      }

      currentModule = null
    }

    viewport.replaceChildren()

    currentKey = view
    onChange(view)

    const mod = await LOADERS[view]()

    currentModule = mod
    await mod.mount(viewport)
  }

  function navigate(view: ViewKey, opts: { push?: boolean } = {}): Promise<void> {
    // Skip the remount when the target view is already the one settling. This
    // matters most on popstate: the reader navigates between nodes via
    // location.hash, so Back/Forward within /read fires popstate without
    // changing the view — remounting it here would needlessly tear down its SSE
    // stream and diagram zooms and race the reader's own hashchange re-route.
    // The initial mount still runs (currentModule is null until the first apply).
    if (view === pendingKey && currentModule) {
      return chain
    }

    pendingKey = view

    if (opts.push !== false && normalize(location.pathname) !== normalize(PATHS[view])) {
      history.pushState({ view }, '', PATHS[view])
    }

    chain = chain.then(() => apply(view)).catch((applyErr) => {
      console.error('view mount failed', applyErr)
    })

    return chain
  }

  window.addEventListener('popstate', () => {
    void navigate(viewForPath(location.pathname), { push: false })
  })

  return { navigate, current: () => currentKey }
}
