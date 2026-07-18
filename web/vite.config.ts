import { defineConfig } from 'vite'

// The unified web app builds into the Go package so go:embed can reach it.
//
// base is '/' (not './') because the app is a single-page app served at the
// domain root with client routes like /read: absolute asset URLs resolve the
// same from any route, whereas relative ones break on a deep link.
//
// The entry keeps a fixed name (assets/app.js); chunks and assets carry a
// content hash. Hashing is required now that the two views load lazily — both
// view entries would otherwise collapse onto the same fixed chunk name and
// collide — and it stays drift-safe because a content hash is deterministic:
// the dist-drift guard only needs `make frontend` to reproduce the committed
// bytes, which the same source always does.
//
// assetsInlineLimit is 0 (carried over from the reading view): the app CSP has
// no font-src, so KaTeX's woff2 files must stay same-origin files rather than
// being inlined as data: URIs, which default-src 'self' would block. The
// worker block pins the graph's layout worker into the same hashed-asset
// pipeline instead of letting Vite emit a separately-hashed worker name.
export default defineConfig({
  root: '.',
  base: '/',
  test: {
    // Exclude Playwright e2e specs — those run via `pnpm e2e`, not vitest.
    // jsdom gives the reading-view and shell suites a DOM; the graph suites
    // stub the browser globals they touch, so it is harmless for them too.
    exclude: ['e2e/**', 'node_modules/**'],
    environment: 'jsdom',
    // Polyfills jsdom omits (matchMedia) that theme.ts reads at module load.
    setupFiles: ['./vitest.setup.ts'],
  },
  build: {
    outDir: '../internal/webapp/dist',
    emptyOutDir: true,
    sourcemap: false,
    assetsInlineLimit: 0,
    rollupOptions: {
      output: {
        entryFileNames: 'assets/app.js',
        chunkFileNames: 'assets/[name]-[hash].js',
        assetFileNames: 'assets/[name]-[hash][extname]',
      },
    },
  },
  worker: {
    format: 'es',
    rollupOptions: {
      output: {
        entryFileNames: 'assets/[name]-[hash].js',
        chunkFileNames: 'assets/[name]-[hash].js',
        assetFileNames: 'assets/[name]-[hash][extname]',
      },
    },
  },
})
