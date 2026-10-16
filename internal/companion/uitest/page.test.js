// Tests of the companion page itself, in jsdom (see page.js), run by
// `node --test` (see review_ui_test.go). Needs `npm ci` in this folder.
'use strict'
const test = require('node:test')
const assert = require('node:assert/strict')
const { openPage } = require('./page.js')

// reviewPage is a page for a sandbox with a Review tab: vibe says the agent
// last updated the review at at.reviewAt, which a test can move on; the
// review is review. conversation says the agent can be talked to.
function reviewPage({ reviewAt = '', review = { threads: [], lastId: 0 }, storage = {}, diff = [], conversation = true } = {}) {
  const at = { reviewAt }
  const routes = {
    '/api/state': () => ({ sandbox: 'box', sandboxTabs: true, review: true, conversation, reviewAt: at.reviewAt, attention: {} }),
    '/api/review': () => ({ review: at.review || review, diff }),
    '/api/log': { entries: [] },
  }
  return openPage({ routes, storage: Object.assign({ 'vibe-companion-tab': 'log' }, storage) }).then((p) => Object.assign(p, { at }))
}

const dot = (p) => {
  const tab = p.$('tab-review')
  return tab.querySelector('.tab-dot') ? tab.title : null
}

test("the Review tab is marked when the agent has updated the review, and says so", async () => {
  const p = await reviewPage({ reviewAt: 't1' })
  try {
    assert.equal(dot(p), 'The agent has updated the review', 'never seen in this browser')
  } finally { p.close() }
})

test('nothing from the agent yet: no mark', async () => {
  const p = await reviewPage({ reviewAt: '' })
  try {
    assert.equal(dot(p), null)
  } finally { p.close() }
})

test('opening the Review tab clears the mark, and it stays cleared', async () => {
  const p = await reviewPage({ reviewAt: 't1' })
  try {
    await p.click('tab-review')
    assert.equal(dot(p), null)
    await p.click('tab-log')
    await p.poll()
    assert.equal(dot(p), null, 'seen, so not news on the next poll')
  } finally { p.close() }
})

test('an update while on another tab marks the Review tab', async () => {
  const p = await reviewPage({ reviewAt: 't1', storage: { 'vibe-companion-review-seen': 't1' } })
  try {
    assert.equal(dot(p), null)
    p.at.reviewAt = 't2'
    await p.poll()
    assert.equal(dot(p), 'The agent has updated the review')
  } finally { p.close() }
})

test('an update while the Review tab is open is marked until the user clicks or types in it', async () => {
  const p = await reviewPage({ reviewAt: 't1' })
  try {
    await p.click('tab-review')
    p.at.reviewAt = 't2'
    await p.poll()
    assert.ok(dot(p), 'marked while in view')
    p.$('review-files').dispatchEvent(new p.window.Event('pointerdown', { bubbles: true }))
    assert.equal(dot(p), null, 'a click in the pane')

    p.at.reviewAt = 't3'
    await p.poll()
    assert.ok(dot(p))
    p.$('review-filter').dispatchEvent(new p.window.KeyboardEvent('keydown', { key: 'a', bubbles: true }))
    assert.equal(dot(p), null, 'a key in the pane')
  } finally { p.close() }
})

test('a click outside the Review pane leaves the mark', async () => {
  const p = await reviewPage({ reviewAt: 't1' })
  try {
    p.$('tab-log').dispatchEvent(new p.window.Event('pointerdown', { bubbles: true }))
    assert.ok(dot(p))
  } finally { p.close() }
})

// --- commenting -----------------------------------------------------------

const diff = [{ path: 'a.txt', status: 'modified', hunks: [{ oldStart: 1, newStart: 1, header: '@@ -1 +1,2 @@', lines: [
  { kind: 'context', old: 1, new: 1, text: 'one' },
  { kind: 'add', new: 2, text: 'two' },
] }] }]

test('a comment on a line is written, then posted with where it is', async () => {
  const p = await reviewPage({ diff })
  try {
    await p.click('tab-review')
    await p.click(p.$('review-files').querySelector('[aria-label="Comment on line R2"]'))
    const box = p.window.document.querySelector('.review-compose-text')
    assert.ok(box, 'the composer opened')
    assert.equal(p.button('Comment', p.$('review-files')).disabled, true, 'nothing written yet')
    await p.type(box, 'why two?')
    await p.click(p.button('Comment', p.$('review-files')))
    assert.deepEqual(p.posted('/api/review/do'), [{ op: 'comment', file: 'a.txt', side: 'new', line: 2, code: 'two', body: 'why two?' }])
  } finally { p.close() }
})

test('cancelling a comment with something written asks first', async () => {
  const p = await reviewPage({ diff })
  try {
    await p.click('tab-review')
    await p.click(p.$('review-files').querySelector('[aria-label="Comment on line R2"]'))
    await p.type(p.window.document.querySelector('.review-compose-text'), 'half a thought')
    await p.click(p.button('Cancel', p.$('review-files')))
    assert.ok(p.$('confirm').open, 'asked')
    await p.confirm(false)
    assert.ok(p.window.document.querySelector('.review-compose-text'), 'kept, when not discarded')
    await p.click(p.button('Cancel', p.$('review-files')))
    await p.confirm(true)
    assert.equal(p.window.document.querySelector('.review-compose-text'), null, 'discarded')
    assert.deepEqual(p.posted('/api/review/do'), [])
  } finally { p.close() }
})

// The user's unsent comments are known by when they were said, so an edit
// or delete lands on the one meant even if another tab moved it.
const unsent = { lastId: 1, threads: [{ id: 1, status: 'open', comments: [
  { author: 'agent', body: 'P3: something', at: '2026-10-16T10:00:00Z' },
  { author: 'user', body: 'my reply', at: '2026-10-16T10:05:00.123456789Z' },
] }] }

