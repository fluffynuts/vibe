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
      '/api/review': async (call) => {
        if (!call.query.includes('diff=1')) {
          if (world.holdReview) await world.holdReview
          return { review: world.review }
        }
        if (world.holdDiff) await world.holdDiff
        return { review: world.review, diff: world.diff, diffKey: 'k-' + world.review.started }
      },
      '/api/review/do': (call) => (world.refuse ? { status: 409, body: world.refuse } : { review: world.review }),
      '/api/review/close': (call) => (world.refuse ? { status: 409, body: world.refuse } : { review: Object.assign({}, world.review, { closed: 'Closed.' }) }),
      '/api/review/submit': (call) => (world.refuse ? { status: 409, body: world.refuse } : { ok: true, review: world.review }),
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

test('while the new review\'s diff is worked out the old review is not left on show: the tab says it is working, then draws the new one', async () => {
  const world = { review: reviewOf('2026-11-16T09:00:00Z', 'master', [thread(1, 'a.go')]), diff: [lineOf('a.go')] }
  const p = await pageWith(world)
  try {
    assert.equal(p.$('review-body').hidden, false, 'the first review is shown')
    assert.equal(p.$('review-wait').hidden, true)

    let release
    world.holdDiff = new Promise((resolve) => { release = resolve })
    world.review = reviewOf('2026-11-16T11:00:00Z', 'master', [thread(1, 'b.go')])
    world.diff = [lineOf('b.go')]
    await p.kick('tick')
    await p.kick('tick')

    assert.equal(p.$('review-wait').hidden, false, 'it says the diff is being worked out')
    assert.equal(p.$('review-body').hidden, true, 'and the old review, with the new one not yet drawable, is not shown')

    release()
    await p.poll()
    assert.equal(p.$('review-wait').hidden, true)
    assert.equal(p.$('review-body').hidden, false)
    assert.equal(p.$('review-files').querySelectorAll('.review-thread').length, 1, 'the new review is drawn, its comment on its line')
    assert.equal(p.$('review-general').querySelectorAll('.review-thread').length, 0)
  } finally { p.close() }
})

// What was half-written in the review that is replaced belongs to it: thread numbers start again at 1 in the next, so a draft
// kept would come back in a different review's comment.
const replyBox = (p) => p.$('review-files').querySelector('textarea[data-thread="1"]')
const composeBox = (p) => p.$('review-files').querySelector('textarea[aria-label="Your comment"]')
const secondReview = (world, path = 'b.go') => {
  world.review = reviewOf('2026-11-16T11:00:00Z', 'master', [thread(1, path)])
  world.diff = [lineOf(path)]
}

test("a reply half-written in the review that is replaced does not come back in the new review's comment with the same number", async () => {
  const world = { review: reviewOf('2026-11-16T09:00:00Z', 'master', [thread(1, 'a.go')]), diff: [lineOf('a.go')] }
  const p = await pageWith(world)
  try {
    await p.type(replyBox(p), 'half a reply to the old one')
    assert.equal(replyBox(p).value, 'half a reply to the old one')
    secondReview(world)
    await p.poll()
    await p.poll()
    assert.equal(replyBox(p).value, '', 'the new #1 starts empty')
  } finally { p.close() }
})

test("a comment half-written on a line of the review that is replaced is not left open on the same line of the new one", async () => {
  const world = { review: reviewOf('2026-11-16T09:00:00Z', 'master', [thread(1, 'a.go')]), diff: [lineOf('a.go')] }
  const p = await pageWith(world)
  try {
    await p.click(p.$('review-files').querySelector('[aria-label="Comment on line R1"]'))
    await p.type(composeBox(p), 'about the old line')
    composeBox(p).blur()
    secondReview(world, 'a.go')
    world.diff = [Object.assign(lineOf('a.go'), { fingerprint: 'changed' })]
    await p.poll()
    await p.poll()
    assert.equal(composeBox(p), null, 'no comment box is left open, with the old text, on the new review')
  } finally { p.close() }
})

