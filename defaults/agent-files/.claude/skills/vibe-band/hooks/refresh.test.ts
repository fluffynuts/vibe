import { expect, mock, test } from 'claude-code/testing'

import type { BandInfo } from '../types'

test('refreshes on the timer, with no turn in between', async ($, on) => {
  const clock = mock.clock(on)
  mock.env(on, { HOME: '/home/agent', SANDBOX_NAME: 'vibe' })
  let shown: BandInfo | null = null
  let version = 0
  on('state.get', () => ({ value: { value: shown, version } }))
  on('state.set', (_, e) => {
    shown = e.value as BandInfo
    version += 1

    return { value: { version } }
  })
  on('session.start', (_, e) => ({ cwd: e.cwd }))
  on('session.cwd', () => ({ value: '/home/agent/vibe' }))
  let status = '# branch.head master\n'
  on('process.run', () => ({ value: { exitCode: 0, stdout: status, stderr: '' } }))

  await $.session.start({ cwd: '/home/agent/vibe', surface: 'terminal', isInteractive: true })
  await clock.settle()
  expect(shown?.git?.untracked).toBe(0)

  status += '? new.txt\n'
  await clock.advance(2_000)
  expect(shown?.git?.untracked).toBe(1)
})

test('says the clipboard page is starting until clipboard-url says it is ready', async ($, on) => {
  const clock = mock.clock(on)
  mock.env(on, { HOME: '/home/agent', SANDBOX_NAME: 'vibe' })
  let shown: BandInfo | null = null
  let version = 0
  on('state.get', () => ({ value: { value: shown, version } }))
  on('state.set', (_, e) => {
    shown = e.value as BandInfo
    version += 1

    return { value: { version } }
  })
  on('session.start', (_, e) => ({ cwd: e.cwd }))
  on('session.cwd', () => ({ value: '/home/agent/vibe' }))
  on('fs.read', () => {
    throw new Error('no review url')
  })
  let ready = false
  on('process.run', (_, e) => {
    if (e.argv[0].endsWith('/clipboard-url')) {
      return { value: ready ? { exitCode: 0, stdout: 'http://localhost:5405\n', stderr: '' } : { exitCode: 3, stdout: '', stderr: '' } }
    }

    return { value: { exitCode: 0, stdout: '# branch.head master\n', stderr: '' } }
  })

  await $.session.start({ cwd: '/home/agent/vibe', surface: 'terminal', isInteractive: true })
  await clock.settle()
  expect(shown?.clipboard).toBe('starting')

  ready = true
  await clock.advance(2_000)
  expect(shown?.clipboard).toEqual({ url: 'http://localhost:5405' })
})