test('editing an unsent comment sends when it was said', async () => {
  const p = await reviewPage({ review: unsent })
  try {
    await p.click('tab-review')
    await p.click(p.button('Edit', p.$('review-general')))
    await p.type(p.$('review-general').querySelector('textarea[aria-label="Edit your comment"]'), 'my better reply')
    await p.click(p.button('Save', p.$('review-general')))
    assert.deepEqual(p.posted('/api/review/do'), [{ op: 'edit', thread: 1, at: '2026-10-16T10:05:00.123456789Z', body: 'my better reply' }])
  } finally { p.close() }
})

test('deleting an unsent comment asks first, then sends when it was said', async () => {
  const p = await reviewPage({ review: unsent })
  try {
    await p.click('tab-review')
    await p.click(p.button('Delete', p.$('review-general')))
    assert.deepEqual(p.posted('/api/review/do'), [], 'not before asking')
    await p.confirm(true)
    assert.deepEqual(p.posted('/api/review/do'), [{ op: 'delete', thread: 1, at: '2026-10-16T10:05:00.123456789Z' }])
  } finally { p.close() }
})

test('Submit to agent sends the unsent comments', async () => {
  const p = await reviewPage({ review: unsent })
  try {
    await p.click('tab-review')
    assert.equal(p.$('review-submit').disabled, false)
    await p.click('review-submit')
    assert.deepEqual(p.posted('/api/review/submit'), [{}])
  } finally { p.close() }
})

test("Submit to agent is off when the page can't talk to the agent", async () => {
  const p = await reviewPage({ review: unsent, conversation: false })
  try {
    await p.click('tab-review')
    assert.equal(p.$('review-submit').disabled, true)
  } finally { p.close() }
})

// --- finding the comments -------------------------------------------------

const twoFiles = [
  { path: 'a.txt', status: 'modified', hunks: [{ oldStart: 1, newStart: 1, header: '@@ -1 +1,2 @@', lines: [
    { kind: 'context', old: 1, new: 1, text: 'one' },
    { kind: 'add', new: 2, text: 'two' },
  ] }] },
  { path: 'b.txt', status: 'added', hunks: [{ oldStart: 0, newStart: 1, header: '@@ -0,0 +1 @@', lines: [
    { kind: 'add', new: 1, text: 'b' },
  ] }] },
]
const said = (body) => [{ author: 'agent', body, at: '2026-10-16T10:00:00Z' }]
const commented = { lastId: 4, threads: [
  { id: 1, status: 'open', comments: said('on the whole review') },
  { id: 2, file: 'a.txt', side: 'new', line: 2, status: 'open', comments: said('on two') },
  { id: 3, file: 'a.txt', side: 'new', line: 1, status: 'open', comments: said('on one') },
  { id: 4, file: 'b.txt', side: 'new', line: 1, status: 'resolved', comments: said('done with') },
] }

const treeRow = (p, name) => Array.from(p.$('review-tree').querySelectorAll('[role=treeitem]')).find((r) => r.querySelector('.name').textContent === name)

test('the file tree shows how many comment threads each file has', async () => {
  const p = await reviewPage({ review: commented, diff: twoFiles })
  try {
    await p.click('tab-review')
    const a = treeRow(p, 'a.txt').querySelector('.review-tree-comments')
    assert.ok(a, 'a.txt is marked')
    assert.equal(a.textContent, '2')
    assert.equal(a.title, '2 comments')
    assert.equal(treeRow(p, 'b.txt').querySelector('.review-tree-comments').textContent, '1', 'resolved, but counted')
  } finally { p.close() }
})

// laidOut gives the page the layout jsdom hasn't: the review bar's foot at
// 50, and each thread's top as tops says (by thread id), for the page to
// measure where it is in the comments.
function laidOut(p, tops) {
  p.window.Element.prototype.getBoundingClientRect = function () {
    const id = this.id && this.id.match(/^review-thread-(\d+)$/)
    if (id) return { top: tops[id[1]], bottom: tops[id[1]] + 80, height: 80 }
    if (this.parentElement && this.parentElement.id === 'panel-review' && this.classList.contains('bar')) return { top: 0, bottom: 50, height: 50 }
    return { top: 0, bottom: 0, height: 0 }
  }
}

test('the bar counts the comment threads, and says how many the filter hides', async () => {
  const p = await reviewPage({ review: commented, diff: twoFiles })
  try {
    laidOut(p, { 1: 200, 3: 400, 2: 600, 4: 800 })
    await p.click('tab-review')
    assert.equal(p.$('review-count').textContent, '1/4 comments')
    await p.type(p.$('review-filter'), 'b.txt')
    assert.equal(p.$('review-count').textContent, '1/2 comments (2 hidden by the filter)')
  } finally { p.close() }
})

test('the bar says which comment the page is at, by the arrows or by scrolling', async () => {
  const p = await reviewPage({ review: commented, diff: twoFiles })
  try {
    p.window.Element.prototype.scrollIntoView = function () {}
    const tops = { 1: 200, 3: 400, 2: 600, 4: 800 }
    laidOut(p, tops)
    await p.click('tab-review')
    assert.equal(p.$('review-count').textContent, '1/4 comments', 'none reached yet')
    Object.assign(tops, { 1: -300, 3: -100, 2: 100, 4: 300 })
    p.$('panel-review').dispatchEvent(new p.window.Event('scroll'))
    assert.equal(p.$('review-count').textContent, '2/4 comments', 'scrolled past two')
    const counts = []
    for (const id of ['review-next-comment', 'review-next-comment', 'review-next-comment', 'review-prev-comment']) {
      await p.click(id)
      counts.push(p.$('review-count').textContent)
    }
    assert.deepEqual(counts, ['3/4 comments', '4/4 comments', '1/4 comments', '4/4 comments'], 'on, on, round to the first, back to the last')
  } finally { p.close() }
})

