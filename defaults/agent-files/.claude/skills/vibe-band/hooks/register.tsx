import { atom, read, update } from 'claude-code'
import type { EngineInterface, Register } from 'claude-code'

import type { BandInfo, GitInfo } from '../types'
import { parseStatus, shortenPath } from './git'

const info = atom({ plugin: 'vibe-band', key: 'info' } as const, null)

const REFRESH_MS = 2_000

let isRefreshing = false

async function git($: EngineInterface, args: string[]) {
  const ran = await $.process.run(['git', ...args], { timeoutMs: 10_000 })

  return ran.exitCode === 0 ? ran.stdout : null
}

async function refresh($: EngineInterface) {
  if (isRefreshing) return
  isRefreshing = true
  try {
    const cwd = await $.session.cwd()
    const workspace = await $.env.get('WORKSPACE_DIR')
    const project =
      (await $.env.get('SANDBOX_NAME')) ??
      (workspace ?? cwd).split('/').filter(Boolean).pop() ??
      ''
    const status = await git($, ['--no-optional-locks', 'status', '--porcelain=v2', '--branch'])
    const next: BandInfo = {
      project,
      folder: shortenPath(cwd, await $.env.get('HOME')),
      git: status === null ? null : parseStatus(status),
    }
    const prev = await read($, info)
    if (JSON.stringify(prev) !== JSON.stringify(next)) {
      await update($, info, () => next)
    }
  } catch {
    // git missing or timed out: keep what's shown
  } finally {
    isRefreshing = false
  }
}

export const register: Register = on => {
  on('session.start', async ($, e, next) => {
    const started = await next(e)
    await refresh($)
    $.clock.every(REFRESH_MS, () => void refresh($))

    return started
  })

  on('turn.complete', async ($, e, next) => {
    void refresh($)

    return next(e)
  })

  on('ui.render', { component: 'AbovePrompt' }, async ($, e, next) => {
    const band = await read($, info)
    if (e.props.hasSurvey || band === null) {
      return next(e)
    }

    const { Box, Text } = $.ui.resolve(e)

    return (
      <Box flexDirection="row" flexWrap="wrap">
        <Text color="magenta" bold>
          ⬢ {band.project}
        </Text>
        <Text dimColor> │ </Text>
        <Text color="cyan">{band.folder}</Text>
        {band.git && <Text dimColor> │ </Text>}
        {band.git && <Git Text={Text} git={band.git} />}
      </Box>
    )
  })
}

const Git = ({ Text, git }: { Text: any; git: GitInfo }) => {
  const parts: [string, string][] = []
  if (git.staged) parts.push(['green', `+${git.staged}`])
  if (git.modified) parts.push(['red', `~${git.modified}`])
  if (git.untracked) parts.push(['blue', `?${git.untracked}`])
  if (git.conflicted) parts.push(['magenta', `✘${git.conflicted}`])
  if (git.ahead) parts.push(['yellow', `↑${git.ahead}`])
  if (git.behind) parts.push(['yellow', `↓${git.behind}`])

  return (
    <Text>
      <Text color="green"> {git.branch}</Text>
      {parts.map(([color, text]) => (
        <Text color={color}> {text}</Text>
      ))}
    </Text>
  )
}
