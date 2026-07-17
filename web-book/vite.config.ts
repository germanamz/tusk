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
    rollupOptions: {
      output: {
        entryFileNames: 'assets/app.js',
        chunkFileNames: 'assets/[name].js',
        assetFileNames: 'assets/[name][extname]',
      },
    },
  },
})