// At the foot of the page the last threads can't be scrolled up to the bar:
// stepping on goes from the one stepped to, not from where the page is.
test("the arrows step on past threads that can't reach the bar, and round", async () => {
  const p = await reviewPage({ review: commented, diff: twoFiles })
  try {
    const shown = []
    p.window.Element.prototype.scrollIntoView = function () { shown.push(this.id) }
    laidOut(p, { 1: -500, 3: -300, 2: 200, 4: 400 })
    await p.click('tab-review')
    for (let i = 0; i < 3; i++) await p.click('review-next-comment')
    assert.deepEqual(shown, ['review-thread-2', 'review-thread-4', 'review-thread-1'])
  } finally { p.close() }
})

test('scrolling by hand takes over from the arrows', async () => {
  const p = await reviewPage({ review: commented, diff: twoFiles })
  try {
    p.window.Element.prototype.scrollIntoView = function () {}
    const tops = { 1: -500, 3: -300, 2: 200, 4: 400 }
    laidOut(p, tops)
    await p.click('tab-review')
    await p.click('review-next-comment')
    assert.equal(p.$('review-count').textContent, '3/4 comments')
    p.$('panel-review').dispatchEvent(new p.window.Event('wheel'))
    p.$('panel-review').dispatchEvent(new p.window.Event('scroll'))
    assert.equal(p.$('review-count').textContent, '2/4 comments', 'measured again: thread 2 is still below the bar')
  } finally { p.close() }
})

test('the comment arrows go through all the threads in page order, round at the ends', async () => {
  const p = await reviewPage({ review: commented, diff: twoFiles })
  try {
    const shown = []
    p.window.Element.prototype.scrollIntoView = function () { shown.push(this.id) }
    await p.click('tab-review')
    // jsdom lays nothing out: every thread is "at" the top, so the first
    // step each way goes round to the end.
    await p.click('review-next-comment')
    await p.click('review-prev-comment')
    assert.deepEqual(shown, ['review-thread-1', 'review-thread-4'], 'the first (the whole review), then the last (resolved, on b.txt)')
    assert.equal(p.window.document.activeElement.dataset.thread, '4', 'its reply box has focus, to type in at once')
  } finally { p.close() }
})

test('the comment arrows skip threads the filter hides, and are off with none to go to', async () => {
  const p = await reviewPage({ review: { lastId: 1, threads: [commented.threads[1]] }, diff: twoFiles })
  try {
    await p.click('tab-review')
    assert.equal(p.$('review-next-comment').disabled, false)
    await p.type(p.$('review-filter'), 'b.txt')
    assert.equal(p.$('review-next-comment').disabled, true)
    assert.equal(p.$('review-prev-comment').disabled, true)
  } finally { p.close() }
})

test('Submit to agent goes on to the Conversation tab, where the answer comes', async () => {
  const p = await reviewPage({ review: unsent })
  try {
    await p.click('tab-review')
    await p.click('review-submit')
    assert.equal(p.$('tab-conversation').getAttribute('aria-selected'), 'true')
  } finally { p.close() }
})

// --- Ask, answered on the thread ----------------------------------------------

const asked = { lastId: 1, threads: [{ id: 1, status: 'open', comments: [
  { author: 'agent', body: 'P3: something', at: '2026-10-16T10:00:00Z' },
  { author: 'user', body: 'why?', at: '2026-10-16T10:05:00Z', sent: true },
] }] }

async function askPage(ask, review = asked) {
  const p = await openPage({
    routes: {
      '/api/state': { sandbox: 'box', sandboxTabs: true, review: true, conversation: true, reviewAt: '', attention: {} },
      '/api/review': { review, diff: [], ask },
      '/api/log': { entries: [] },
    },
    storage: { 'vibe-companion-tab': 'review' },
  })
  return { p, thread: p.$('review-thread-1') }
}

test('while the agent answers an Ask, the thread shows it at work', async () => {
  const { p, thread } = await askPage({ thread: 1, answering: true })
  try {
    assert.ok(thread.querySelector('.typing'), 'the three dots')
  } finally { p.close() }
})

test('an Ask whose turn ended unanswered says so on the thread', async () => {
  const { p, thread } = await askPage({ thread: 1, answering: false })
  try {
    assert.equal(thread.querySelector('.typing'), null)
    assert.match(thread.querySelector('.review-unanswered').textContent, /without answering/)
  } finally { p.close() }
})

test('an answered Ask shows neither', async () => {
  const answered = { lastId: 1, threads: [{ ...asked.threads[0], comments: [...asked.threads[0].comments, { author: 'agent', body: 'because', at: '2026-10-16T10:06:00Z' }] }] }
  for (const answering of [true, false]) {
    const { p, thread } = await askPage({ thread: 1, answering }, answered)
    try {
      assert.equal(thread.querySelector('.typing'), null, 'answering=' + answering)
      assert.equal(thread.querySelector('.review-unanswered'), null, 'answering=' + answering)
    } finally { p.close() }
  }
})

// --- threads on lines the diff leaves out ----------------------------------------

const gapped = [{ path: 'c.txt', status: 'modified', hunks: [{ oldStart: 10, newStart: 10, header: '@@ -10 +10,2 @@', lines: [
  { kind: 'context', old: 10, new: 10, text: 'line 10' },
  { kind: 'add', new: 11, text: 'added' },
] }] }]
const fileText = Array.from({ length: 40 }, (_, i) => i === 10 ? 'added' : 'line ' + (i + 1))
const offDiff = { lastId: 3, threads: [
  { id: 1, file: 'c.txt', side: 'new', line: 30, code: 'line 30', status: 'open', comments: said('on 30') },
  { id: 2, file: 'c.txt', side: 'old', line: 5, code: 'what it was', status: 'open', comments: said('on old 5') },
  { id: 3, file: 'c.txt', side: 'new', line: 99, status: 'open', comments: said('past the end') },
] }