test("someone typing in a box is not parted from it: the new review waits until they leave the box, then replaces the old", async () => {
  const world = { review: reviewOf('2026-11-16T09:00:00Z', 'master', [thread(1, 'a.go')]), diff: [lineOf('a.go')] }
  const p = await pageWith(world)
  try {
    const box = replyBox(p)
    await p.type(box, 'mid-sentence')
    box.focus()
    const loads = diffLoads(p)
    secondReview(world)
    await p.poll()
    await p.poll()
    assert.equal(diffLoads(p), loads, 'the diff is not asked for while they type')
    assert.equal(p.$('review-body').hidden, false, 'the review they are in is not hidden from under them')
    assert.equal(replyBox(p), box, 'and the box they are in is the one they had')
    assert.equal(box.value, 'mid-sentence')

    box.blur()
    await p.poll()
    await p.poll()
    assert.ok(diffLoads(p) > loads, 'once they have left it, the new review is worked out')
    assert.equal(p.$('review-general').querySelectorAll('.review-thread').length, 0)
    assert.equal(replyBox(p).value, '', 'and its comment starts empty')
  } finally { p.close() }
})

// Holding the new review back from someone typing is said, once, with a way out.
const toastText = (p) => (p.$('toast').hidden ? '' : p.$('toast').textContent)

test('the user is told why the review has not changed while they are in a box, once however many polls pass', async () => {
  const world = { review: reviewOf('2026-11-16T09:00:00Z', 'master', [thread(1, 'a.go')]), diff: [lineOf('a.go')] }
  const p = await pageWith(world)
  try {
    assert.equal(toastText(p), '', 'nothing to say yet')
    const box = replyBox(p)
    await p.type(box, 'mid-sentence')
    box.focus()
    secondReview(world)
    await p.poll()
    assert.match(toastText(p), /agent has started a new review.*leave the comment box/s)
    assert.match(toastText(p), /Switch now/)

    p.$('toast').hidden = true
    await p.poll()
    await p.poll()
    assert.equal(toastText(p), '', 'it is not said again at every poll')
  } finally { p.close() }
})

test('Switch now takes the new review at once, dropping what was written in the old', async () => {
  const world = { review: reviewOf('2026-11-16T09:00:00Z', 'master', [thread(1, 'a.go')]), diff: [lineOf('a.go')] }
  const p = await pageWith(world)
  try {
    const box = replyBox(p)
    await p.type(box, 'mid-sentence')
    box.focus()
    secondReview(world)
    await p.poll()
    const loads = diffLoads(p)

    await p.click(p.$('toast').querySelector('.toast-action'))
    await p.poll()

    assert.ok(diffLoads(p) > loads, 'the new review\'s diff was asked for')
    assert.equal(p.$('review-general').querySelectorAll('.review-thread').length, 0)
    assert.equal(p.$('review-files').querySelectorAll('.review-thread').length, 1, 'the new review is drawn')
    assert.equal(replyBox(p).value, '', 'with nothing of the old one written in it')
  } finally { p.close() }
})

test('a review nobody is typing against is replaced with no word said about holding it', async () => {
  const world = { review: reviewOf('2026-11-16T09:00:00Z', 'master', [thread(1, 'a.go')]), diff: [lineOf('a.go')] }
  const p = await pageWith(world)
  try {
    secondReview(world)
    await p.poll()
    await p.poll()
    assert.doesNotMatch(toastText(p), /leave the comment box/)
  } finally { p.close() }
})

// The host refuses a change meant for a review the agent has replaced, and the page says which review it was showing.
const replied = (p) => p.sent('/api/review/do')

test('a change from the page names the review it was shown', async () => {
  const world = { review: reviewOf('2026-11-16T09:00:00Z', 'master', [thread(1, 'a.go')]), diff: [lineOf('a.go')] }
  const p = await pageWith(world)
  try {
    await p.type(replyBox(p), 'yes')
    await p.click(p.button('Reply', p.$('review-files')))
    assert.deepEqual(replied(p).pop().for, { started: '2026-11-16T09:00:00Z', ref: 'master' })
  } finally { p.close() }
})

test('a change the host refuses because the agent has replaced the review is answered by taking the new review, with the reason said', async () => {
  const world = { review: reviewOf('2026-11-16T09:00:00Z', 'master', [thread(1, 'a.go')]), diff: [lineOf('a.go')] }
  const p = await pageWith(world)
  try {
    await p.type(replyBox(p), 'yes')
    // The page has not polled since: the agent started the review meanwhile.
    secondReview(world)
    world.refuse = { error: 'the agent has replaced this review with another: what you did was not applied to it', code: 'review-replaced' }
    await p.click(p.button('Reply', p.$('review-files')))

    assert.match(toastText(p), /agent has replaced this review/, 'the reason is said')
    assert.equal(p.$('review-general').querySelectorAll('.review-thread').length, 0, 'nothing is bunched at the top')
    assert.equal(p.$('review-files').querySelectorAll('.review-thread').length, 1, 'the new review is drawn')
    assert.equal(replyBox(p).value, '')
  } finally { p.close() }
})

