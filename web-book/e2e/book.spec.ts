import { test, expect } from '@playwright/test'

// Every test gets a CSP-violation collector installed before any page script
// runs (page.addInitScript fires ahead of the document's own scripts, on
// every navigation). This is the durable e2e-level replacement for the Task
// 4.3 scratch harness that first proved mermaid renders under the real
// shipped `script-src 'self'` header (no 'unsafe-eval') — that harness was
// deleted, leaving no lasting check. If mermaid (or KaTeX) ever needs a
// looser CSP after a future dependency bump, a securitypolicyviolation event
// fires and the assertion below fails loudly instead of the diagram silently
// blanking.
test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => {
    ;(window as unknown as { __cspViolations: string[] }).__cspViolations = []

    document.addEventListener('securitypolicyviolation', (evt) => {
      ;(window as unknown as { __cspViolations: string[] }).__cspViolations.push(
        `${evt.violatedDirective}: ${evt.blockedURI}`,
      )
    })
  })
})

test('read a node with math, mermaid, image and wikilink; search; follow related', async ({ page }) => {
  await page.goto('/')

  // 1. Contents lists the fixture's three nodes (a, b, c).
  await expect(page.locator('#contents [data-id]')).toHaveCount(3)

  // 2. Open node A. Its body carries the KaTeX math, the mermaid fence, the
  // image, and a [[b]] wikilink; its frontmatter carries `references: [c]`.
  await page.locator('#contents [data-id="a"]').click()

  const reader = page.locator('#reader')
  await expect(reader.locator('h1')).toHaveText('A')
  await expect(reader.locator('.katex')).toBeVisible()
  await expect(reader.locator('.mermaid svg')).toBeVisible()
  await expect(reader.locator('img')).toHaveAttribute('src', /^\.\/api\/asset\//)

  // The load-bearing CSP assertion: the mermaid <svg> above just rendered
  // under the real, server-sent CSP header (this is the actual binary, not a
  // stub) with zero policy violations recorded. Assert the svg is genuinely
  // present (already done above) AND that nothing was silently blocked to get
  // there.
  const violations = await page.evaluate(() => (window as unknown as { __cspViolations: string[] }).__cspViolations)
  expect(violations).toEqual([])

  // 2b. The diagram is zoomable in place. Drive the zoom-in control and confirm
  // the diagram's transform actually changed, then reset. Re-check CSP after:
  // the pan/zoom library is bundled same-origin and transform-based, so it must
  // run under the shipped `script-src 'self'` (no unsafe-eval) with no violations.
  const diagram = reader.locator('pre.zoomable-diagram')
  await expect(diagram).toBeVisible()
  const diagramSvg = diagram.locator('svg')
  const transformBefore = await diagramSvg.evaluate((node) => node.style.transform)

  await diagram.hover()
  await diagram.getByRole('button', { name: 'Zoom in' }).click()
  await expect
    .poll(async () => diagramSvg.evaluate((node) => node.style.transform))
    .not.toBe(transformBefore)

  await diagram.getByRole('button', { name: 'Reset zoom' }).click()

  const afterZoomViolations = await page.evaluate(
    () => (window as unknown as { __cspViolations: string[] }).__cspViolations,
  )
  expect(afterZoomViolations).toEqual([])

  // 3. Follow the rendered wikilink ([[b]]) to node B.
  await reader.locator('.node-body a[href^="#/node/"]').click()
  await expect(page).toHaveURL(/#\/node\/b$/)
  await expect(reader.locator('h1')).toHaveText('B')

  // Back to Contents and reopen A so its Related rail is populated again.
  await page.locator('#contents [data-id="a"]').click()
  await expect(reader.locator('h1')).toHaveText('A')

  // 4. Search. POST /api/search returns 422 ("semantic search unavailable")
  // when no embedder (Ollama) is reachable — the expected condition in a CI
  // box, not a failure. This test drives the real header search box (not a
  // raw structural-filter fetch) and accepts EITHER a populated result list
  // OR the unavailable banner as "Results appear", so it stays green with or
  // without a local Ollama. See the task report for the fuller rationale.
  await page.locator('header input[type="search"]').fill('vectors')
  await page.locator('header form').press('Enter')

  await expect(page.locator('.results-bar')).toBeVisible()

  const matchCount = await page.locator('#contents .results-entry[data-id]').count()
  const bannerCount = await page.locator('.results-banner').count()
  expect(matchCount > 0 || bannerCount > 0).toBe(true)

  await page.locator('.results-back').click()

  // 5. Click a Related rail entry. The rail is embedder-free (it walks the
  // edge graph via graphexpand, never touching Ollama), so it always works.
  // A's frontmatter `references: [c]` makes C both a direct out-link (the
  // "Links" section) AND a distance-1 graph-walk neighbor (the "Related
  // (graph)" section) — scope to the Related section specifically, or the
  // `data-id="c"` entry resolves twice.
  const relatedSection = page.locator('#rails .rail-section', { hasText: 'Related (graph)' })
  const relatedC = relatedSection.locator('.rail-entry[data-id="c"]')
  await expect(relatedC).toBeVisible()
  await relatedC.click()
  await expect(reader.locator('h1')).toHaveText('C')
})