async function gappedPage() {
  const p = await openPage({
    routes: {
      '/api/state': { sandbox: 'box', sandboxTabs: true, review: true, conversation: true, reviewAt: '', attention: {} },
      '/api/review': { review: offDiff, diff: gapped },
      '/api/review/file': { lines: fileText },
      '/api/log': { entries: [] },
    },
    storage: { 'vibe-companion-tab': 'review' },
  })
  return p
}

// rowAfter is the row after the context row showing new line n.
const rowAfter = (p, n) => {
  const rows = Array.from(p.$('review-files').querySelectorAll('tr'))
  const i = rows.findIndex((r) => { const td = r.querySelector('td.num.context.start'); return td && td.textContent === String(n) })
  assert.ok(i >= 0, 'line ' + n + ' is shown')
  return rows[i + 1]
}

test('a thread on a hidden line is shown under it, on its side, the lines around it opened', async () => {
  const p = await gappedPage()
  try {
    const right = rowAfter(p, 30)
    assert.ok(right.classList.contains('threads'))
    assert.ok(right.querySelector('td.side.start #review-thread-1'), 'a new line: the right pane')
    const left = rowAfter(p, 5)
    assert.ok(left.querySelector('td.side:not(.start) #review-thread-2'), 'an old line: the left pane')
    assert.ok(rowAfter(p, 33), 'context after it opened too')
  } finally { p.close() }
})

test("a thread on a hidden line whose text has changed is marked so", async () => {
  const p = await gappedPage()
  try {
    assert.ok(p.$('review-thread-2').querySelector('.review-outdated'), 'old line 5 reads differently now')
    assert.equal(p.$('review-thread-1').querySelector('.review-outdated'), null)
  } finally { p.close() }
})

test('a thread on no line of the file stays above it, and the file is read once', async () => {
  const p = await gappedPage()
  try {
    const block = p.$('review-thread-3').closest('.note')
    assert.ok(block, 'past the end: above the file')
    assert.equal(block.querySelector('#review-thread-1'), null)
    await p.poll()
    assert.equal(p.calls.filter((c) => c.path === '/api/review/file').length, 1)
  } finally { p.close() }
})

// --- replying from the keyboard ---------------------------------------------------

test('Ctrl+Enter in a reply box replies (never asks)', async () => {
  const p = await reviewPage({ review: commented, diff: twoFiles })
  try {
    await p.click('tab-review')
    const box = p.$('review-thread-1').querySelector('textarea[data-thread]')
    box.focus()
    await p.type(box, 'noted')
    box.dispatchEvent(new p.window.KeyboardEvent('keydown', { key: 'Enter', ctrlKey: true, bubbles: true }))
    await p.settle()
    assert.deepEqual(p.posted('/api/review/do'), [{ op: 'reply', thread: 1, body: 'noted' }])
    assert.deepEqual(p.posted('/api/review/ask'), [])
  } finally { p.close() }
})

test("an empty reply box with focus doesn't hold back what the agent says, and keeps focus", async () => {
  const p = await reviewPage({ review: commented, diff: twoFiles })
  try {
    await p.click('tab-review')
    p.$('review-thread-2').querySelector('textarea[data-thread]').focus()
    const later = JSON.parse(JSON.stringify(commented))
    later.threads[1].comments.push({ author: 'agent', body: 'more on two', at: '2026-10-16T11:00:00Z' })
    p.at.review = later
    await p.poll()
    assert.match(p.$('review-thread-2').textContent, /more on two/)
    assert.equal(p.window.document.activeElement.dataset.thread, '2')
  } finally { p.close() }
})

test('a reply box with something typed holds back a redraw', async () => {
  const p = await reviewPage({ review: commented, diff: twoFiles })
  try {
    await p.click('tab-review')
    const box = p.$('review-thread-2').querySelector('textarea[data-thread]')
    box.focus()
    await p.type(box, 'half a')
    const later = JSON.parse(JSON.stringify(commented))
    later.threads[1].comments.push({ author: 'agent', body: 'more on two', at: '2026-10-16T11:00:00Z' })
    p.at.review = later
    await p.poll()
    assert.equal(p.window.document.activeElement, box, 'the same box, untouched')
  } finally { p.close() }
})

// --- starting and closing reviews ----------------------------------------------

const none = { threads: [], lastId: 0, started: '0001-01-01T00:00:00Z' }
const allOptions = { uncommitted: true, defaultBranch: 'master', upstream: 'origin/feat', unpushed: 2 }

// startPage is a page whose fake vibe keeps the review as the page starts,
// closes and reopens it.
async function startPage({ review = none, options = allOptions, conversation = true } = {}) {
  let current = review
  return openPage({
    routes: {
      '/api/state': { sandbox: 'box', sandboxTabs: true, review: true, conversation, reviewAt: '', attention: {} },
      '/api/review': () => ({ review: current, diff: [] }),
      '/api/review/options': options,
      '/api/review/start': (call) => ({ review: (current = { ...none, started: '2026-10-16T12:00:00Z', state: call.body.agent ? 'in progress' : '' }) }),
      '/api/review/do': (call) => ({ review: (current = { ...current, closed: call.body.op === 'close' ? call.body.body : '' }) }),
      '/api/log': { entries: [] },
    },
    storage: { 'vibe-companion-tab': 'review' },
  })
}

const startButtons = (p) => Array.from(p.$('review-start-options').querySelectorAll('button')).map((b) => b.textContent)

test('with no review, the tab offers to start one, and nothing else', async () => {
  const p = await startPage()
  try {
    assert.equal(p.$('review-start').hidden, false)
    assert.equal(p.$('review-body').hidden, true)
    assert.deepEqual(startButtons(p), [
      'Review uncommitted changes', 'Review against master', 'Review 2 unpushed commits',
      'Show uncommitted changes', 'Diff against master', 'Show 2 unpushed commits',
    ])
  } finally { p.close() }
})

