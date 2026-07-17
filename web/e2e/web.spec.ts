import { test, expect } from '@playwright/test'

// End-to-end smoke for the unified `tusk web` shell: the console chrome, the
// two views behind the rail switcher, live theme switching, SPA deep-linking,
// and the reading view's mermaid/katex render pipeline. Complements graph.spec
// (which drills into the graph view itself).

test('console shell renders and switches between the graph and reading views', async ({ page }) => {
  await page.goto('/')

  await expect(page.locator('.tbar-brand')).toContainText('TUSK')
  await expect(page.locator('#view-name')).toHaveText('GRAPH')
  await expect(page.locator('.rail-btn[data-view="graph"]')).toHaveAttribute('aria-current', 'page')
  await expect(page.locator('#graph canvas')).toBeVisible({ timeout: 15000 })

  // Rail switches to the reading view; the path and the chrome follow.
  await page.locator('.rail-btn[data-view="read"]').click()
  await expect(page).toHaveURL(/\/read$/)
  await expect(page.locator('#view-name')).toHaveText('READ')
  await expect(page.locator('.book-root')).toBeVisible()

  // Opening a Contents entry paints a document.
  await page.locator('#contents button.contents-entry').first().click()
  await expect(page.locator('#reader')).not.toContainText('Select something', { timeout: 10000 })

  // Rail switches back to the graph, which re-mounts.
  await page.locator('.rail-btn[data-view="graph"]').click()
  await expect(page).toHaveURL(/\/$/)
  await expect(page.locator('#graph canvas')).toBeVisible({ timeout: 15000 })
})

test('theme toggle flips the app and persists across reloads', async ({ page }) => {
  await page.goto('/')

  await page.locator('.theme-toggle button[data-mode="dark"]').click()
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark')
  expect(await page.evaluate(() => localStorage.getItem('tusk.theme'))).toBe('dark')

  await page.reload()
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark')

  await page.locator('.theme-toggle button[data-mode="light"]').click()
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'light')
})

test('deep-links the reading view directly (SPA history fallback)', async ({ page }) => {
  await page.goto('/read')

  await expect(page.locator('#view-name')).toHaveText('READ')
  await expect(page.locator('.book-root')).toBeVisible()
  await expect(page.locator('.rail-btn[data-view="read"]')).toHaveAttribute('aria-current', 'page')
})

test('reader renders mermaid diagrams and katex math', async ({ page }) => {
  await page.goto('/read')

  await page.locator('#contents button.contents-entry[data-id="notes/diagram"]').click()

  // mermaid resolves its diagram to an <svg>; katex renders math into .katex.
  await expect(page.locator('#reader svg')).toBeVisible({ timeout: 15000 })
  await expect(page.locator('#reader .katex').first()).toBeVisible()
})
