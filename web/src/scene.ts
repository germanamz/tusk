import ForceGraph3D from '3d-force-graph'
import type { Graph } from './api'
import { colorForType, sizeForDegree, EDGE_KIND_COLORS } from './encode'

export interface Scene {
  setGraph(graph: Graph): void
  focus(ids: string[]): void
  instance: ReturnType<typeof ForceGraph3D>
}

export function createScene(el: HTMLElement): Scene {
  const graph = ForceGraph3D()(el)
    .backgroundColor('#0b0e14')
    .nodeId('id')
    .nodeLabel((node: any) => `${node.title || node.id} (${node.type})`)
    .nodeColor((node: any) => colorForType(node.type))
    .nodeVal((node: any) => sizeForDegree(node.degree))
    .linkColor((link: any) => EDGE_KIND_COLORS[link.kind] ?? '#888')
    .linkDirectionalArrowLength(3)

  return {
    instance: graph,
    setGraph(next: Graph) {
      graph.graphData({
        nodes: next.nodes.map((node) => ({ ...node })),
        links: next.edges.map((edge) => ({ ...edge })),
      })
    },
    focus(ids: string[]) {
      const data = graph.graphData() as { nodes: any[] }
      const target = data.nodes.find((node) => ids.includes(node.id))
      if (!target) return
      graph.cameraPosition({ x: target.x, y: target.y, z: (target.z ?? 0) + 120 }, target, 1200)
    },
  }
}
