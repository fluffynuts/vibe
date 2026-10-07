import { expect, test } from 'claude-code/testing'

import { parseStatus, shortenPath } from './git'

test('parses branch, counts and ahead/behind', async () => {
  const out = [
    '# branch.oid 54272f8abcdef',
    '# branch.head master',
    '# branch.upstream origin/master',
    '# branch.ab +3 -1',
    '# stash 2',
    '1 M. N... 100644 100644 100644 a b staged.go',
    '1 .M N... 100644 100644 100644 a b modified.go',
    '1 MM N... 100644 100644 100644 a b both.go',
    '2 R. N... 100644 100644 100644 a b R100 new.go\told.go',
    'u UU N... 100644 100644 100644 100644 a b c conflict.go',
    '? new1.txt',
    '? new2.txt',
    '',
  ].join('\n')

  expect(parseStatus(out)).toEqual({
    branch: 'master',
    untracked: 2,
    staged: 3,
    modified: 2,
    conflicted: 1,
    ahead: 3,
    behind: 1,
    stashed: 2,
    hasUpstream: true,
  })
})

test('detached head shows the short oid', async () => {
  const out = '# branch.oid 54272f8abcdef\n# branch.head (detached)\n'
  expect(parseStatus(out).branch).toBe('@54272f8')
})

test('shortens home paths with ~', async () => {
  expect(shortenPath('/home/agent/x', '/home/agent')).toBe('~/x')
  expect(shortenPath('/home/davydm/code/opensource/vibe', '/home/agent')).toBe(
    '~/code/opensource/vibe',
  )
  expect(shortenPath('/Users/someone', undefined)).toBe('~')
  expect(shortenPath('/opt/work', '/home/agent')).toBe('/opt/work')
})
