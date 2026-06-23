import { execFileSync } from 'node:child_process'
import { fileURLToPath } from 'node:url'
import path from 'node:path'

// The fixture's index (.tusk/) is gitignored and `tusk graph` serves whatever
// is already indexed without re-reading content, so a fresh checkout would
// start the server against an empty index. Build it once before the suite so
// the run is self-contained on any machine.
export default function globalSetup(): void {
  const here = path.dirname(fileURLToPath(import.meta.url))
  const bin = path.resolve(here, '../../bin/tusk')
  const fixture = path.resolve(here, 'fixture')
  execFileSync(bin, ['reindex'], { cwd: fixture, stdio: 'inherit' })
}
