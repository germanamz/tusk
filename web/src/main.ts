import { fetchGraph } from './api'
import { createScene } from './scene'
import { subscribeGraph } from './stream'

async function boot(): Promise<void> {
  const el = document.getElementById('graph')!
  const scene = createScene(el)

  scene.setGraph(await fetchGraph())
  subscribeGraph((graph) => scene.setGraph(graph))
}

void boot()
