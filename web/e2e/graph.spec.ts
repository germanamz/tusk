import { test, expect } from '@playwright/test'

// The graph now lives inside the unified `tusk web` shell, whose theme controls
// the WebGL scene colors. Pin dark mode before each load so the color
// assertions below (e.g. a selected node burns to --graph-selected, #ffffff in
// dark) are deterministic regardless of the runner's OS preference.
test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => localStorage.setItem('tusk.theme', 'dark'))
})

// The graph loads its snapshot asynchronously after the lazy view chunk mounts
// and the canvas appears, so tests that read the live scene data must wait for
// the first paint to populate graphData rather than racing the fetch.
async function waitForGraphData(page: import('@playwright/test').Page): Promise<void> {
  await page.waitForFunction(
    () => {
      const graph = (window as unknown as { tuskGraph?: { graphData(): { nodes: unknown[] } } }).tuskGraph
      return !!graph && graph.graphData().nodes.length > 0
    },
    undefined,
    { timeout: 15000 },
  )
}

// normHex expands a CSS #rgb shorthand to #rrggbb and lowercases it, so a color
// compare survives the production build minifying #ffffff down to #fff.
function normHex(color: string): string {
  const lower = color.trim().toLowerCase()
  const short = /^#([0-9a-f])([0-9a-f])([0-9a-f])$/.exec(lower)

  return short ? `#${short[1]}${short[1]}${short[2]}${short[2]}${short[3]}${short[3]}` : lower
}