test('starting a review asks the agent', async () => {
  const p = await startPage()
  try {
    await p.click(p.button('Review against master'))
    assert.deepEqual(p.posted('/api/review/start'), [{ kind: 'branch', agent: true }])
  } finally { p.close() }
})

test('showing the diff doesn’t ask the agent', async () => {
  const p = await startPage()
  try {
    await p.click(p.button('Show 2 unpushed commits'))
    assert.deepEqual(p.posted('/api/review/start'), [{ kind: 'unpushed', agent: false }])
  } finally { p.close() }
})

test('a started review shows its diff at once', async () => {
  const p = await startPage()
  try {
    await p.click(p.button('Show uncommitted changes'))
    assert.deepEqual(p.posted('/api/review/start'), [{ kind: 'uncommitted', agent: false }])
    assert.equal(p.$('review-start').hidden, true)
    assert.ok(p.calls.some((c) => c.path === '/api/review' && c.query === '?diff=1' && p.calls.indexOf(c) > p.calls.findIndex((x) => x.path === '/api/review/start')), 'the diff fetched afresh')
  } finally { p.close() }
})

test('with no way to talk to the agent, only the diff is offered', async () => {
  const p = await startPage({ conversation: false })
  try {
    assert.deepEqual(startButtons(p), ['Show uncommitted changes', 'Diff against master', 'Show 2 unpushed commits'])
  } finally { p.close() }
})

test('with nothing to review, it says so', async () => {
  const p = await startPage({ options: { uncommitted: false } })
  try {
    assert.deepEqual(startButtons(p), [])
    assert.match(p.$('review-start').textContent, /Nothing to review/)
  } finally { p.close() }
})

test('a closed review says why, and can be reopened', async () => {
  const closed = { ...none, started: '2026-10-16T10:00:00Z', closed: 'Closed by itself: its changes have all been committed (or put away).',
    threads: [{ id: 1, status: 'open', comments: said('x') }], lastId: 1 }
  const p = await startPage({ review: closed })
  try {
    assert.equal(p.$('review-start').hidden, false)
    assert.match(p.$('review-closed').textContent, /committed/)
    await p.click('review-reopen')
    assert.deepEqual(p.posted('/api/review/do'), [{ op: 'reopen-review' }])
  } finally { p.close() }
})

const openReview = { ...none, started: '2026-10-16T10:00:00Z', lastId: 2, threads: [
  { id: 1, status: 'open', comments: said('x') },
  { id: 2, status: 'resolved', comments: said('y') },
] }

test('closing a review with comments still open asks first', async () => {
  const p = await startPage({ review: openReview })
  try {
    assert.equal(p.$('review-start').hidden, true)
    await p.click('review-close')
    assert.ok(p.$('confirm').open)
    assert.match(p.$('confirm-text').textContent, /1 comment is still open/)
    await p.confirm(true)
    assert.deepEqual(p.posted('/api/review/do'), [{ op: 'close', body: 'You closed it.' }])
  } finally { p.close() }
})

test('closing a review with nothing open just closes it', async () => {
  const p = await startPage({ review: { ...openReview, threads: [openReview.threads[1]] } })
  try {
    await p.click('review-close')
    assert.equal(p.$('confirm').open, false)
    assert.deepEqual(p.posted('/api/review/do'), [{ op: 'close', body: 'You closed it.' }])
    assert.equal(p.$('review-start').hidden, false, 'back to starting one')
    assert.match(p.$('review-closed').textContent, /You closed it/)
  } finally { p.close() }
})

// --- the file tree's icons -------------------------------------------------------------

test("each file in the tree has an icon for what happened to it", async () => {
  const files = ['added', 'modified', 'deleted', 'renamed'].map((status) => ({ path: status + '.txt', oldPath: 'was.txt', status, hunks: [] }))
  const p = await reviewPage({ review: { ...none, started: '2026-10-16T10:00:00Z' }, diff: files })
  try {
    await p.click('tab-review')
    for (const status of ['added', 'modified', 'deleted', 'renamed']) {
      const icon = treeRow(p, status + '.txt').querySelector('svg.file-icon')
      assert.ok(icon, status + ' has an icon')
      assert.ok(icon.classList.contains(status), status + ': coloured as such')
      assert.equal(icon.getAttribute('data-sign'), { added: '+', modified: '±', deleted: '−', renamed: '→' }[status])
    }
  } finally { p.close() }
})

// --- the Conversation tab's dot -------------------------------------------------------------

// convoPage is a page on the Review tab whose conversation is convo, which
// a test can add to: { events, running }.
async function convoPage(convo) {
  const p = await openPage({
    routes: {
      '/api/state': { sandbox: 'box', sandboxTabs: true, review: true, conversation: true, reviewAt: '', attention: {} },
      '/api/review': { review: none, diff: [] },
      '/api/conversation': (call) => {
        const after = Number(new URLSearchParams(call.query).get('after')) || 0
        const events = convo.events.filter((e) => e.seq > after)
        return { events, last: convo.events.length ? convo.events[convo.events.length - 1].seq : 0, running: convo.running }
      },
      '/api/log': { entries: [] },
    },
    storage: { 'vibe-companion-tab': 'review' },
  })
  await p.every('pollConversation')
  return p
}

const convoDot = (p) => {
  const dot = p.$('tab-conversation').querySelector('.tab-dot')
  return dot ? { title: p.$('tab-conversation').title, working: dot.classList.contains('working') } : null
}
const agentSaid = (seq, text) => ({ seq, kind: 'agent', agent: { type: 'assistant', message: { content: [{ type: 'text', text }] } } })

test("what the agent said before the page was opened isn't news", async () => {
  const p = await convoPage({ events: [{ seq: 1, kind: 'user', text: 'hi' }, agentSaid(2, 'hello')], running: false })
  try {
    assert.equal(convoDot(p), null)
  } finally { p.close() }
})

