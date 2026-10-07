export type GitInfo = {
  branch: string
  untracked: number
  staged: number
  modified: number
  conflicted: number
  ahead: number
  behind: number
  stashed: number
  hasUpstream: boolean
}

export type BandInfo = {
  project: string
  folder: string
  git: GitInfo | null
}

declare module 'claude-code' {
  interface PluginState {
    'vibe-band': { info: BandInfo | null }
  }
}