test('renders the graph and opens an inspect panel', async ({ page }) => {
  await page.goto('/')

  // The 3d-force-graph scene renders into a canvas.
  await expect(page.locator('#graph canvas')).toBeVisible({ timeout: 15000 })

  // The snapshot API returns the fixture's nodes.
  const graph = await page.evaluate(async () => (await fetch('/api/graph').then((r) => r.json())))
  expect(graph.nodes.length).toBeGreaterThanOrEqual(2)

  // Search endpoint responds (structural filter; no embedder needed).
  const result = await page.evaluate(async () =>
    (await fetch('/api/graph/query', { method: 'POST', headers: { 'content-type': 'application/json' }, body: JSON.stringify({ filter: 'type:note' }) }).then((r) => r.json())),
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
  // The 320px panel shrinks the canvas width below the window, proving it is NOT
  // painted at window.innerWidth. (The controls drawer is an absolute overlay and
  // the graph fills the full height, so only the width is reduced.)
  expect(canvasBox.width).toBeLessThan(win.w - 100)

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

test('selecting a node highlights it + its edges and focuses the camera; deselecting clears it', async ({ page }) => {
  await page.goto('/')
  await expect(page.locator('#graph canvas')).toBeVisible({ timeout: 15000 })
  await waitForGraphData(page)

  // Pick a connected node. (Driving scene.select directly — instead of a canvas
  // click — keeps this off the flaky raycast-against-a-moving-node path while
  // still exercising the real highlight/focus code the click handler calls.)
  const sel = await page.evaluate(() => {
    const g = (window as any).tuskGraph
    const { nodes, links } = g.graphData()
    const id = (end: any) => (typeof end === 'object' ? end.id : end)
    const node = nodes.find((n: any) => links.some((l: any) => id(l.source) === n.id || id(l.target) === n.id))
    return node ? node.id : null
  })
  expect(sel, 'fixture must have a connected node').not.toBeNull()

  const before = await page.evaluate((selId) => {
    const g = (window as any).tuskGraph
    const { nodes } = g.graphData()
    const node = nodes.find((n: any) => n.id === selId)
    return { color: g.nodeColor()(node), camera: g.cameraPosition() }
  }, sel!)

  // Select the node, then read styling + camera straight off the live scene.
  const after = await page.evaluate((selId) => {
    const g = (window as any).tuskGraph
    ;(window as any).tuskScene.select(selId)
    const { nodes, links } = g.graphData()
    const id = (end: any) => (typeof end === 'object' ? end.id : end)
    const node = nodes.find((n: any) => n.id === selId)
    const incident = links.find((l: any) => id(l.source) === selId || id(l.target) === selId)
    const other = links.find((l: any) => id(l.source) !== selId && id(l.target) !== selId)
    return {
      nodeColor: g.nodeColor()(node),
      incidentWidth: g.linkWidth()(incident),
      otherWidth: other ? g.linkWidth()(other) : 0,
    }
  }, sel!)
  // Selected node burns white; its incident edge promotes to a real (cylinder) width.
  // normHex expands the CSS shorthand the production build minifies the theme
  // token into (--graph-selected: #ffffff -> #fff) so the compare is exact.
  expect(normHex(after.nodeColor)).toBe('#ffffff')
  expect(after.incidentWidth).toBeGreaterThan(0)
  expect(after.otherWidth).toBe(0)

  // Focus: the camera flies to the node (animated ~1.2s), so its position shifts.
  await page.waitForTimeout(1600)
  const camAfter = await page.evaluate(() => (window as any).tuskGraph.cameraPosition())
  const moved =
    Math.abs(camAfter.x - before.camera.x) +
    Math.abs(camAfter.y - before.camera.y) +
    Math.abs(camAfter.z - before.camera.z)
  expect(moved, 'camera should move toward the selected node').toBeGreaterThan(0.5)

  // Deselecting restores the node to its type color.
  const cleared = await page.evaluate((selId) => {
    const g = (window as any).tuskGraph
    ;(window as any).tuskScene.select(null)
    const node = g.graphData().nodes.find((n: any) => n.id === selId)
    return g.nodeColor()(node)
  }, sel!)
  expect(normHex(cleared)).not.toBe('#ffffff')
  expect(cleared.toLowerCase()).toBe(before.color.toLowerCase())
})

test('pan is bound to the Alt modifier (Blender/CAD-style)', async ({ page }) => {
  await page.goto('/')
  await expect(page.locator('#graph canvas')).toBeVisible({ timeout: 15000 })
  // TrackballControls.keys maps [rotate, zoom, pan] modifier codes; the pan slot
  // points at Alt so alt+drag pans while left-drag still orbits.
  const keys = await page.evaluate(() => (window as any).tuskGraph.controls().keys)
  expect(keys).toEqual(['', '', 'AltLeft'])
  // The controls drawer footer hint advertises the Alt+drag pan to the user.
  await expect(page.locator('.controls-footer')).toContainText('alt+drag pan')
})

test('the group filter visually hides non-matching group rows', async ({ page }) => {
  await page.goto('/')
  await expect(page.locator('#graph canvas')).toBeVisible({ timeout: 15000 })

  // The fixture has two type-groups (note, person). Typing "person" into the
  // group filter must VISUALLY hide the "note" row. Regression guard: a row
  // hidden via the `hidden` attribute stays visible because
  // `.controls-row { display: flex }` overrides the UA `[hidden] { display: none }`,
  // so the filter must toggle inline `display` instead.
  await expect(page.locator('.controls-row')).toHaveCount(2)

  const disp = await page.evaluate(() => {
    const rows = [...document.querySelectorAll('.controls-row')] as HTMLElement[]
    const byLabel = (l: string) => rows.find((r) => r.querySelector('.controls-row-label')?.textContent === l)
    const search = document.querySelector('.controls-group-search') as HTMLInputElement
    search.value = 'person'
    search.dispatchEvent(new Event('input', { bubbles: true }))
    const computed = (l: string) => {
      const r = byLabel(l)
      return r ? getComputedStyle(r).display : 'absent'
    }
    return { note: computed('note'), person: computed('person') }
  })

  expect(disp.person).not.toBe('none') // matching row stays visible
  expect(disp.note).toBe('none') // non-matching row is actually hidden (the bug left it 'flex')
})

test('neighbor navigation recurses across hops (A→B→A→C)', async ({ page }) => {
  await page.goto('/')
  await expect(page.locator('#graph canvas')).toBeVisible({ timeout: 15000 })

  // Seed the first detail panel deterministically via the debug seam (the same
  // navigate() the node-click handler routes through). notes/a relates to b and c
  // and mentions people/d, so its panel exposes outbound neighbor buttons.
  await page.evaluate(() => (window as { tuskNavigate?: (id: string) => void }).tuskNavigate?.('notes/a'))
  await expect(page.locator('#panel h2')).toHaveText('A')

  // Hop to neighbor B (outbound "relates").
  await page.locator('#panel button', { hasText: /→ B \[/ }).click()
  await expect(page.locator('#panel h2')).toHaveText('B')

  // From B, hop BACK to A via its inbound "relates" neighbor. This is the
  // regression: under the old no-op onNeighbor the B panel's buttons were dead.
  await page.locator('#panel button', { hasText: /← A \[/ }).click()
  await expect(page.locator('#panel h2')).toHaveText('A')

  // From A again, hop to C — proves navigated-to panels are themselves navigable.
  await page.locator('#panel button', { hasText: /→ C \[/ }).click()
  await expect(page.locator('#panel h2')).toHaveText('C')
})

test('node size & brightness track total degree end-to-end', async ({ page }) => {
  await page.goto('/')
  await expect(page.locator('#graph canvas')).toBeVisible({ timeout: 15000 })
  await waitForGraphData(page)

  // Drive the live scene accessors over the fixture (notes/a is a degree-2 hub
  // relating to b and c; b and c are degree-1 leaves). This is the only check
  // that covers the whole pipeline: snapshot.go degree -> wire -> scene.ts's
  // node.degree read + maxDegree reduce -> encode.ts size/brightness. A revert to
  // node.in_degree fails here (notes/a has in_degree 0, so it would render at the
  // floor despite being the highest-degree node).
  const enc = await page.evaluate(() => {
    const g = (window as any).tuskGraph
    const nodes = g.graphData().nodes as any[]
    const valFn = g.nodeVal()
    const colorFn = g.nodeColor()
    const lum = (hex: string) => {
      const m = /^#([0-9a-f]{6})$/i.exec(hex)
      if (!m) return -1
      const n = parseInt(m[1], 16)
      const ch = [(n >> 16) & 0xff, (n >> 8) & 0xff, n & 0xff]
      return (Math.max(...ch) + Math.min(...ch)) / 2
    }
    const rows = nodes.map((node: any) => ({ degree: node.degree, val: valFn(node), lum: lum(colorFn(node)) }))
    return {
      hi: rows.reduce((a, b) => (b.degree > a.degree ? b : a)),
      lo: rows.reduce((a, b) => (b.degree < a.degree ? b : a)),
      allNumericDegree: rows.every((r) => typeof r.degree === 'number'),
    }
  })

  // The wire carries a numeric total-degree per node, and the fixture has a real
  // spread, so the comparison below is meaningful (not two equal-degree nodes).
  expect(enc.allNumericDegree).toBe(true)
  expect(enc.hi.degree).toBeGreaterThan(enc.lo.degree)
  // Higher total degree => larger node val (size) and brighter (higher luminance).
  expect(enc.hi.val).toBeGreaterThan(enc.lo.val)
  expect(enc.hi.lum).toBeGreaterThan(enc.lo.lum)
})
