// jsdom test shim. vitest's jsdom environment provides no window.matchMedia,
// but theme.ts (pulled in transitively via encode.ts) calls it at module load.
// The graph unit tests that reach encode.ts import this FIRST so that import
// resolves. Production never loads this file — only *.test.ts import it, and a
// real browser already has window.matchMedia.
if (typeof window !== 'undefined' && typeof window.matchMedia !== 'function') {
  window.matchMedia = ((query: string) => ({
    matches: false,
    media: query,
    onchange: null,
    addEventListener: () => {},
    removeEventListener: () => {},
    addListener: () => {},
    removeListener: () => {},
    dispatchEvent: () => false,
  })) as unknown as typeof window.matchMedia
}
