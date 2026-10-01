// Tests for the hooks module `tusk claude install` writes. Run them with
// `make claude-plugin-check`, which installs the plugin into a scratch vault,
// copies this file beside it and runs `claude plugin test` there. The test's
// own hooks sit beneath the plugin and stand in for the engine: the
// prompt.context bottom echoes its blocks, process.run answers for the tusk
// binary, ui.toast records what the plugin shows.
import { expect, test } from 'claude-code/testing'
import type { On, ProcessRunResult } from 'claude-code'

const BASE = [{ name: 'currentDate', text: "Today's date is 2026-10-01." }]

type Run = Partial<ProcessRunResult> | { deny: string }

function stub(on: On, answer: Run) {
  const calls: (readonly string[])[] = []
  const toasts: string[] = []

  on('prompt.context', (_$, e) => ({ blocks: e.blocks }))
  on('process.run', (_$, e) => {
    calls.push(e.argv)

    if ('deny' in answer) {
      return { deny: answer.deny }
    }

    return {
      value: {
        exitCode: 0,
        stdout: '',
        stderr: '',
        isStdoutTruncated: false,
        isStderrTruncated: false,
        ...answer,
      },
    }
  })
  on('ui.toast', (_$, e) => {
    toasts.push(e.text)

    return { value: undefined }
  })

  return { calls, toasts }
}

test('appends the digest as a tusk block', async ($, on) => {
  const { calls, toasts } = stub(on, { stdout: 'This project is a tusk vault ("x").\n' })

  const { blocks } = await $.prompt.context({ blocks: BASE })

  expect(blocks.map(block => block.name)).toEqual(['currentDate', 'tusk'])
  expect(blocks[1]?.text).toBe('This project is a tusk vault ("x").')
  expect(calls[0]?.slice(1, 4)).toEqual(['claude', 'context', '--plugin-version'])
  expect(toasts).toEqual([])
})

test('replaces a tusk block already in the list', async ($, on) => {
  stub(on, { stdout: 'fresh' })

  const { blocks } = await $.prompt.context({ blocks: [...BASE, { name: 'tusk', text: 'stale' }] })

  expect(blocks.filter(block => block.name === 'tusk')).toEqual([{ name: 'tusk', text: 'fresh' }])
})

test('empty output leaves the blocks alone', async ($, on) => {
  stub(on, { stdout: '  \n' })

  const { blocks } = await $.prompt.context({ blocks: BASE })

  expect(blocks).toEqual(BASE)
})

test('a failed run leaves the blocks alone', async ($, on) => {
  stub(on, { exitCode: 1, stdout: 'partial', stderr: '' })

  const { blocks } = await $.prompt.context({ blocks: BASE })

  expect(blocks).toEqual(BASE)
})

test('a run that cannot start toasts and leaves the blocks alone', async ($, on) => {
  const { toasts } = stub(on, { deny: 'spawn tusk ENOENT' })

  const { blocks } = await $.prompt.context({ blocks: BASE })

  expect(blocks).toEqual(BASE)
  expect(toasts.length).toBe(1)
  expect(toasts[0]).toContain('tusk: could not run')
})

test('stderr shows as one toast while the block still lands', async ($, on) => {
  const { toasts } = stub(on, {
    stdout: 'digest',
    stderr: 'tusk plugin is v1.0.0, binary is v1.1.0; run tusk claude install\nmore\n',
  })

  const { blocks } = await $.prompt.context({ blocks: BASE })

  expect(toasts).toEqual(['tusk plugin is v1.0.0, binary is v1.1.0; run tusk claude install'])
  expect(blocks.at(-1)).toEqual({ name: 'tusk', text: 'digest' })
})
