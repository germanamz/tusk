// Global test setup. jsdom omits a couple of browser APIs the app touches at
// module-load or runtime; polyfill the minimum so any suite that imports the
// theme controller or the shell status has them. Individual tests may still
// override window.matchMedia to simulate a dark OS preference.
if (typeof window !== 'undefined' && !window.matchMedia) {
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
