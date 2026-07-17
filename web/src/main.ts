// main.ts — the unified web app entry point.
//
// It brings up the theme before anything paints (so the first frame uses the
// right palette), then wires the console shell, which mounts the initial view.
// The two views (graph, reader) load lazily through the router.

import './theme/tokens.css'
import './shell/shell.css'
import { initTheme } from './theme/theme'
import { initShell } from './shell/shell'

initTheme()
initShell()