test('the Conversation tab is marked while the agent works, then when it has answered', async () => {
  const convo = { events: [{ seq: 1, kind: 'user', text: 'hi' }], running: false }
  const p = await convoPage(convo)
  try {
    convo.events.push({ seq: 2, kind: 'user', text: 'please review' })
    convo.running = true
    await p.every('pollConversation')
    assert.deepEqual(convoDot(p), { title: 'The agent is working', working: true })
    convo.events.push(agentSaid(3, 'done'))
    convo.running = false
    await p.every('pollConversation')
    assert.deepEqual(convoDot(p), { title: 'The agent has answered', working: false })
    await p.click('tab-conversation')
    assert.equal(convoDot(p), null, 'opening it clears it')
  } finally { p.close() }
})

test('the Conversation tab is never marked while it is open', async () => {
  const convo = { events: [{ seq: 1, kind: 'user', text: 'hi' }], running: false }
  const p = await convoPage(convo)
  try {
    await p.click('tab-conversation')
    convo.events.push(agentSaid(2, 'more'))
    convo.running = true
    await p.every('pollConversation')
    assert.equal(convoDot(p), null)
  } finally { p.close() }
})

// --- pasting on the Review tab --------------------------------------------------------

// pastePage is a page on a review whose fake vibe answers pastes as the
// clipboard page would, already having those named in seen; waiting is how
// many pastes it says are waiting.
async function pastePage({ seen = [], waiting = 0 } = {}) {
  return openPage({
    routes: {
      '/api/state': { sandbox: 'box', sandboxTabs: true, review: true, conversation: true, reviewAt: '', attention: {} },
      '/api/review': { review: { ...none, started: '2026-10-16T10:00:00Z' }, diff: [], pastes: waiting },
      '/api/clipboard/paste': (call) => (seen.includes(call.body.name) ? { duplicate: true } : { id: 1 }),
      '/api/log': { entries: [] },
    },
    storage: { 'vibe-companion-tab': 'review' },
  })
}

// paste pastes into the Review tab what a clipboard holding text and files
// would give.
function paste(p, { text = '', files = [] }) {
  const ev = new p.window.Event('paste', { bubbles: true, cancelable: true })
  ev.clipboardData = { getData: (type) => (type === 'text/plain' ? text : ''), files }
  p.$('review-files').dispatchEvent(ev)
  return ev
}

test('an image pasted on the Review tab goes to the clipboard page, and says so', async () => {
  const p = await pastePage()
  try {
    const image = new p.window.File(['png'], 'shot.png', { type: 'image/png' })
    const ev = paste(p, { files: [image] })
    await p.settle()
    assert.ok(ev.defaultPrevented, 'not pasted into a box as well')
    const sent = p.calls.filter((c) => c.path === '/api/clipboard/paste')
    assert.equal(sent.length, 1)
    assert.equal(sent[0].body, image)
    assert.equal(sent[0].headers['Content-Type'], 'image/png')
    assert.equal(sent[0].headers['X-Filename'], 'shot.png')
    assert.equal(p.$('toast').textContent, 'Image pasted')
  } finally { p.close() }
})

test('a file pasted again before the agent has it says it already was', async () => {
  const p = await pastePage({ seen: ['notes.pdf'] })
  try {
    paste(p, { files: [new p.window.File(['pdf'], 'notes.pdf', { type: 'application/pdf' })] })
    await p.settle()
    assert.equal(p.$('toast').textContent, 'Already pasted')
  } finally { p.close() }
})

test('a pasted file that isn’t already waiting says so', async () => {
  const p = await pastePage()
  try {
    paste(p, { files: [new p.window.File(['pdf'], 'notes.pdf', { type: 'application/pdf' })] })
    await p.settle()
    assert.equal(p.$('toast').textContent, 'File pasted')
  } finally { p.close() }
})

test('text pastes as text, as ever, even with an image alongside it', async () => {
  const p = await pastePage()
  try {
    const ev = paste(p, { text: 'some words', files: [new p.window.File(['png'], 'a.png', { type: 'image/png' })] })
    await p.settle()
    assert.equal(ev.defaultPrevented, false)
    assert.deepEqual(p.calls.filter((c) => c.path === '/api/clipboard/paste'), [])
  } finally { p.close() }
})

test('the bar says how many pastes will go with the next message', async () => {
  const p = await pastePage({ waiting: 2 })
  try {
    assert.equal(p.$('review-pastes').hidden, false)
    assert.equal(p.$('review-pastes').textContent, '2 pastes to send')
  } finally { p.close() }
  const none = await pastePage({ waiting: 0 })
  try {
    assert.equal(none.$('review-pastes').hidden, true)
  } finally { none.close() }
})

// --- the file the page is in ------------------------------------------------------------------

const nested = ['src/a.txt', 'src/b.txt', 'top.txt'].map((path) => ({ path, status: 'modified', hunks: [{ oldStart: 1, newStart: 1, header: '@@ -1 +1 @@', lines: [
  { kind: 'del', old: 1, text: 'x' }, { kind: 'add', new: 1, text: 'y' },
] }] }))

// layout gives the page the layout jsdom hasn't: the review bar's foot at
// 50, and whatever rectOf says of other elements (else nothing).
function layout(p, rectOf) {
  p.window.Element.prototype.getBoundingClientRect = function () {
    const r = rectOf(this)
    if (r) return r
    if (this.parentElement && this.parentElement.id === 'panel-review' && this.classList.contains('bar')) return { top: 0, bottom: 50, height: 50 }
    return { top: 0, bottom: 0, height: 0 }
  }
}

// filesAt lays the files' sections out with their tops at tops (by index).
const filesAt = (tops) => (node) => {
  const m = node.id && node.id.match(/^review-file-(\d+)$/)
  return m ? { top: tops[m[1]], bottom: tops[m[1]] + 500, height: 500 } : null
}

