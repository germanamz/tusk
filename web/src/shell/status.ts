// status.ts — the shell's live mission status, independent of the active view.
//
// The two views each own a data stream, but the chrome needs a status signal
// that persists across view switches, so the shell keeps its own lightweight
// subscription to the change stream (/api/read/stream — a bare
// {generation, epoch} frame both views advance in lockstep). It drives the
// top-bar state dot and the bottom strip. Everything shown is something the
// browser truthfully knows: connection state, the reindex generation, the bind
// host, and whether that host is loopback. Client counts and the build version
// live in the CLI footer, not here, so they are never invented on screen.

const UPDATED_MS = 1400

function isLoopbackHost(hostname: string): boolean {
  return (
    hostname === 'localhost' ||
    hostname === '127.0.0.1' ||
    hostname === '::1' ||
    hostname === '[::1]' ||
    hostname.endsWith('.localhost')
  )
}

export function initStatus(): void {
  const status = document.getElementById('mission-status') as HTMLElement
  const label = document.getElementById('status-label') as HTMLElement
  const gen = document.getElementById('status-gen') as HTMLElement
  const conn = document.getElementById('conn') as HTMLElement
  const connLabel = document.getElementById('conn-label') as HTMLElement
  const host = document.getElementById('host') as HTMLElement
  const loopback = document.getElementById('loopback') as HTMLElement

  host.textContent = location.host

  if (isLoopbackHost(location.hostname)) {
    loopback.hidden = false
  }

  let lastGen = -1
  let settleTimer = 0

  function setState(state: 'synced' | 'indexing' | 'offline' | 'connecting', text: string): void {
    status.dataset.state = state
    conn.dataset.state = state
    label.textContent = text
    connLabel.textContent = text
  }

  function settleSoon(): void {
    window.clearTimeout(settleTimer)
    settleTimer = window.setTimeout(() => setState('synced', 'SYNCED'), UPDATED_MS)
  }

  const source = new EventSource('/api/read/stream')

  source.addEventListener('open', () => setState('synced', 'SYNCED'))

  source.addEventListener('change', (event) => {
    try {
      const signal = JSON.parse((event as MessageEvent).data) as { generation?: number }

      if (typeof signal.generation === 'number') {
        gen.textContent = 'GEN ' + signal.generation

        if (lastGen >= 0 && signal.generation !== lastGen) {
          setState('indexing', 'UPDATED')
          settleSoon()
        } else {
          setState('synced', 'SYNCED')
        }

        lastGen = signal.generation

        return
      }
    } catch {
      // A malformed frame still means the link is up.
    }

    setState('synced', 'SYNCED')
  })

  source.addEventListener('error', () => setState('offline', 'OFFLINE'))
}
