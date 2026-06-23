import { defineConfig } from '@playwright/test'

export default defineConfig({
  testDir: './e2e',
  webServer: {
    // Playwright resolves `cwd` relative to this config file (web/), so cwd is
    // web/e2e/fixture; the built binary is 3 levels up at repo-root ./bin/tusk.
    command: '../../../bin/tusk graph --addr 127.0.0.1:7399 --open=false',
    cwd: 'e2e/fixture',
    url: 'http://127.0.0.1:7399/healthz',
    reuseExistingServer: false,
  },
  use: { baseURL: 'http://127.0.0.1:7399', viewport: { width: 1280, height: 720 } },
})
