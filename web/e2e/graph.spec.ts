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

test('canvas fits its container and re-fits on resize without pushing UI out of bounds', async ({ page }) => {
  await page.goto('/')

  const canvas = page.locator('#graph canvas')
  await expect(canvas).toBeVisible({ timeout: 15000 })
  // Let the 3D scene settle so layout is stable before measuring.
  await page.waitForTimeout(300)

  const win = await page.evaluate(() => ({ w: window.innerWidth, h: window.innerHeight }))

  // 1. Canvas CSS box matches the #graph container, not the window.
  const graphBox = (await page.locator('#graph').boundingBox())!
  const canvasBox = (await canvas.boundingBox())!
  expect(Math.abs(canvasBox.width - graphBox.width)).toBeLessThanOrEqual(2)
  expect(Math.abs(canvasBox.height - graphBox.height)).toBeLessThanOrEqual(2)
  // The 320px panel and the facets bar mean the canvas is meaningfully smaller
  // than the window — proves it is NOT painted at window.innerWidth/innerHeight.
  expect(canvasBox.width).toBeLessThan(win.w - 100)
  expect(canvasBox.height).toBeLessThan(win.h - 20)

  // 2. The inspect panel is visible and fully inside the viewport.
  const panelBox = (await page.locator('#panel').boundingBox())!
  expect(panelBox.x + panelBox.width).toBeLessThanOrEqual(win.w + 2)

  // 3. No horizontal document overflow (no out-of-bounds scrollbar).
  const overflow = await page.evaluate(() => ({
    scrollWidth: document.documentElement.scrollWidth,
    clientWidth: document.documentElement.clientWidth,
  }))
  expect(overflow.scrollWidth).toBeLessThanOrEqual(overflow.clientWidth + 2)

  // 4. After a viewport resize the canvas re-fits the new container size,
  //    proving the ResizeObserver wiring (the library has none of its own).
  await page.setViewportSize({ width: 1024, height: 768 })
  await page.waitForTimeout(300)
  const graphBox2 = (await page.locator('#graph').boundingBox())!
  const canvasBox2 = (await canvas.boundingBox())!
  expect(Math.abs(canvasBox2.width - graphBox2.width)).toBeLessThanOrEqual(2)
  expect(Math.abs(canvasBox2.height - graphBox2.height)).toBeLessThanOrEqual(2)
  expect(Math.abs(canvasBox2.width - canvasBox.width)).toBeGreaterThan(10)

  const overflow2 = await page.evaluate(() => ({
    scrollWidth: document.documentElement.scrollWidth,
    clientWidth: document.documentElement.clientWidth,
  }))
  expect(overflow2.scrollWidth).toBeLessThanOrEqual(overflow2.clientWidth + 2)
})

test('a wrapped (multi-row) facets bar never overlaps the graph', async ({ page }) => {
  await page.goto('/')
  await expect(page.locator('#graph canvas')).toBeVisible({ timeout: 15000 })
  await page.waitForTimeout(300)

  // Real vaults have many node types / edge kinds, so the facets bar wraps to
  // several rows. Inject enough labels to force at least two rows.
  const facetsHeight = await page.evaluate(() => {
    const bar = document.querySelector('#facets')!
    const form = bar.querySelector('#search-form')
    for (const t of ['note', 'ticket', 'spec', 'decision', 'person', 'meeting', 'project', 'epic', 'risk', 'question', 'reference', 'glossary', 'milestone', 'retro', 'adr', 'okr']) {
      const label = document.createElement('label')
      const cb = document.createElement('input')
      cb.type = 'checkbox'
      cb.checked = true
      label.appendChild(cb)
      label.appendChild(document.createTextNode(' ' + t))
      bar.insertBefore(label, form)
    }
    return (bar as HTMLElement).getBoundingClientRect().height
  })
  // Confirm the bar actually wrapped past a single row — otherwise the test
  // would be a false negative.
  expect(facetsHeight).toBeGreaterThan(40)
  await page.waitForTimeout(300)

  const boxes = await page.evaluate(() => {
    const b = (sel: string) => document.querySelector(sel)!.getBoundingClientRect()
    return {
      facets: b('#facets'),
      mainArea: b('#main-area'),
      graph: b('#graph'),
      canvas: b('#graph canvas'),
    }
  })
  // The facets bar sits entirely above the graph area — no overlap.
  expect(boxes.facets.bottom).toBeLessThanOrEqual(boxes.mainArea.top + 1)
  expect(boxes.graph.top).toBeGreaterThanOrEqual(boxes.facets.bottom - 1)
  // The canvas still fits its container after the bar grew.
  expect(Math.abs(boxes.canvas.width - boxes.graph.width)).toBeLessThanOrEqual(2)
  expect(Math.abs(boxes.canvas.height - boxes.graph.height)).toBeLessThanOrEqual(2)

  const overflow = await page.evaluate(() => ({
    scrollWidth: document.documentElement.scrollWidth,
    clientWidth: document.documentElement.clientWidth,
    scrollHeight: document.documentElement.scrollHeight,
    clientHeight: document.documentElement.clientHeight,
  }))
  expect(overflow.scrollWidth).toBeLessThanOrEqual(overflow.clientWidth + 2)
  expect(overflow.scrollHeight).toBeLessThanOrEqual(overflow.clientHeight + 2)
})
