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

// The clipboard feature's `clipboard-url`, when that feature is installed.
async function clipboardUrl($: EngineInterface, home: string | undefined) {
  try {
    const ran = await $.process.run([`${home ?? '/home/agent'}/.local/bin/clipboard-url`], { timeoutMs: 5_000 })
    const url = ran.exitCode === 0 ? ran.stdout.trim() : ''

    return url === '' ? null : url
  } catch {
    return null
  }
}

// The latest URL diffity-url gave the user, while a diffity viewer is still
// running (the file outlives the viewer, so a stale one isn't shown).
async function reviewUrl($: EngineInterface, home: string | undefined) {
  try {
    const url = (await $.fs.read(`${home ?? '/home/agent'}/.local/state/vibe/diffity-review-url`)).trim()
    if (url === '') return null
    const ran = await $.process.run(['diffity', 'list', '--json'], { timeoutMs: 5_000 })
    const sessions = ran.exitCode === 0 ? JSON.parse(ran.stdout) : null

    return Array.isArray(sessions) && sessions.length > 0 ? url : null
  } catch {
    return null
  }
}

async function refresh($: EngineInterface) {
  if (isRefreshing) return
  isRefreshing = true
  try {
    const cwd = await $.session.cwd()
    const workspace = await $.env.get('WORKSPACE_DIR')
    // the title names the project, so it follows the workspace, not wherever the agent has cd'd to
    const projectDir = workspace ?? cwd
    const project =
      (await $.env.get('SANDBOX_NAME')) ??
      projectDir.split('/').filter(Boolean).pop() ??
      ''
    const home = await $.env.get('HOME')
    const status = await git($, ['--no-optional-locks', 'status', '--porcelain=v2', '--branch', '--show-stash'])
    const next: BandInfo = {
      project,
      folder: shortenPath(projectDir, home),
      git: status === null ? null : parseStatus(status),
      clipboardUrl: await clipboardUrl($, home),
      reviewUrl: await reviewUrl($, home),
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
      <Box flexDirection="column">
        <Box flexDirection="row" flexWrap="wrap">
          <Text color="magenta" bold>
            ⬢ {band.project}
          </Text>
          <Text dimColor> │ </Text>
          <Text color="cyan">{band.folder}</Text>
          {band.git && <Text dimColor> │ </Text>}
          {band.git && <Git Text={Text} git={band.git} />}
        </Box>
        {band.reviewUrl && (
          <Text>
            <Text dimColor>review at: </Text>
            <Text color="cyan">{band.reviewUrl}</Text>
          </Text>
        )}
        {band.clipboardUrl && (
          <Text>
            <Text dimColor>copy-paste at: </Text>
            <Text color="cyan">{band.clipboardUrl}</Text>
          </Text>
        )}
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
  if (git.stashed) parts.push(['green', `#${git.stashed}`])

  return (
    <Text>
      <Text color="green"> {git.branch}</Text>
      {parts.map(([color, text]) => (
        <Text color={color}> {text}</Text>
      ))}
    </Text>
  )
}
