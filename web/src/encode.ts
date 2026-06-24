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

// buildTypeColors assigns a stable, collision-free color to each distinct node
// type. Types are sorted so the assignment is deterministic across loads. The
// first BASE_PALETTE.length types take the curated palette; any beyond that get
// a golden-angle hue at fixed S/L so the map never wraps or collides.
export function buildTypeColors(types: string[]): Map<string, string> {
  const distinct = [...new Set(types)].sort()
  const out = new Map<string, string>()
  distinct.forEach((type, i) => {
    if (i < BASE_PALETTE.length) {
      out.set(type, BASE_PALETTE[i])
    } else {
      const hue = (i * GOLDEN_ANGLE) % 360
      out.set(type, hslToHex(hue, GENERATED_S, GENERATED_L))
    }
  })
  return out
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

// Brightness range: cap lightness short of white so the hue survives at the
// bright (hub) end, and floor it so dim leaves stay visible. Saturation tracks
// importance too, so hubs read both brighter and more vivid.
const L_MIN = 0.36
const L_MAX = 0.68
const S_MIN = 0.45
const S_MAX = 0.9

const lerp = (a: number, b: number, t: number): number => a + (b - a) * t

// importanceColor maps a type's base hue plus its in-degree to a hue-preserving
// color: more incoming links → higher saturation and lightness (a brighter,
// more vivid node). maxInDegree normalizes the scale; 0 is safe (flat dim).
export function importanceColor(baseHex: string, inDegree: number, maxInDegree: number): string {
  let t = maxInDegree > 0 ? Math.sqrt(Math.max(0, inDegree)) / Math.sqrt(maxInDegree) : 0
  t = Math.min(1, Math.max(0, t))
  const [h] = hexToHsl(baseHex)
  return hslToHex(h, lerp(S_MIN, S_MAX, t), lerp(L_MIN, L_MAX, t))
}

// sizeForDegree keeps its monotonic shape but is now fed in-degree. The floor of
// 2 keeps in-degree-0 nodes visible but smallest.
export function sizeForDegree(degree: number): number {
  return 2 + Math.sqrt(degree) * 1.5
}
