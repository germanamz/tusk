// theme.ts — the app-wide theme controller.
//
// Three modes: 'light', 'dark', and 'system' (follow the OS). Whatever the
// mode, an explicit 'light' | 'dark' is always stamped as data-theme on <html>
// so styling never depends on the media query at runtime (that query is only
// the pre-JS flash guard in tokens.css). The chosen mode persists to
// localStorage. Views subscribe with onThemeChange to recolor imperatively —
// the WebGL graph scene and mermaid, which cannot read CSS variables through
// stylesheets alone.

export type ThemeMode = 'light' | 'dark' | 'system'
export type ResolvedTheme = 'light' | 'dark'

const STORAGE_KEY = 'tusk.theme'

const listeners = new Set<(theme: ResolvedTheme) => void>()
const media = window.matchMedia('(prefers-color-scheme: dark)')

let mode: ThemeMode = 'system'

function readStoredMode(): ThemeMode {
  try {
    const stored = localStorage.getItem(STORAGE_KEY)

    if (stored === 'light' || stored === 'dark' || stored === 'system') {
      return stored
    }
  } catch {
    // localStorage can throw in private-mode / sandboxed contexts; fall through.
  }

  return 'system'
}

function resolve(currentMode: ThemeMode): ResolvedTheme {
  if (currentMode === 'system') {
    return media.matches ? 'dark' : 'light'
  }

  return currentMode
}

function apply(): void {
  const resolved = resolve(mode)

  document.documentElement.setAttribute('data-theme', resolved)

  for (const listener of listeners) {
    listener(resolved)
  }
}

// initTheme reads the saved mode, stamps the resolved theme, and keeps the
// 'system' mode live by re-applying when the OS preference flips. Call once,
// before mounting any view, so first paint uses the right theme.
export function initTheme(): void {
  mode = readStoredMode()

  media.addEventListener('change', () => {
    if (mode === 'system') {
      apply()
    }
  })

  apply()
}

export function getMode(): ThemeMode {
  return mode
}

export function getResolvedTheme(): ResolvedTheme {
  return resolve(mode)
}

export function setMode(nextMode: ThemeMode): void {
  mode = nextMode

  try {
    localStorage.setItem(STORAGE_KEY, nextMode)
  } catch {
    // Persisting is best-effort; the in-memory mode still applies this session.
  }

  apply()
}

// onThemeChange registers a callback fired on every resolved-theme change (and
// never on boot — call getResolvedTheme for the initial value). Returns an
// unsubscribe function; views call it on unmount.
export function onThemeChange(callback: (theme: ResolvedTheme) => void): () => void {
  listeners.add(callback)

  return () => {
    listeners.delete(callback)
  }
}

// cssVar reads a CSS custom property off <html> — the bridge for imperative
// consumers (the WebGL scene, mermaid) that need the active theme's concrete
// color values rather than a var() reference.
export function cssVar(name: string): string {
  return getComputedStyle(document.documentElement).getPropertyValue(name).trim()
}