const currentRow = (p) => {
  const rows = Array.from(p.$('review-tree').querySelectorAll('.current'))
  return rows.map((r) => r.querySelector('.name').textContent)
}

async function nestedPage(tops) {
  const p = await reviewPage({ review: { ...none, started: '2026-10-16T10:00:00Z' }, diff: nested })
  p.window.Element.prototype.scrollIntoView = function () {}
  layout(p, filesAt(tops))
  await p.click('tab-review')
  return p
}

const scrolled = (p) => p.$('panel-review').dispatchEvent(new p.window.Event('scroll'))

test('the tree marks the file the page is in, and counts how far through it is', async () => {
  const tops = { 0: 200, 1: 900, 2: 1600 }
  const p = await nestedPage(tops)
  try {
    assert.deepEqual(currentRow(p), [], 'above the first file')
    assert.equal(p.$('review-tree-count').textContent, '3 files')
    Object.assign(tops, { 0: -600, 1: 40, 2: 700 })
    scrolled(p)
    assert.deepEqual(currentRow(p), ['b.txt'])
    assert.equal(p.$('review-tree-count').textContent, '2/3 files')
  } finally { p.close() }
})

test("a file in a collapsed folder marks the folder, until it's opened", async () => {
  const p = await nestedPage({ 0: -600, 1: 40, 2: 700 })
  try {
    scrolled(p)
    await p.click(treeRow(p, 'src/'))
    assert.deepEqual(currentRow(p), ['src/'], 'collapsed')
    await p.click(treeRow(p, 'src/'))
    assert.deepEqual(currentRow(p), ['b.txt'], 'opened again')
  } finally { p.close() }
})

test('a file clicked in the tree is marked at once, until the page is scrolled by hand', async () => {
  const p = await nestedPage({ 0: 200, 1: 900, 2: 1600 })
  try {
    await p.click(treeRow(p, 'top.txt'))
    assert.deepEqual(currentRow(p), ['top.txt'])
    assert.equal(p.$('review-tree-count').textContent, '3/3 files')
    scrolled(p)
    assert.deepEqual(currentRow(p), ['top.txt'], 'the scroll it set going')
    p.$('panel-review').dispatchEvent(new p.window.Event('wheel'))
    scrolled(p)
    assert.deepEqual(currentRow(p), [], 'measured again')
  } finally { p.close() }
})

test("the tree scrolls to keep the marked file's row in view", async () => {
  const p = await reviewPage({ review: { ...none, started: '2026-10-16T10:00:00Z' }, diff: nested })
  try {
    const pane = p.$('review-tree').closest('.review-tree-pane')
    const files = filesAt({ 0: -600, 1: -100, 2: 40 })
    layout(p, (node) => {
      if (node === pane) return { top: 60, bottom: 160, height: 100 }
      if (node.getAttribute && node.getAttribute('role') === 'treeitem' && node.dataset.path === 'top.txt') return { top: 300, bottom: 320, height: 20 }
      return files(node)
    })
    await p.click('tab-review')
    pane.scrollTop = 0
    scrolled(p)
    assert.equal(pane.scrollTop, 320 - 160 + 4, 'just far enough, and a little')
  } finally { p.close() }
})

// --- each file's figures ---------------------------------------------------------------------

test("each file's header says how much changed, and how long it is", async () => {
  const files = [
    { path: 'a.txt', status: 'modified', lines: 412, hunks: [{ oldStart: 1, newStart: 1, header: '', lines: [
      { kind: 'context', old: 1, new: 1, text: 'x' }, { kind: 'del', old: 2, text: 'y' }, { kind: 'add', new: 2, text: 'z' }, { kind: 'add', new: 3, text: 'w' },
    ] }] },
    { path: 'gone.txt', status: 'deleted', lines: 3, hunks: [{ oldStart: 1, newStart: 0, header: '', lines: [
      { kind: 'del', old: 1, text: 'a' }, { kind: 'del', old: 2, text: 'b' }, { kind: 'del', old: 3, text: 'c' },
    ] }] },
    { path: 'pic.png', status: 'added', binary: true, hunks: [] },
  ]
  const p = await reviewPage({ review: { ...none, started: '2026-10-16T10:00:00Z' }, diff: files })
  try {
    await p.click('tab-review')
    const stat = (i) => p.$('review-file-' + i).querySelector('header .diffstat')
    const a = stat(0)
    assert.equal(a.querySelector('.adds').textContent, '+2')
    assert.equal(a.querySelector('.dels').textContent, '−1')
    assert.deepEqual(Array.from(a.querySelectorAll('.square')).map((s) => s.className.replace('square ', '')), ['add', 'add', 'del', 'none', 'none'])
    assert.equal(a.querySelector('.length').textContent, '412 lines')
    assert.equal(a.title, '2 lines added, 1 deleted, of 412')
    assert.equal(stat(1).querySelector('.length').textContent, 'was 3 lines')
    assert.equal(stat(2), null, 'a binary file has no lines to count')
  } finally { p.close() }
})

// --- one order: the tree's -----------------------------------------------------------------------

test('files are shown as the tree lists them, the reading order numbered on them', async () => {
  const one = (path) => ({ path, status: 'modified', hunks: [{ oldStart: 1, newStart: 1, header: '', lines: [{ kind: 'add', new: 1, text: 'x' }] }] })
  const order = [{ file: 'z.go', label: 'start here' }, { file: 'ui/b.go', label: 'then' }]
  const p = await reviewPage({ review: { ...none, started: '2026-10-16T10:00:00Z', order }, diff: [one('z.go'), one('ui/b.go'), one('a.go')] })
  try {
    await p.click('tab-review')
    const shown = Array.from(p.$('review-files').querySelectorAll('.review-file')).map((c) => c.dataset.path)
    assert.deepEqual(shown, ['ui/b.go', 'a.go', 'z.go'], 'folders first, then by name: as the tree')
    const head = p.$('review-files').querySelector('[data-path="z.go"] header')
    assert.equal(head.querySelector('.review-step').textContent, '1')
    assert.equal(head.querySelector('.review-stop').textContent, 'start here')
    const row = treeRow(p, 'z.go')
    assert.equal(row.querySelector('.review-step').textContent, '1')
    assert.equal(row.querySelector('.review-step').title, 'Step 1 of the reading order: start here')
    assert.equal(treeRow(p, 'a.go').querySelector('.review-step'), null, 'not in the reading order')
  } finally { p.close() }
})

