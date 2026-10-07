import type { GitInfo } from '../types'

// Parses `git status --porcelain=v2 --branch` output.
export const parseStatus = (out: string): GitInfo => {
  const info: GitInfo = {
    branch: '',
    untracked: 0,
    staged: 0,
    modified: 0,
    conflicted: 0,
    ahead: 0,
    behind: 0,
    hasUpstream: false,
  }
  let oid = ''

  for (const line of out.split('\n')) {
    if (line.startsWith('# branch.oid ')) {
      oid = line.slice(13)
    } else if (line.startsWith('# branch.head ')) {
      info.branch = line.slice(14)
    } else if (line.startsWith('# branch.upstream ')) {
      info.hasUpstream = true
    } else if (line.startsWith('# branch.ab ')) {
      const match = /\+(\d+) -(\d+)/.exec(line)
      if (match) {
        info.ahead = Number(match[1])
        info.behind = Number(match[2])
      }
    } else if (line.startsWith('? ')) {
      info.untracked += 1
    } else if (line.startsWith('u ')) {
      info.conflicted += 1
    } else if (line.startsWith('1 ') || line.startsWith('2 ')) {
      const [x, y] = line.slice(2, 4)
      if (x !== '.') info.staged += 1
      if (y !== '.') info.modified += 1
    }
  }

  if (info.branch === '(detached)') {
    info.branch = oid === '(initial)' ? 'detached' : `@${oid.slice(0, 7)}`
  }

  return info
}

// Shortens a path with ~: the sandbox's own $HOME first, then the host home
// that vibe mirrors the workspace under (/home/<user>, /Users/<user>).
export const shortenPath = (path: string, home: string | undefined): string => {
  if (home && (path === home || path.startsWith(`${home}/`))) {
    return `~${path.slice(home.length)}`
  }
  const match = /^(\/home\/[^/]+|\/Users\/[^/]+)(\/.*)?$/.exec(path)

  return match ? `~${match[2] ?? ''}` : path
}
