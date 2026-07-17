import { defineConfig } from 'vite'

// Output goes into the Go package so go:embed can reach it. Filenames are
// fixed (no content hash) and there are no build timestamps, so `make
// web-book` is byte-reproducible and the dist drift check is exact.
export default defineConfig({
  root: '.',
  base: './',
  test: {
    // Exclude Playwright e2e specs — those run via `pnpm e2e`, not vitest.
    exclude: ['e2e/**', 'node_modules/**'],
    environment: 'jsdom',
  },
  build: {
    outDir: '../internal/bookview/dist',
    emptyOutDir: true,
    sourcemap: false,
    // The CSP (internal/bookview/server.go) has no font-src, so fonts fall
    // back to default-src 'self' — which does not include `data:`. Vite's
    // default assetsInlineLimit (4KB) would otherwise inline small assets
    // (KaTeX_Size3's woff2 was under that threshold) as `data:` URIs straight
    // into the CSS, and the browser blocks loading them under this CSP.
    // Setting this to 0 disables size-based inlining entirely — there is no
    // 4KB (or any other) size line to worry about for future assets; every
    // font/asset, whatever its size, stays a same-origin file.
    assetsInlineLimit: 0,
    rollupOptions: {
      output: {
        entryFileNames: 'assets/app.js',
        chunkFileNames: 'assets/[name].js',
        assetFileNames: 'assets/[name][extname]',
      },
    },
  },
})
