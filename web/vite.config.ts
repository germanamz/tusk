import { defineConfig } from 'vite'

// Output goes into the Go package so go:embed can reach it. Filenames are
// fixed (no content hash) and there are no build timestamps, so `make web` is
// byte-reproducible and the dist drift check is exact.
export default defineConfig({
  root: '.',
  base: './',
  build: {
    outDir: '../internal/graphview/dist',
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
