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

test('the bar counts the comment threads, and says how many the filter hides', async () => {
  const p = await reviewPage({ review: commented, diff: twoFiles })
  try {
    await p.click('tab-review')
    assert.equal(p.$('review-count').textContent, '4 comments')
    await p.type(p.$('review-filter'), 'b.txt')
    assert.equal(p.$('review-count').textContent, '4 comments (2 hidden by the filter)')
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
