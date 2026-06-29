// Curated, perceptually-distinct qualitative palette (~12 hues spread across the
// wheel at medium saturation/lightness) chosen to read well on the #0b0e14
// canvas background. Used by buildTypeColors to assign a hue per node type.
export const BASE_PALETTE = [
  '#4f8cff', // blue
  '#ff6b6b', // red
  '#51cf66', // green
  '#ffd43b', // yellow
  '#cc5de8', // purple
  '#22b8cf', // cyan
  '#ff922b', // orange
  '#94d82d', // lime
  '#f06595', // pink
  '#20c997', // teal
  '#a78bfa', // violet
  '#fab005', // amber
]

export const EDGE_KIND_COLORS: Record<string, string> = {
  direct: '#7aa2f7',
  derived: '#9d7cd8',
  structural: '#565f89',
}

// The canvas background (#0b0e14) that dimmed colors blend toward.
const BACKGROUND: [number, number, number] = [0x0b, 0x0e, 0x14]

// Selection palette. The selected node burns white; its incident edges glow a
// warm accent that is distinct from the orange used for transient search pulses.
export const SELECTED_COLOR = '#ffffff'
export const HIGHLIGHT_LINK_COLOR = '#f5d76e'
export const PULSE_COLOR = '#f5a623'

// dimColor fades a hex color toward the canvas background so that, while a node
// is selected, everything that is not part of the selection recedes and the
// highlighted node + edges read clearly. `amount` is 0 (unchanged) → 1 (fully
// background). Non `#rrggbb` inputs are returned untouched.
export function dimColor(hex: string, amount = 0.82): string {
  const m = /^#?([0-9a-f]{6})$/i.exec(hex)
  if (!m) return hex
  const n = parseInt(m[1], 16)
  const channels = [(n >> 16) & 0xff, (n >> 8) & 0xff, n & 0xff]
  const mixed = channels.map((c, i) => Math.round(c + (BACKGROUND[i] - c) * amount))
  return '#' + mixed.map((c) => c.toString(16).padStart(2, '0')).join('')
}

// Fixed saturation/lightness for golden-angle-generated hues (the overflow past
// BASE_PALETTE). Tuned to match the base palette's perceived weight on the dark
// background.
const GENERATED_S = 0.62
const GENERATED_L = 0.6
// Golden-angle spacing in degrees: stepping the hue by this for each successive
// type keeps generated colors maximally far apart for ANY number of types.
const GOLDEN_ANGLE = 137.508

// assignPalette is the shared dedup→sort→assign body behind buildTypeColors and
// buildGroupColors. Values are deduped and sorted so the assignment is
// deterministic across loads; the first BASE_PALETTE.length distinct values take
// the curated palette and any beyond that get a golden-angle hue at fixed S/L so
// the map never wraps or collides.
function assignPalette(values: string[]): Map<string, string> {
  const distinct = [...new Set(values)].sort()
  const out = new Map<string, string>()
  distinct.forEach((value, i) => {
    if (i < BASE_PALETTE.length) {
      out.set(value, BASE_PALETTE[i])
    } else {
      const hue = (i * GOLDEN_ANGLE) % 360
      out.set(value, hslToHex(hue, GENERATED_S, GENERATED_L))
    }
  })
  return out
}

// buildTypeColors assigns a stable, collision-free color to each distinct node
// type. Types are sorted so the assignment is deterministic across loads. The
// first BASE_PALETTE.length types take the curated palette; any beyond that get
// a golden-angle hue at fixed S/L so the map never wraps or collides.
export function buildTypeColors(types: string[]): Map<string, string> {
  return assignPalette(types)
}

// buildGroupColors assigns a stable, collision-free color to each distinct
// group value. Groups are sorted so the assignment is deterministic across
// loads. The first BASE_PALETTE.length groups take the curated palette; any
// beyond that get a golden-angle hue at fixed S/L so the map never wraps or
// collides. When by = "type", group === type and the output is pixel-identical
// to buildTypeColors.
export function buildGroupColors(groups: string[]): Map<string, string> {
  // Exclude the empty-string sentinel used for nodes whose property field is
  // absent. Those nodes fall back to #888888 (neutral grey) in nodeColor via
  // the `?? '#888888'` default; assigning them a real palette hue would waste
  // a palette slot and make the grey fallback unreachable.
  return assignPalette(groups.filter((g) => g !== ''))
}

// Minimal #rrggbb → HSL converter. h in [0,360), s/l in [0,1]. Non-hex input
// falls back to a neutral grey so callers never crash on bad data.
export function hexToHsl(hex: string): [number, number, number] {
  const m = /^#?([0-9a-f]{6})$/i.exec(hex)
  if (!m) return [0, 0, 0.5]
  const n = parseInt(m[1], 16)
  const r = ((n >> 16) & 0xff) / 255
  const g = ((n >> 8) & 0xff) / 255
  const b = (n & 0xff) / 255
  const max = Math.max(r, g, b)
  const min = Math.min(r, g, b)
  const l = (max + min) / 2
  const d = max - min
  if (d === 0) return [0, 0, l]
  const s = l > 0.5 ? d / (2 - max - min) : d / (max + min)
  let h: number
  if (max === r) h = ((g - b) / d + (g < b ? 6 : 0)) / 6
  else if (max === g) h = ((b - r) / d + 2) / 6
  else h = ((r - g) / d + 4) / 6
  return [h * 360, s, l]
}

