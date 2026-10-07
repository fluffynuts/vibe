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
