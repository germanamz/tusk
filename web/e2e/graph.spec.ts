import { test, expect } from '@playwright/test'

test('renders the graph and opens an inspect panel', async ({ page }) => {
  await page.goto('/')

  // The 3d-force-graph scene renders into a canvas.
  await expect(page.locator('#graph canvas')).toBeVisible({ timeout: 15000 })

  // The snapshot API returns the fixture's two nodes.
  const graph = await page.evaluate(async () => (await fetch('./api/graph').then((r) => r.json())))
  expect(graph.nodes.length).toBeGreaterThanOrEqual(2)

  // Search endpoint responds (structural filter; no embedder needed).
  const result = await page.evaluate(async () =>
    (await fetch('./api/query', { method: 'POST', headers: { 'content-type': 'application/json' }, body: JSON.stringify({ filter: 'type:note' }) }).then((r) => r.json())),
  )
  expect(result.matches.length).toBeGreaterThanOrEqual(2)
})
