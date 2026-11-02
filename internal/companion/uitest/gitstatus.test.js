// The git status at the top of the page, in the same visual language as the command line's prompt
// (powerline): what each part says is tested here, what colour it has in firefox.test.js.
'use strict'
const test = require('node:test')
const assert = require('node:assert/strict')
const { openPage } = require('./page.js')

function pageWithGit(git) {
  return openPage({ routes: { '/api/state': { sandbox: 'box', sandboxTabs: true, folder: '~/work/box', git, attention: {} }, '/api/log': { entries: [] } } })
}

const parts = (p) => [...p.$('git').children].map((span) => [span.className, span.textContent])

test('every part of the status says what it is in the prompt\'s glyphs, in the prompt\'s order', async () => {
  const p = await pageWithGit({ branch: 'main', staged: 2, modified: 3, untracked: 1, conflicted: 1, ahead: 1, behind: 2, stashed: 3 })
  try {
    assert.deepEqual(parts(p), [
      ['branch', 'main'],
      ['staged', '+2'],
      ['modified', '!3'],
      ['untracked', '?1'],
      ['conflicted', '✘1'],
      ['ahead', '⇡1'],
      ['behind', '⇣2'],
      ['stashed', '#3'],
    ])
  } finally { p.close() }
})

test('a part that is none is not shown', async () => {
  const p = await pageWithGit({ branch: 'main', staged: 0, modified: 0, untracked: 0, conflicted: 0, ahead: 1, behind: 0, stashed: 0 })
  try {
    assert.deepEqual(parts(p), [['branch', 'main'], ['ahead', '⇡1']])
  } finally { p.close() }
})

test('every part has a title that says it in words, since a glyph is a convention', async () => {
  const p = await pageWithGit({ branch: 'main', staged: 2, modified: 3, untracked: 1, conflicted: 1, ahead: 1, behind: 2, stashed: 3 })
  try {
    const titles = Object.fromEntries([...p.$('git').children].map((span) => [span.className, span.title]))
    assert.match(titles.staged, /2 staged/)
    assert.match(titles.modified, /3 modified/)
    assert.match(titles.untracked, /1 untracked/)
    assert.match(titles.conflicted, /1 conflicted/)
    assert.match(titles.ahead, /1 commit.*not pushed/)
    assert.match(titles.behind, /2 commits.*fetched.*not merged/)
    assert.match(titles.stashed, /3 stash/)
  } finally { p.close() }
})

test('an operation under way is said, after the branch and before the counts', async () => {
  const p = await pageWithGit({ branch: 'main', staged: 0, modified: 0, untracked: 0, conflicted: 2, ahead: 0, behind: 0, stashed: 0, operation: 'rebase 2/5' })
  try {
    assert.deepEqual(parts(p), [['branch', 'main'], ['operation', 'rebase 2/5'], ['conflicted', '✘2']])
    assert.match([...p.$('git').children][1].title, /rebase 2\/5.*under way/)
  } finally { p.close() }
})

test('a branch that follows none says so, and a detached head says it is one', async () => {
  const p = await pageWithGit({ branch: 'topic', staged: 0, modified: 0, untracked: 0, conflicted: 0, ahead: 0, behind: 0, stashed: 0, noUpstream: true })
  const q = await pageWithGit({ branch: '@1a2b3c4', staged: 0, modified: 0, untracked: 0, conflicted: 0, ahead: 0, behind: 0, stashed: 0, detached: true })
  try {
    assert.deepEqual(parts(p), [['branch', 'topic'], ['noupstream', 'no upstream']])
    assert.match([...p.$('git').children][1].title, /follows no branch/)
    assert.deepEqual(parts(q), [['branch detached', '@1a2b3c4']])
    assert.match([...q.$('git').children][0].title, /detached HEAD/)
  } finally { p.close(); q.close() }
})
