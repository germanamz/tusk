// Written by `tusk claude install`. Re-run it after upgrading tusk; edits here
// are overwritten.
//
// Two hooks, each of which only moves bytes `tusk` prints:
//
//   - prompt.context appends the vault digest `tusk claude context` prints to
//     the context blocks of a conversation's first message. Claude Code
//     recomputes those blocks after /clear and compaction, so the digest is
//     re-read then too. Whatever goes wrong, the session starts with its blocks
//     untouched.
//   - tool.call runs `tusk claude refs` after a successful Edit, Write or
//     NotebookEdit and, when vault pages name the edited file, adds their list
//     to what the model reads after the tool result. Each file is reminded once
//     per session and agent. Whatever goes wrong, the result stands as it was.
import type { Register } from 'claude-code'

const BIN = __TUSK_BIN__
const VERSION = __TUSK_VERSION__
const BLOCK = 'tusk'
const TIMEOUT_MS = 5000
const REFS_TIMEOUT_MS = 3000

// reminded holds, per session and agent, the files already reminded about.
// A reload starts it over, which costs at most one repeated reminder.
const reminded = new Map<string, Set<string>>()

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

  on('tool.call', { tool: ['Edit', 'Write', 'NotebookEdit'] }, async ($, e, next) => {
    const ran = await next(e)

    if (ran.deny !== undefined || ran.isError === true) {
      return ran
    }

    const path = e.tool === 'NotebookEdit' ? e.notebook_path : e.file_path
    const key = `${await $.session.id()}:${e.agentId ?? 'main'}`
    const seen = reminded.get(key) ?? new Set<string>()

    if (typeof path !== 'string' || path === '' || seen.has(path)) {
      return ran
    }

    let refs

    try {
      // `--` keeps a path that starts with "-" from reading as a flag.
      refs = await $.process.run([BIN, 'claude', 'refs', '--', path], { timeoutMs: REFS_TIMEOUT_MS })
    } catch {
      // The prompt.context hook already toasts a binary that won't start;
      // one toast per edit would only add noise.
      return ran
    }

    const text = refs.stdout.trim()

    if (refs.exitCode !== 0 || text === '') {
      return ran
    }

    seen.add(path)
    reminded.set(key, seen)

    return { ...ran, context: [...(ran.context ?? []), text] }
  })
}