// Minimal HSL → #rrggbb converter. h in degrees, s/l in [0,1].
export function hslToHex(h: number, s: number, l: number): string {
  const hk = (((h % 360) + 360) % 360) / 360
  const sat = Math.min(1, Math.max(0, s))
  const lum = Math.min(1, Math.max(0, l))
  const channel = (t: number): number => {
    let tc = t
    if (tc < 0) tc += 1
    if (tc > 1) tc -= 1
    if (tc < 1 / 6) return p + (q - p) * 6 * tc
    if (tc < 1 / 2) return q
    if (tc < 2 / 3) return p + (q - p) * (2 / 3 - tc) * 6
    return p
  }
  const q = lum < 0.5 ? lum * (1 + sat) : lum + sat - lum * sat
  const p = 2 * lum - q
  const r = sat === 0 ? lum : channel(hk + 1 / 3)
  const g = sat === 0 ? lum : channel(hk)
  const b = sat === 0 ? lum : channel(hk - 1 / 3)
  const to255 = (v: number) => Math.round(v * 255).toString(16).padStart(2, '0')
  return '#' + to255(r) + to255(g) + to255(b)
}

// Brightness range: widened to amplify the hub/leaf contrast. Lightness is
// capped short of white so the hue survives at the bright (hub) end, and floored
// so dim leaves stay visible. Saturation tracks importance too, so hubs read
// both brighter and more vivid.
const L_MIN = 0.3
const L_MAX = 0.78
const S_MIN = 0.45
const S_MAX = 1.0

// Node "val" range. 3d-force-graph renders sphere radius ∝ ∛val, which compresses
// the scale hard; mapping into 3→60 yields a fixed ~2.7× radius for the top hub
// vs a min-degree leaf in any view. (The old formula was unnormalized, so its
// hub/leaf ratio drifted with the graph's max degree — typically ~1.2–2×.) The
// floor keeps leaves visible.
const SIZE_MIN_VAL = 3
const SIZE_MAX_VAL = 60

const lerp = (a: number, b: number, t: number): number => a + (b - a) * t

// importance normalizes a node's total degree to [0,1] against the rendered
// set's max, on a sqrt scale so the long tail of low-degree nodes still spreads
// out. Shared by both visual channels (size and brightness) so they stay in
// lockstep; maxDegree of 0 is safe (returns 0 → flat minimum). Also used by
// edgeAlpha to compute hub-damp.
export function importance(degree: number, maxDegree: number): number {
  if (maxDegree <= 0) return 0
  const t = Math.sqrt(Math.max(0, degree)) / Math.sqrt(maxDegree)
  return Math.min(1, Math.max(0, t))
}

// Edge-emphasis defaults. Exposed as named exports so the drawer and scene
// state can reference a single source of truth.
export const INTRA_ALPHA_DEFAULT = 0.85
export const INTER_ALPHA_DEFAULT = 0.10
export const HUB_STRENGTH_DEFAULT = 0.75
export const HUB_FLOOR = 0.2
export const DIM_FACTOR = 0.15

// rgba converts a #rrggbb hex color to an rgba() CSS string with the given
// alpha. Non-#rrggbb inputs are returned unchanged so callers never crash on
// edge-kind fallback strings like '#888'.
export function rgba(hex: string, alpha: number): string {
  const m = /^#?([0-9a-f]{2})([0-9a-f]{2})([0-9a-f]{2})$/i.exec(hex)
  if (!m) return hex
  const r = parseInt(m[1], 16)
  const g = parseInt(m[2], 16)
  const b = parseInt(m[3], 16)
  const a = Math.min(1, Math.max(0, alpha))
  return `rgba(${r},${g},${b},${a})`
}

// edgeAlpha computes the per-edge base alpha (before selection/group-focus dim).
//   sameGroup ? intraAlpha : interAlpha, then multiplied by a hub-damp factor:
//   hubDamp = clamp(1 - hubStrength * importance(max(srcDeg,tgtDeg), maxDegree), HUB_FLOOR, 1)
export function edgeAlpha(
  sameGroup: boolean,
  srcDeg: number,
  tgtDeg: number,
  maxDegree: number,
  opts: { intraAlpha: number; interAlpha: number; hubStrength: number },
): number {
  const base = sameGroup ? opts.intraAlpha : opts.interAlpha
  const maxEndpointDeg = Math.max(srcDeg, tgtDeg)
  const imp = importance(maxEndpointDeg, maxDegree)
  const hubDamp = Math.min(1, Math.max(HUB_FLOOR, 1 - opts.hubStrength * imp))
  return Math.min(1, Math.max(0, base * hubDamp))
}

// importanceColor maps a type's base hue plus its total degree to a hue-
// preserving color: more connections → higher saturation and lightness (a
// brighter, more vivid node). maxDegree normalizes the scale; 0 is safe (flat dim).
export function importanceColor(baseHex: string, degree: number, maxDegree: number): string {
  const t = importance(degree, maxDegree)
  const [h] = hexToHsl(baseHex)
  return hslToHex(h, lerp(S_MIN, S_MAX, t), lerp(L_MIN, L_MAX, t))
}

// sizeForDegree maps a node's total degree to its render val, normalized against
// the rendered set's max so the most-connected hub is always largest in any view.
// maxDegree of 0 yields the flat minimum (safe, no NaN).
export function sizeForDegree(degree: number, maxDegree: number): number {
  return lerp(SIZE_MIN_VAL, SIZE_MAX_VAL, importance(degree, maxDegree))
}

// Cluster-lens layout channel (Phase 4) tunables. ANCHOR_RADIUS sets how far
// apart group lobes sit; ANCHOR_PULL_STRENGTH is the per-group spring (0..1)
// toward the anchor; COLLIDE_RADIUS keeps huddled nodes from overlapping; the
// charge is softened so the group pull dominates without each lobe imploding.
export const ANCHOR_RADIUS = 400
export const ANCHOR_PULL_STRENGTH = 0.4
export const COLLIDE_RADIUS = 6
export const SOFT_CHARGE_STRENGTH = -20
export const DEFAULT_CHARGE_STRENGTH = -30