// --- the order the comment arrows go in -----------------------------------------------------------

const stepped = (() => {
  const one = (path) => ({ path, status: 'modified', hunks: [{ oldStart: 1, newStart: 1, header: '', lines: [{ kind: 'add', new: 1, text: 'x' }] }] })
  const on = (id, file) => ({ id, file, side: 'new', line: 1, status: 'open', comments: said('on ' + (file || 'the whole review')) })
  return {
    diff: [one('z.go'), one('ui/b.go'), one('a.go')],
    review: { ...none, started: '2026-10-16T10:00:00Z', lastId: 4,
      threads: [{ id: 1, status: 'open', comments: said('on the whole review') }, on(2, 'a.go'), on(3, 'ui/b.go'), on(4, 'z.go')],
      order: [{ file: 'z.go', label: 'start here' }, { file: 'ui/b.go', label: 'then' }] },
  }
})()

// arrowsVisit presses ▼ times times, and says which threads it went to.
async function arrowsVisit(p, times) {
  const shown = []
  p.window.Element.prototype.scrollIntoView = function () { shown.push(Number(this.id.replace('review-thread-', ''))) }
  // None reached yet: all below the bar.
  laidOut(p, { 1: 200, 2: 300, 3: 400, 4: 500 })
  for (let i = 0; i < times; i++) await p.click('review-next-comment')
  return shown
}

const orderItem = (p, text) => Array.from(p.$('review-order-menu').querySelectorAll('[role=menuitemradio]')).find((b) => b.textContent.startsWith(text))

test('by default the comment arrows go in path order, as the files are shown', async () => {
  const p = await reviewPage(stepped)
  try {
    await p.click('tab-review')
    assert.match(p.$('review-order').title, /path order/)
    assert.deepEqual(await arrowsVisit(p, 4), [1, 3, 2, 4], 'the whole review, then ui/b.go, a.go, z.go')
  } finally { p.close() }
})

test('the count opens a choice of order; the reading order sends the arrows by step, and is kept', async () => {
  const p = await reviewPage(stepped)
  try {
    await p.click('tab-review')
    assert.equal(p.$('review-order-menu').hidden, true)
    await p.click('review-order')
    assert.equal(p.$('review-order-menu').hidden, false)
    assert.equal(orderItem(p, 'By path').getAttribute('aria-checked'), 'true')
    await p.click(orderItem(p, "By the agent's reading order"))
    assert.equal(p.$('review-order-menu').hidden, true, 'closed once chosen')
    assert.match(p.$('review-order').title, /reading order/)
    assert.equal(p.window.localStorage.getItem('vibe-review-comment-order'), 'reading')
    assert.deepEqual(await arrowsVisit(p, 4), [1, 4, 3, 2], 'the whole review, then z.go (1), ui/b.go (2), a.go')
    assert.equal(p.$('review-count').textContent, '4/4 comments')
    const files = Array.from(p.$('review-files').querySelectorAll('.review-file')).map((c) => c.dataset.path)
    assert.deepEqual(files, ['ui/b.go', 'a.go', 'z.go'], 'the files stay as the tree has them')
  } finally { p.close() }
})

test('with no reading order, it can’t be chosen, and says why', async () => {
  const p = await reviewPage({ ...stepped, review: { ...stepped.review, order: [] } })
  try {
    await p.click('tab-review')
    await p.click('review-order')
    const item = orderItem(p, "By the agent's reading order")
    assert.equal(item.disabled, true)
    assert.match(item.textContent, /the agent set no reading order/)
  } finally { p.close() }
})

test('the order menu closes with Escape, or a click elsewhere', async () => {
  const p = await reviewPage(stepped)
  try {
    await p.click('tab-review')
    await p.click('review-order')
    p.$('review-order-menu').dispatchEvent(new p.window.KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    assert.equal(p.$('review-order-menu').hidden, true, 'Escape')
    await p.click('review-order')
    p.$('review-files').dispatchEvent(new p.window.Event('pointerdown', { bubbles: true }))
    assert.equal(p.$('review-order-menu').hidden, true, 'a click elsewhere')
  } finally { p.close() }
})

test('the chosen order is remembered', async () => {
  const p = await reviewPage({ ...stepped, storage: { 'vibe-review-comment-order': 'reading' } })
  try {
    await p.click('tab-review')
    assert.match(p.$('review-order').title, /reading order/)
  } finally { p.close() }
})

// --- the Conversation tab ---------------------------------------------------------------

test('opening the Conversation tab puts the cursor in the prompt box', async () => {
  const p = await convoPage({ events: [], running: false })
  try {
    p.$('review-filter').focus()
    await p.click('tab-conversation')
    assert.equal(p.window.document.activeElement, p.$('convo-input'))
  } finally { p.close() }
  const opened = await openPage({
    routes: { '/api/state': { sandbox: 'box', sandboxTabs: true, review: true, conversation: true, reviewAt: '', attention: {} }, '/api/conversation': { events: [], last: 0 } },
    storage: { 'vibe-companion-tab': 'conversation' },
  })
  try {
    assert.equal(opened.window.document.activeElement, opened.$('convo-input'), 'when the page opens on it too')
  } finally { opened.close() }
})