test('Submit refused for a replaced review is answered the same way', async () => {
  const mine = { author: 'user', body: 'to send', at: '2026-11-16T10:05:00Z' }
  const first = reviewOf('2026-11-16T09:00:00Z', 'master', [Object.assign(thread(1, 'a.go'), { comments: [thread(1, 'a.go').comments[0], mine] })])
  const world = { review: first, diff: [lineOf('a.go')] }
  const p = await pageWith(world)
  try {
    secondReview(world)
    world.refuse = { error: 'the agent has replaced this review with another: what you did was not applied to it', code: 'review-replaced' }
    const loads = diffLoads(p)
    await p.click(p.$('review-submit'))
    assert.deepEqual(p.sent('/api/review/submit').pop().for, { started: '2026-11-16T09:00:00Z', ref: 'master' })
    assert.ok(diffLoads(p) > loads, 'the new review is taken')
    assert.equal(p.$('review-files').querySelectorAll('.review-thread').length, 1)
  } finally { p.close() }
})

// The reason a review is waiting stays on show after the toast has gone, until it is replaced.
const heldNote = (p) => (p.$('review-held').hidden ? '' : p.$('review-held').textContent)

test('while a new review is held back, a note beside Refresh says so for as long as it is, and goes when it is replaced', async () => {
  const world = { review: reviewOf('2026-11-16T09:00:00Z', 'master', [thread(1, 'a.go')]), diff: [lineOf('a.go')] }
  const p = await pageWith(world)
  try {
    assert.equal(heldNote(p), '')
    const box = replyBox(p)
    await p.type(box, 'mid-sentence')
    box.focus()
    secondReview(world)
    await p.poll()
    p.$('toast').hidden = true
    await p.poll()
    await p.poll()
    assert.match(heldNote(p), /newer review is waiting/, 'still said, with the toast long gone')

    box.blur()
    await p.poll()
    await p.poll()
    assert.equal(heldNote(p), '', 'gone once the new review is shown')
  } finally { p.close() }
})

test('Switch now during a load that is on its way takes the new review as that load ends, not a poll later', async () => {
  const world = { review: reviewOf('2026-11-16T09:00:00Z', 'master', [thread(1, 'a.go')]), diff: [lineOf('a.go')] }
  const p = await pageWith(world)
  try {
    const box = replyBox(p)
    await p.type(box, 'mid-sentence')
    box.focus()
    secondReview(world)
    await p.poll()
    assert.match(toastText(p), /Switch now/)

    let release
    world.holdReview = new Promise((resolve) => { release = resolve })
    await p.kick('tick')
    const loads = diffLoads(p)
    p.$('toast').querySelector('.toast-action').click()
    release()
    world.holdReview = null
    await p.settle()

    assert.ok(diffLoads(p) > loads, 'the new review was taken without waiting for another poll')
    assert.equal(p.$('review-files').querySelectorAll('.review-thread').length, 1)
  } finally { p.close() }
})

test('closing the review names the one shown, and is answered like the rest when the agent has replaced it', async () => {
  const done = Object.assign(reviewOf('2026-11-16T09:00:00Z', 'master', [Object.assign(thread(1, 'a.go'), { status: 'resolved' })]), { state: 'done' })
  const world = { review: done, diff: [lineOf('a.go')] }
  const p = await pageWith(world)
  try {
    secondReview(world)
    world.refuse = { error: 'the agent has replaced this review with another: what you did was not applied to it', code: 'review-replaced' }
    const loads = diffLoads(p)
    await p.click('review-close')
    assert.deepEqual(p.sent('/api/review/close').pop().for, { started: '2026-11-16T09:00:00Z', ref: 'master' })
    assert.ok(diffLoads(p) > loads, 'the new review is taken, and not asked about again as if the agent were at work')
    assert.match(toastText(p), /agent has replaced this review/)
    assert.equal(p.$('confirm').open, false, 'no "are you sure" for a close that was not done')
  } finally { p.close() }
})
