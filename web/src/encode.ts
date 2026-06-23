const PALETTE = ['#4f8cff', '#ff6b6b', '#51cf66', '#ffd43b', '#cc5de8', '#22b8cf', '#ff922b', '#94d82d']

export const EDGE_KIND_COLORS: Record<string, string> = {
  direct: '#7aa2f7',
  derived: '#9d7cd8',
  structural: '#565f89',
}

export function colorForType(type: string): string {
  let hash = 0
  for (let i = 0; i < type.length; i++) hash = (hash * 31 + type.charCodeAt(i)) >>> 0
  return PALETTE[hash % PALETTE.length]
}

export function sizeForDegree(degree: number): number {
  return 2 + Math.sqrt(degree) * 1.5
}
