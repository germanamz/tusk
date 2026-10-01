// Written by `tusk claude install`. Re-run it after upgrading tusk; edits here
// are overwritten.
//
// Appends the vault digest `tusk claude context` prints to the context blocks
// of a conversation's first message. Claude Code recomputes those blocks after
// /clear and compaction, so the digest is re-read then too. Whatever goes
// wrong, the session starts with its blocks untouched.
import type { Register } from 'claude-code'

const BIN = __TUSK_BIN__
const VERSION = __TUSK_VERSION__
const BLOCK = 'tusk'
const TIMEOUT_MS = 5000

export const register: Register = on => {
  on('prompt.context', async ($, e, next) => {
    const below = await next(e)
    const argv = [BIN, 'claude', 'context', '--plugin-version', VERSION]
    let ran

    try {
      ran = await $.process.run(argv, { timeoutMs: TIMEOUT_MS })
    } catch (error) {
      $.ui.toast(`tusk: could not run ${BIN}: ${String(error)}`)

      return below
    }

    const warning = ran.stderr.trim().split('\n')[0] ?? ''

    if (warning !== '') {
      $.ui.toast(warning)
    }

    const text = ran.stdout.trim()

    if (ran.exitCode !== 0 || text === '') {
      return below
    }

    const blocks = below.blocks.filter(block => block.name !== BLOCK)

    return { ...below, blocks: [...blocks, { name: BLOCK, text }] }
  })
}
