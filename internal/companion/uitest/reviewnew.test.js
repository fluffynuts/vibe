// A review the agent starts from the terminal (`vibe-review start`) replaces the one the Review tab holds, and its comments arrive by
// the page's polls, which ask for the review without its diff. The diff the page holds is the old review's, so the new comments sat
// under "On files not in this diff", bunched at the top, until Refresh. A review that is not the one the diff was worked out for
// has its diff worked out afresh.
'use strict'
const test = require('node:test')
const assert = require('node:assert/strict')
const { openPage } = require('./page.js')

const lineOf = (path) => ({ path, status: 'modified', lines: 1, fingerprint: 'f-' + path, hunks: [{ oldStart: 1, newStart: 1, lines: [{ kind: 'add', new: 1, text: 'x' }] }] })
const reviewOf = (started, ref, threads) => ({ threads, lastId: threads.length, started, ref, state: 'done' })
const thread = (id, file) => ({ id, file, side: 'new', line: 1, endLine: 1, comments: [{ author: 'agent', body: 'found', at: '2026-11-16T10:00:00Z' }], status: 'open' })

async function pageWith(world) {
  const p = await openPage({
    routes: {
      '/api/state': { sandbox: 'box', sandboxTabs: true, review: true, conversation: true, reviewAt: '', attention: {} },
      '/api/review': (call) => (call.query.includes('diff=1')
        ? { review: world.review, diff: world.diff, diffKey: 'k-' + world.review.started }
        : { review: world.review }),
      '/api/conversation': { events: [], last: 0, running: false },
      '/api/log': { entries: [] },
    },
    storage: { 'vibe-companion-tab': 'review' },
  })
  return p
}

const diffLoads = (p) => p.calls.filter((c) => c.path === '/api/review' && c.query.includes('diff=1')).length

test("a review started elsewhere has its diff worked out afresh, so its comments sit on their lines and not at the top", async () => {
  const world = { review: reviewOf('2026-11-16T09:00:00Z', 'master', [thread(1, 'a.go')]), diff: [lineOf('a.go')] }
  const p = await pageWith(world)
  try {
    assert.equal(p.$('review-general').querySelectorAll('.review-thread').length, 0, 'the first review is placed')
    const loads = diffLoads(p)

    world.review = reviewOf('2026-11-16T11:00:00Z', 'master', [thread(1, 'b.go')])
    world.diff = [lineOf('b.go')]
    await p.poll()
    await p.poll()

    assert.ok(diffLoads(p) > loads, 'the new review\'s diff was asked for')
    assert.equal(p.$('review-general').querySelectorAll('.review-thread').length, 0, 'no comment is bunched at the top')
    assert.equal(p.$('review-files').querySelectorAll('.review-thread').length, 1, 'it is under its line, in its file')
  } finally { p.close() }
})

test('polls of the same review do not ask for its diff again', async () => {
  const world = { review: reviewOf('2026-11-16T09:00:00Z', 'master', [thread(1, 'a.go')]), diff: [lineOf('a.go')] }
  const p = await pageWith(world)
  try {
    const loads = diffLoads(p)
    world.review = reviewOf('2026-11-16T09:00:00Z', 'master', [thread(1, 'a.go'), thread(2, 'a.go')])
    await p.poll()
    await p.poll()
    assert.equal(diffLoads(p), loads, 'a comment added to the review it holds is no reason to lose the place in the diff')
  } finally { p.close() }
})

test('a review started from the terminal while the tab showed no review at all gets its diff too', async () => {
  // The tab was open on the start screen: no review, and a diff of nothing.
  const world = { review: { threads: [], lastId: 0, started: '0001-01-01T00:00:00Z' }, diff: [] }
  const p = await pageWith(world)
  try {
    const loads = diffLoads(p)
    world.review = reviewOf('2026-11-16T11:00:00Z', 'master', [thread(1, 'b.go')])
    world.diff = [lineOf('b.go')]
    await p.poll()
    await p.poll()
    assert.ok(diffLoads(p) > loads, "the review's diff was asked for")
    assert.equal(p.$('review-general').querySelectorAll('.review-thread').length, 0, 'no comment is bunched at the top')
    assert.equal(p.$('review-files').querySelectorAll('.review-thread').length, 1)
  } finally { p.close() }
})
