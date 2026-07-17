import { defineConfig } from '@playwright/test'

export default defineConfig({
  testDir: './e2e',
  // Build the fixture index before starting the book server (see global-setup).
  globalSetup: './e2e/global-setup.ts',
  webServer: {
    // Playwright resolves `cwd` relative to this config file (web-book/), so
    // cwd is web-book/e2e/fixture; the built binary is 3 levels up at
    // repo-root ./bin/tusk. Port 7398 is the book suite's own — 7399 belongs
    // to web/e2e's graph suite; keep them distinct so both can run at once.
    command: '../../../bin/tusk book --addr 127.0.0.1:7398 --open=false',
    cwd: 'e2e/fixture',
    url: 'http://127.0.0.1:7398/healthz',
    reuseExistingServer: false,
  },
  use: { baseURL: 'http://127.0.0.1:7398', viewport: { width: 1280, height: 720 } },
})
