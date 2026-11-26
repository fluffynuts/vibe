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

// The pull request open for the branch, as the host's gh says: a link after the status, past a divider as the parts before it are,
// opening on GitHub in a tab of its own.
function pageWithPR(pr, git = { branch: 'feat/a', web: 'https://github.com/me/vibe' }) {
  return openPage({ routes: { '/api/state': { sandbox: 'box', sandboxTabs: true, folder: '~/work/box', git, branchPullRequest: pr, attention: {} }, '/api/log': { entries: [] } } })
}

test("the branch's open pull request is a link after the status, past a divider", async () => {
  const p = await pageWithPR({ number: 40, url: 'https://github.com/me/vibe/pull/40' })
  try {
    const part = p.$('pr-part')
    assert.ok(!part.hidden)
    assert.equal(part.previousElementSibling, p.$('git-part'), 'not after the git status')
    assert.equal(part.querySelector('.sep').textContent, '│', 'no divider')
    const link = part.querySelector('a')
    assert.equal(link.tagName, 'A')
    assert.equal(link.textContent, 'PR #40')
    assert.equal(link.getAttribute('href'), 'https://github.com/me/vibe/pull/40')
    assert.equal(link.target, '_blank')
    assert.match(link.rel, /noopener/)
    assert.match(link.title, /pull request #40.*open.*this branch/i)
  } finally { p.close() }
})

test('a branch with no open pull request has no link', async () => {
  const p = await pageWithPR(null)
  try {
    assert.ok(p.$('pr-part').hidden)
  } finally { p.close() }
})

test("an address that isn't a pull request on GitHub is not linked", async () => {
  for (const url of ['javascript:alert(1)', 'https://example.com/me/vibe/pull/40', 'https://github.com/me/vibe/issues/40']) {
    const p = await pageWithPR({ number: 40, url })
    try {
      assert.ok(p.$('pr-part').hidden, url)
    } finally { p.close() }
  }
})

// The bar is drawn again only when what it says changes: drawn afresh at every poll, a link focused from the keyboard lost focus.
test('the pull request link keeps focus while the state is polled', async () => {
  const p = await pageWithPR({ number: 40, url: 'https://github.com/me/vibe/pull/40' })
  try {
    const link = p.$('pr')
    link.focus()
    await p.poll()
    assert.equal(p.window.document.activeElement, link)
    assert.ok(link.isConnected, 'drawn again')
  } finally { p.close() }
})

// A dot before the link says the pull request's status at a glance: red failing, yellow pending, green passing; a ring, not a dot, for
// a draft, still in its status's colour (a draft is often opened to watch the checks). Said in words too: its title, and to a screen
// reader, as colour alone says nothing to some.
for (const [status, words] of [['failing', /failing/i], ['pending', /pending|running|waiting/i], ['passing', /passing/i]]) {
  test('the pull request\'s status is a dot before its link: ' + status, async () => {
    const p = await pageWithPR({ number: 41, url: 'https://github.com/me/vibe/pull/41', status, why: 'the reason' })
    try {
      const dot = p.$('pr-dot')
      assert.ok(!dot.hidden)
      assert.equal(dot.nextElementSibling, p.$('pr'), 'not just before the link')
      assert.ok(dot.classList.contains(status), dot.className)
      assert.ok(!dot.classList.contains('draft'))
      assert.match(dot.getAttribute('aria-label'), words)
      assert.match(p.$('pr').title, words)
      assert.match(p.$('pr').title, /the reason/)
    } finally { p.close() }
  })
}

test('a draft pull request has a ring in its status\'s colour, and says it is a draft', async () => {
  const p = await pageWithPR({ number: 41, url: 'https://github.com/me/vibe/pull/41', draft: true, status: 'failing', why: 'build failed' })
  try {
    const dot = p.$('pr-dot')
    assert.ok(dot.classList.contains('draft') && dot.classList.contains('failing'), dot.className)
    assert.match(dot.getAttribute('aria-label'), /draft/i)
    assert.match(p.$('pr').title, /draft/i)
  } finally { p.close() }
})

test('a pull request with no status said has no dot', async () => {
  const p = await pageWithPR({ number: 41, url: 'https://github.com/me/vibe/pull/41' })
  try {
    assert.ok(p.$('pr-dot').hidden)
  } finally { p.close() }
})

// While a check is running, the dot pulses: "the checks are running" is not "half-way there" (a review waited for), and both are yellow.
test('the dot pulses while a check is running, and not while only waiting on a review', async () => {
  const running = await pageWithPR({ number: 41, url: 'https://github.com/me/vibe/pull/41', status: 'pending', running: true, why: '2 checks running' })
  try {
    assert.ok(running.$('pr-dot').classList.contains('running'), running.$('pr-dot').className)
    assert.match(running.$('pr-dot').getAttribute('aria-label'), /2 checks running/)
  } finally { running.close() }
  const waiting = await pageWithPR({ number: 41, url: 'https://github.com/me/vibe/pull/41', status: 'pending', why: 'waiting on @bob' })
  try {
    assert.ok(!waiting.$('pr-dot').classList.contains('running'), waiting.$('pr-dot').className)
  } finally { waiting.close() }
})
