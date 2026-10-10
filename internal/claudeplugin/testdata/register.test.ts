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

// The tool.call tests stand in for the engine beneath the plugin: tool.call
// answers as the edit tool would, process.run answers for `tusk claude refs`,
// session.id names the session. Each test uses its own session id, since the
// plugin remembers reminded files per session for as long as it is loaded.
type Edit = { isError?: boolean; stdout?: string; exitCode?: number; session: string }

function stubEdit(on: On, edit: Edit) {
  const calls: (readonly string[])[] = []

  on('tool.call', () =>
    edit.isError
      ? { isError: true as const, result: 'boom', text: 'boom' }
      : { result: { ok: true }, text: 'edited' })
  on('process.run', (_$, e) => {
    calls.push(e.argv)

    return {
      value: {
        exitCode: edit.exitCode ?? 0,
        stdout: edit.stdout ?? '',
        stderr: '',
        isStdoutTruncated: false,
        isStderrTruncated: false,
      },
    }
  })
  on('session.id', () => ({ value: edit.session }))

  return calls
}

const REMINDER = 'tusk: 1 page names server/a.go. Check whether this edit changes what they say.\n- technical/a\n'

test('an edit to a named file adds the reminder after the result', async ($, on) => {
  const calls = stubEdit(on, { stdout: REMINDER, session: 's-named' })

  const ran = await $.tool.call({ tool: 'Edit', file_path: '/vault/server/a.go', old_string: 'x', new_string: 'y' })

  expect(calls.length).toBe(1)
  expect(calls[0]?.slice(1)).toEqual(['claude', 'refs', '--', '/vault/server/a.go'])
  expect(ran.text).toBe('edited')
  expect(ran.context).toEqual([REMINDER.trim()])
})

test('a failed edit runs nothing', async ($, on) => {
  const calls = stubEdit(on, { isError: true, stdout: REMINDER, session: 's-failed' })

  const ran = await $.tool.call({ tool: 'Write', file_path: '/vault/server/a.go', content: 'x' })

  expect(ran.isError).toBe(true)
  expect(calls.length).toBe(0)
  expect(ran.context).toBe(undefined)
})

test('other tools are left alone', async ($, on) => {
  const calls = stubEdit(on, { stdout: REMINDER, session: 's-read' })

  await $.tool.call({ tool: 'Read', file_path: '/vault/server/a.go' })

  expect(calls.length).toBe(0)
})

test('no output adds nothing', async ($, on) => {
  const quiet = stubEdit(on, { stdout: '  \n', session: 's-quiet' })

  const ran = await $.tool.call({ tool: 'Edit', file_path: '/vault/server/b.go', old_string: 'x', new_string: 'y' })

  expect(quiet.length).toBe(1)
  expect(ran.context).toBe(undefined)
})

test('a run that exits non-zero adds nothing', async ($, on) => {
  stubEdit(on, { stdout: REMINDER, exitCode: 1, session: 's-exit' })

  const ran = await $.tool.call({ tool: 'Edit', file_path: '/vault/server/a.go', old_string: 'x', new_string: 'y' })

  expect(ran.context).toBe(undefined)
})

test('a file is reminded once per session and agent', async ($, on) => {
  const calls = stubEdit(on, { stdout: REMINDER, session: 's-once' })
  const edit = { tool: 'Edit' as const, file_path: '/vault/server/a.go', old_string: 'x', new_string: 'y' }

  const first = await $.tool.call(edit)
  const again = await $.tool.call(edit)
  const subagent = await $.tool.call({ ...edit, agentId: 'helper' })

  expect(first.context).toEqual([REMINDER.trim()])
  expect(again.context).toBe(undefined)
  expect(subagent.context).toEqual([REMINDER.trim()])
  expect(calls.length).toBe(2)
})

test('a notebook edit looks up its notebook path', async ($, on) => {
  const calls = stubEdit(on, { stdout: REMINDER, session: 's-notebook' })

  await $.tool.call({ tool: 'NotebookEdit', notebook_path: '/vault/analysis/run.ipynb', new_source: 'x' })

  expect(calls[0]?.at(-1)).toBe('/vault/analysis/run.ipynb')
})
