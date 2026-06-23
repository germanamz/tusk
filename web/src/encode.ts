const PALETTE = ['#4f8cff', '#ff6b6b', '#51cf66', '#ffd43b', '#cc5de8', '#22b8cf', '#ff922b', '#94d82d']

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

export function colorForType(type: string): string {
  let hash = 0
  for (let i = 0; i < type.length; i++) hash = (hash * 31 + type.charCodeAt(i)) >>> 0
  return PALETTE[hash % PALETTE.length]
}

export function sizeForDegree(degree: number): number {
  return 2 + Math.sqrt(degree) * 1.5
}
