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
    '/api/review/do': () => ({ review: at.review || review }),
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

test('Ctrl+Enter in the edit box saves the edit', async () => {
  const p = await reviewPage({ review: unsent })
  try {
    await p.click('tab-review')
    await p.click(p.button('Edit', p.$('review-general')))
    const box = p.$('review-general').querySelector('textarea[aria-label="Edit your comment"]')
    await p.type(box, 'my better reply')
    box.dispatchEvent(new p.window.KeyboardEvent('keydown', { key: 'Enter', ctrlKey: true, bubbles: true }))
    await p.settle()
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
  // The number, not the + beside it.
  const i = rows.findIndex((r) => { const td = r.querySelector('td.num.context.start'); return td && td.firstChild.textContent === String(n) })
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

// replyByKeys replies on a thread with Ctrl+Enter, the server taking it.
async function replyByKeys(p, id, text) {
  const box = p.$('review-thread-' + id).querySelector('textarea[data-thread]')
  box.focus()
  await p.type(box, text)
  box.dispatchEvent(new p.window.KeyboardEvent('keydown', { key: 'Enter', ctrlKey: true, bubbles: true }))
  await p.settle()
}

const replyBoxFocused = (p) => {
  const box = p.window.document.activeElement
  return box && box.dataset && box.dataset.thread ? Number(box.dataset.thread) : null
}

test('Ctrl+Enter in a reply box goes on to the next comment, ready to reply', async () => {
  const p = await reviewPage({ review: commented, diff: twoFiles })
  try {
    await p.click('tab-review')
    assert.equal(p.$('review-reply-next').checked, true, 'on unless turned off')
    const shown = []
    p.window.Element.prototype.scrollIntoView = function () { shown.push(this.id) }
    await replyByKeys(p, 1, 'noted')
    assert.deepEqual(shown, ['review-thread-3'], 'the next comment down the page')
    assert.equal(replyBoxFocused(p), 3, 'its reply box ready')
    assert.match(p.$('review-count').textContent, /^2\/4/)
  } finally { p.close() }
})

test('with "next after reply" unticked, Ctrl+Enter replies and stays, and the choice is kept', async () => {
  const p = await reviewPage({ review: commented, diff: twoFiles })
  try {
    await p.click('tab-review')
    const tick = p.$('review-reply-next')
    tick.checked = false
    tick.dispatchEvent(new p.window.Event('change', { bubbles: true }))
    await replyByKeys(p, 1, 'noted')
    assert.deepEqual(p.posted('/api/review/do'), [{ op: 'reply', thread: 1, body: 'noted' }])
    assert.equal(replyBoxFocused(p), null)
  } finally { p.close() }
  const again = await reviewPage({ review: commented, diff: twoFiles, storage: { 'vibe-review-reply-next': 'off' } })
  try {
    await again.click('tab-review')
    assert.equal(again.$('review-reply-next').checked, false)
  } finally { again.close() }
})

test("the Reply button doesn't move on: only Ctrl+Enter does", async () => {
  const p = await reviewPage({ review: commented, diff: twoFiles })
  try {
    await p.click('tab-review')
    const card = p.$('review-thread-1')
    await p.type(card.querySelector('textarea[data-thread]'), 'noted')
    await p.click(Array.from(card.querySelectorAll('button')).find((b) => b.textContent === 'Reply'))
    assert.equal(replyBoxFocused(p), null)
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
async function startPage({ review = none, options = allOptions, conversation = true, defaultBranch = { why: '' } } = {}) {
  let current = review
  return openPage({
    routes: {
      '/api/default-branch': (call) => (typeof defaultBranch === 'function' ? defaultBranch(call) : defaultBranch),
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

// clipboardOf gives the page a clipboard that keeps what's written to it,
// or refuses with refusal.
function clipboardOf(p, refusal) {
  const written = []
  Object.defineProperty(p.window.navigator, 'clipboard', {
    configurable: true,
    value: { writeText: async (text) => { if (refusal) throw new Error(refusal); written.push(text) } },
  })
  return written
}

test('a code block the agent wrote has a button copying just its code', async () => {
  const code = 'line one\n  line two'
  const p = await convoPage({ events: [{ seq: 1, kind: 'user', text: 'hi' }, agentSaid(2, 'Here:\n\n```\n' + code + '\n```\n\nand more')], running: false })
  try {
    const written = clipboardOf(p)
    const copy = p.$('convo-log').querySelector('.msg.agent button.copy-code')
    assert.ok(copy, 'the block has a copy button')
    await p.click(copy)
    assert.deepEqual(written, [code])
    assert.equal(p.$('toast').textContent, 'Copied')
  } finally { p.close() }
})

// What the agent writes can hide control characters in code (an escape that
// ends a terminal's bracketed paste, so what follows runs): a code block
// shows them, and its copy leaves them out, saying so. Newlines are kept
// (and tabs, as the Markdown reader has them: spaces); a CRLF line end is a
// line end.
test('a code block shows hidden control characters, and copies without them', async () => {
  const code = 'echo safe\x1b[201~\r\nrm -rf ~\x07\tdone\x9b'
  const p = await convoPage({ events: [{ seq: 1, kind: 'user', text: 'hi' }, agentSaid(2, '```\n' + code + '\n```')], running: false })
  try {
    const written = clipboardOf(p)
    const shown = p.$('convo-log').querySelector('.msg.agent .code-block code').textContent
    assert.equal(shown, 'echo safe\u241b[201~\nrm -rf ~\u2407    done\u2426')
    await p.click(p.$('convo-log').querySelector('button.copy-code'))
    assert.deepEqual(written, ['echo safe[201~\nrm -rf ~    done'])
    assert.equal(p.$('toast').textContent, 'Copied, leaving out 3 hidden control characters')
  } finally { p.close() }
})

// Nor does the rest of what the agent writes hide them, from the eye or
// from a copy made by selecting it.
test("the agent's text shows hidden control characters too", async () => {
  const p = await convoPage({ events: [{ seq: 1, kind: 'user', text: 'hi' }, agentSaid(2, 'run `ls\x1b[201~` then\x07 go')], running: false })
  try {
    const text = p.$('convo-log').querySelector('.msg.agent .md').textContent
    assert.ok(text.includes('ls\u241b[201~') && text.includes('then\u2407 go'), text)
  } finally { p.close() }
})

test("a copy the browser refuses says so", async () => {
  const p = await convoPage({ events: [{ seq: 1, kind: 'user', text: 'hi' }, agentSaid(2, '```\nx\n```')], running: false })
  try {
    clipboardOf(p, 'Document is not focused')
    await p.click(p.$('convo-log').querySelector('button.copy-code'))
    assert.equal(p.$('toast').textContent, "Couldn't copy: Document is not focused")
    assert.ok(p.$('toast').classList.contains('bad'))
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

// pasteInto pastes into node (a text box, say) what a clipboard holding
// text and files would give.
function pasteInto(p, node, { text = '', files = [] }) {
  const ev = new p.window.Event('paste', { bubbles: true, cancelable: true })
  ev.clipboardData = { getData: (type) => (type === 'text/plain' ? text : ''), files }
  node.dispatchEvent(ev)
  return ev
}

test('in a text box, text pastes as text, as ever, even with a file alongside it', async () => {
  const p = await pastePage()
  try {
    const box = p.$('review-general').querySelector('textarea') || p.$('panel-review').querySelector('textarea')
    const target = box || p.$('review-filter')
    const ev = pasteInto(p, target, { text: 'some words', files: [new p.window.File(['png'], 'a.png', { type: 'image/png' })] })
    await p.settle()
    assert.equal(ev.defaultPrevented, false)
    assert.deepEqual(p.calls.filter((c) => c.path === '/api/clipboard/paste'), [])
  } finally { p.close() }
})

// A file copied in a file manager carries its path as text too: outside a
// text box, where text has nowhere to go, the file is what's pasted.
test('outside a text box, a file pastes as a file, even with text alongside it', async () => {
  const p = await pastePage()
  try {
    paste(p, { text: '/home/me/notes.pdf', files: [new p.window.File(['pdf'], 'notes.pdf', { type: 'application/pdf' })] })
    await p.settle()
    assert.equal(p.calls.filter((c) => c.path === '/api/clipboard/paste').length, 1)
    assert.equal(p.$('toast').textContent, 'File pasted')
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
    assert.equal(orderItem(p, 'Order by file path').getAttribute('aria-checked'), 'true')
    await p.click(orderItem(p, "Order by agent reading order"))
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
    const item = orderItem(p, "Order by agent reading order")
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

// --- commenting on lines opened from a gap, and on ranges ----------------------------------------

// openedPage is a review of c.txt (a change at lines 10-11 of 40; 1-9 and
// 12-40 hidden) with the lines after the change opened from their gap.
async function openedPage(diff = gapped) {
  const p = await openPage({
    routes: {
      '/api/state': { sandbox: 'box', sandboxTabs: true, review: true, conversation: true, reviewAt: '', attention: {} },
      '/api/review': { review: { ...none, started: '2026-10-16T10:00:00Z' }, diff },
      '/api/review/file': { lines: fileText },
      '/api/review/do': (call) => ({ review: { ...none, started: '2026-10-16T10:00:00Z' } }),
      '/api/log': { entries: [] },
    },
    storage: { 'vibe-companion-tab': 'review' },
  })
  const down = Array.from(p.$('review-files').querySelectorAll('.gap-open')).find((b) => b.textContent === '↓')
  await p.click(down)
  return p
}

const plus = (p, label) => p.$('review-files').querySelector('[aria-label="Comment on line ' + label + '"]')
const shiftClick = async (p, node) => { node.dispatchEvent(new p.window.MouseEvent('click', { bubbles: true, shiftKey: true })); await p.settle() }

async function comment(p, text) {
  await p.type(p.window.document.querySelector('.review-compose-text'), text)
  await p.click(p.button('Comment', p.$('review-files')))
  return p.posted('/api/review/do')
}

test('a line opened from a gap can be commented on, on either side', async () => {
  const p = await openedPage()
  try {
    assert.ok(plus(p, 'R20'), 'the new line has a +')
    await p.click(plus(p, 'R20'))
    assert.deepEqual(await comment(p, 'why?'), [{ op: 'comment', file: 'c.txt', side: 'new', line: 20, code: 'line 20', body: 'why?' }])
  } finally { p.close() }
  const q = await openedPage()
  try {
    // After the change, old lines are one behind the new: old 19 is new 20.
    await q.click(plus(q, 'L19'))
    assert.deepEqual(await comment(q, 'and this?'), [{ op: 'comment', file: 'c.txt', side: 'old', line: 19, code: 'line 20', body: 'and this?' }])
  } finally { q.close() }
})

test('Shift-click makes a range, its lines marked', async () => {
  const p = await openedPage()
  try {
    await p.click(plus(p, 'R20'))
    await shiftClick(p, plus(p, 'R22'))
    assert.equal(p.$('review-files').querySelector('.review-compose-head').textContent, 'Add a comment on lines R20–R22')
    const marked = Array.from(p.$('review-files').querySelectorAll('tr.selected td.num.start')).map((td) => td.textContent.replace('+', ''))
    assert.deepEqual(marked, ['20', '21', '22'])
    assert.deepEqual(await comment(p, 'these three'), [{ op: 'comment', file: 'c.txt', side: 'new', line: 20, endLine: 22, code: 'line 20\nline 21\nline 22', body: 'these three' }])
  } finally { p.close() }
})

// Dragging a + onto another line's moves the comment being written there,
// keeping what's written (as clicking that + does); only Shift-click makes
// a range (the user's choice, 2026-10-16).
test('dragging from one + to another moves the comment being written, keeping its text', async () => {
  const p = await openedPage()
  try {
    await p.click(plus(p, 'R20'))
    await p.type(p.window.document.querySelector('.review-compose-text'), 'kept')
    plus(p, 'R20').dispatchEvent(new p.window.Event('pointerdown', { bubbles: true }))
    plus(p, 'R23').dispatchEvent(new p.window.Event('pointerup', { bubbles: true }))
    await p.settle()
    assert.equal(p.$('review-files').querySelector('.review-compose-head').textContent, 'Add a comment on line R23')
    assert.equal(p.window.document.querySelector('.review-compose-text').value, 'kept')
  } finally { p.close() }
})

// sidesPage is c.txt with, side by side: 10|10, 11|- (deleted), 12|11,
// -|12 (added): rows with a line on both sides, and on one only.
async function sidesPage() {
  const diff = [{ path: 'c.txt', status: 'modified', hunks: [{ oldStart: 10, newStart: 10, header: '', lines: [
    { kind: 'context', old: 10, new: 10, text: 'ten' },
    { kind: 'del', old: 11, text: 'gone' },
    { kind: 'context', old: 12, new: 11, text: 'kept' },
    { kind: 'add', new: 12, text: 'new' },
  ] }] }]
  return openPage({
    routes: {
      '/api/state': { sandbox: 'box', sandboxTabs: true, review: true, conversation: true, reviewAt: '', attention: {} },
      '/api/review': { review: { ...none, started: '2026-10-16T10:00:00Z' }, diff },
      '/api/review/do': () => ({ review: { ...none, started: '2026-10-16T10:00:00Z' } }),
      '/api/log': { entries: [] },
    },
    storage: { 'vibe-companion-tab': 'review' },
  })
}

test('a range begun in one column and ended in the other takes the side both rows have, the new file’s first', async () => {
  const p = await sidesPage()
  try {
    await p.click(plus(p, 'L10'))
    await shiftClick(p, plus(p, 'R12'))
    assert.equal(p.$('review-files').querySelector('.review-compose-head').textContent, 'Add a comment on lines R10–R12')
    assert.deepEqual(await comment(p, 'all of it'), [{ op: 'comment', file: 'c.txt', side: 'new', line: 10, endLine: 12, code: 'ten\nkept\nnew', body: 'all of it' }])
  } finally { p.close() }
})

test('a range between rows with no side in common is refused, saying why', async () => {
  const p = await sidesPage()
  try {
    await p.click(plus(p, 'L11'))
    await shiftClick(p, plus(p, 'R12'))
    assert.match(p.$('toast').textContent, /same side/)
    assert.equal(p.$('review-files').querySelector('.review-compose-head').textContent, 'Add a comment on line L11', 'left as it was')
  } finally { p.close() }
})

test("a range can't take in lines still hidden", async () => {
  const twoChanges = [{ path: 'c.txt', status: 'modified', hunks: [
    { oldStart: 10, newStart: 10, header: '', lines: [{ kind: 'context', old: 10, new: 10, text: 'line 10' }, { kind: 'add', new: 11, text: 'added' }] },
    { oldStart: 29, newStart: 30, header: '', lines: [{ kind: 'context', old: 29, new: 30, text: 'line 30' }] },
  ] }]
  const p = await openPage({
    routes: {
      '/api/state': { sandbox: 'box', sandboxTabs: true, review: true, conversation: true, reviewAt: '', attention: {} },
      '/api/review': { review: { ...none, started: '2026-10-16T10:00:00Z' }, diff: twoChanges },
      '/api/log': { entries: [] },
    },
    storage: { 'vibe-companion-tab': 'review' },
  })
  try {
    await p.click(plus(p, 'R11'))
    await shiftClick(p, plus(p, 'R30'))
    assert.match(p.$('toast').textContent, /hidden/)
    assert.equal(p.$('review-files').querySelector('.review-compose-head').textContent, 'Add a comment on line R11', 'left as it was')
  } finally { p.close() }
})

test('in one column, a line opened from a gap is commented on as the new file’s', async () => {
  const p = await openedPage()
  try {
    await p.click('review-layout')
    assert.equal(plus(p, 'L19'), null, 'one + a line, as for changed lines')
    await p.click(plus(p, 'R20'))
    assert.deepEqual(await comment(p, 'why?'), [{ op: 'comment', file: 'c.txt', side: 'new', line: 20, code: 'line 20', body: 'why?' }])
  } finally { p.close() }
})

test('a comment that has followed its code says where it was made', async () => {
  const one = [{ path: 'a.txt', status: 'modified', hunks: [{ oldStart: 1, newStart: 1, header: '', lines: [{ kind: 'add', new: 1, text: 'x' }, { kind: 'add', new: 2, text: 'y' }] }] }]
  const p = await reviewPage({ review: { ...none, started: '2026-10-16T10:00:00Z', lastId: 1,
    threads: [{ id: 1, file: 'a.txt', side: 'new', line: 2, endLine: 2, movedFrom: 9, code: 'y', status: 'open', comments: said('hm') }] }, diff: one })
  try {
    await p.click('tab-review')
    assert.match(p.$('review-thread-1').querySelector('.muted').textContent, /moved from line 9/)
  } finally { p.close() }
})

// --- "Viewed" ticks ------------------------------------------------------------------------------

// viewedPage is a review of three files (b.txt with a comment) whose fake
// vibe keeps the ticks the page asks for.
async function viewedPage(viewed = {}) {
  const one = (path) => ({ path, status: 'modified', fingerprint: 'fp-' + path, hunks: [{ oldStart: 1, newStart: 1, header: '', lines: [{ kind: 'add', new: 1, text: 'x' }] }] })
  let review = { ...none, started: '2026-10-16T10:00:00Z', lastId: 1, viewed,
    threads: [{ id: 1, file: 'b.txt', side: 'new', line: 1, endLine: 1, code: 'x', status: 'open', comments: said('on b') }] }
  return openPage({
    routes: {
      '/api/state': { sandbox: 'box', sandboxTabs: true, review: true, conversation: true, reviewAt: '', attention: {} },
      '/api/review': () => ({ review, diff: ['a.txt', 'b.txt', 'c.txt'].map(one) }),
      '/api/review/do': (call) => {
        const v = { ...review.viewed }
        if (call.body.op === 'view') v[call.body.file] = call.body.code
        if (call.body.op === 'unview') delete v[call.body.file]
        review = { ...review, viewed: v }
        return { review }
      },
      '/api/log': { entries: [] },
    },
    storage: { 'vibe-companion-tab': 'review' },
  })
}

const tick = (p, path) => p.$('review-files').querySelector('[data-path="' + path + '"] header .review-viewed input')
const folded = (p, path) => p.$('review-files').querySelector('[data-path="' + path + '"] table').closest('[hidden]') !== null

test('ticking a file as viewed folds it, dims it in the tree, and counts it', async () => {
  const p = await viewedPage()
  try {
    assert.equal(tick(p, 'b.txt').checked, false)
    await p.click(tick(p, 'b.txt'))
    assert.deepEqual(p.posted('/api/review/do'), [{ op: 'view', file: 'b.txt', code: 'fp-b.txt' }])
    assert.equal(tick(p, 'b.txt').checked, true)
    assert.ok(folded(p, 'b.txt'), 'its diff folded away')
    assert.ok(treeRow(p, 'b.txt').classList.contains('viewed'))
    assert.match(p.$('review-tree-count').textContent, /1\/3 viewed/)
    await p.click(tick(p, 'b.txt'))
    assert.deepEqual(p.posted('/api/review/do')[1], { op: 'unview', file: 'b.txt' })
    assert.equal(folded(p, 'b.txt'), false, 'shown again')
  } finally { p.close() }
})

test('a file whose change has changed since it was ticked says so, unticked', async () => {
  const p = await viewedPage({ 'a.txt': 'fp-a.txt', 'c.txt': 'what it was' })
  try {
    assert.equal(tick(p, 'a.txt').checked, true)
    assert.equal(tick(p, 'c.txt').checked, false)
    assert.match(p.$('review-files').querySelector('[data-path="c.txt"] header').textContent, /changed since you viewed it/)
    assert.equal(folded(p, 'c.txt'), false)
    assert.match(p.$('review-tree-count').textContent, /1\/3 viewed/)
  } finally { p.close() }
})

test('the comment arrows pass over comments in viewed files, and say so', async () => {
  const p = await viewedPage({ 'b.txt': 'fp-b.txt' })
  try {
    assert.equal(p.$('review-count').textContent, '1 comment (1 in viewed files)')
    assert.equal(p.$('review-next-comment').disabled, true)
  } finally { p.close() }
})

// --- the formatting toolbar ------------------------------------------------------------------

const select = (box, text, from, to) => { box.focus(); box.value = text; box.dispatchEvent(new box.ownerDocument.defaultView.Event('input', { bubbles: true })); box.setSelectionRange(from, to) }
const toolbarOf = (box) => box.previousElementSibling && box.previousElementSibling.matches('.md-toolbar') ? box.previousElementSibling : box.parentElement.querySelector('.md-toolbar')
const pressKey = async (p, box, key) => { box.dispatchEvent(new p.window.KeyboardEvent('keydown', { key, ctrlKey: true, bubbles: true, cancelable: true })); await p.settle() }

test('a reply has a formatting toolbar, and what it does is replied', async () => {
  const p = await reviewPage({ review: commented, diff: twoFiles })
  try {
    await p.click('tab-review')
    const box = p.$('review-thread-1').querySelector('textarea[data-thread]')
    const bar = toolbarOf(box)
    assert.deepEqual(Array.from(bar.querySelectorAll('button')).map((b) => b.getAttribute('aria-label')),
      ['Bold (Ctrl+B)', 'Italic (Ctrl+I)', 'Code', 'Code block', 'Link (Ctrl+K)', 'Quote', 'Bulleted list', 'Numbered list'])
    select(box, 'a word here', 2, 6)
    await p.click(bar.querySelector('[aria-label="Bold (Ctrl+B)"]'))
    assert.equal(box.value, 'a **word** here')
    await p.click(p.button('Reply', p.$('review-thread-1')))
    assert.deepEqual(p.posted('/api/review/do'), [{ op: 'reply', thread: 1, body: 'a **word** here' }])
  } finally { p.close() }
})

test('Ctrl+B, Ctrl+I and Ctrl+K format a new comment as it is written', async () => {
  const p = await openedPage()
  try {
    await p.click(plus(p, 'R20'))
    const box = p.window.document.querySelector('.review-compose-text')
    assert.ok(toolbarOf(box), 'the composer has the toolbar')
    select(box, 'see docs', 4, 8)
    await pressKey(p, box, 'k')
    assert.equal(box.value, 'see [docs](url)')
    select(box, 'x', 0, 1)
    await pressKey(p, box, 'i')
    assert.equal(box.value, '_x_')
    assert.deepEqual(await comment(p, '_x_'), [{ op: 'comment', file: 'c.txt', side: 'new', line: 20, code: 'line 20', body: '_x_' }])
  } finally { p.close() }
})

test('editing an unsent comment has the toolbar too', async () => {
  const p = await reviewPage({ review: unsent })
  try {
    await p.click('tab-review')
    await p.click(p.button('Edit', p.$('review-general')))
    const box = p.$('review-general').querySelector('textarea[aria-label="Edit your comment"]')
    select(box, 'one\ntwo', 0, 7)
    await p.click(toolbarOf(box).querySelector('[aria-label="Bulleted list"]'))
    assert.equal(box.value, '- one\n- two')
  } finally { p.close() }
})

test('clicking another line’s + moves the comment being written there, keeping what’s written', async () => {
  const p = await openedPage()
  try {
    await p.click(plus(p, 'R20'))
    await p.type(p.window.document.querySelector('.review-compose-text'), 'half a thought')
    await p.click(plus(p, 'R25'))
    assert.equal(p.$('review-files').querySelector('.review-compose-head').textContent, 'Add a comment on line R25')
    assert.equal(p.window.document.querySelector('.review-compose-text').value, 'half a thought')
  } finally { p.close() }
})

// --- a page left open across a rebuild of vibe ------------------------------------------------------

test('a page older than the vibe serving it says so, with a way to reload', async () => {
  const served = { version: 'v1' }
  const p = await openPage({
    routes: {
      '/api/state': () => ({ sandbox: 'box', sandboxTabs: true, review: true, conversation: true, reviewAt: '', attention: {}, pageVersion: served.version }),
      '/api/log': { entries: [] },
    },
    storage: { 'vibe-companion-tab': 'log' },
  })
  try {
    assert.equal(p.$('page-outdated').hidden, true, 'the version it was opened with')
    await p.poll()
    assert.equal(p.$('page-outdated').hidden, true)
    served.version = 'v2'
    await p.poll()
    assert.equal(p.$('page-outdated').hidden, false)
    assert.match(p.$('page-outdated').textContent, /vibe has been updated/)
    assert.ok(p.button('Reload', p.$('page-outdated')))
  } finally { p.close() }
})

// --- editing a comment the agent has seen -------------------------------------------------------------

const seen = { lastId: 1, threads: [{ id: 1, status: 'open', comments: [
  { author: 'agent', body: 'P3: something', at: '2026-10-16T10:00:00Z' },
  { author: 'user', body: 'as sent', at: '2026-10-16T10:05:00Z', sent: true },
] }] }

test('a comment the agent has seen can be edited, not deleted; the agent’s can’t be either', async () => {
  const p = await reviewPage({ review: seen })
  try {
    await p.click('tab-review')
    const general = p.$('review-general')
    assert.equal(general.querySelectorAll('.comment')[0].querySelector('.comment-actions'), null, "the agent's")
    assert.equal(p.button('Delete', general), undefined)
    await p.click(p.button('Edit', general))
    const box = general.querySelector('textarea[aria-label="Edit your comment"]')
    assert.equal(box.value, 'as sent')
    await p.type(box, 'better')
    await p.click(p.button('Save', general))
    assert.deepEqual(p.posted('/api/review/do'), [{ op: 'edit', thread: 1, at: '2026-10-16T10:05:00Z', body: 'better' }])
  } finally { p.close() }
})

test('an edited comment says so, shows what it said on asking, and that the edit is to be sent', async () => {
  const edited = { ...seen, threads: [{ ...seen.threads[0], comments: [seen.threads[0].comments[0],
    { author: 'user', body: 'better', at: '2026-10-16T10:05:00Z', was: ['as sent'] }] }] }
  const p = await reviewPage({ review: edited })
  try {
    await p.click('tab-review')
    const mine = p.$('review-general').querySelectorAll('.comment')[1]
    assert.match(mine.textContent, /edit not sent yet/)
    assert.equal(p.button('Delete', mine), undefined, 'the agent saw it before')
    assert.doesNotMatch(mine.textContent, /as sent/, 'what it said is out of the way')
    await p.click(p.button('edited', mine))
    assert.match(p.$('review-general').querySelectorAll('.comment')[1].textContent, /as sent/)
  } finally { p.close() }
})

// --- syntax colouring ------------------------------------------------------------------------------

// colourPage is a review of the files diff, whose fake vibe colours a file
// as colours says, by "path|side".
async function colourPage(diff, colours) {
  return openPage({
    routes: {
      '/api/state': { sandbox: 'box', sandboxTabs: true, review: true, conversation: true, reviewAt: '', attention: {} },
      '/api/review': { review: { ...none, started: '2026-10-16T10:00:00Z' }, diff },
      '/api/review/highlight': (call) => {
        const q = new URLSearchParams(call.query)
        return colours[q.get('path') + '|' + q.get('side')] || {}
      },
      '/api/log': { entries: [] },
    },
    storage: { 'vibe-companion-tab': 'review' },
  })
}

const goDiff = [{ path: 'main.go', status: 'modified', hunks: [{ oldStart: 1, newStart: 1, header: '', lines: [
  { kind: 'context', old: 1, new: 1, text: 'package main' },
  { kind: 'del', old: 2, text: 'var x = 1' },
  { kind: 'add', new: 2, text: 'var x = 2' },
] }] }]
const goColours = {
  'main.go|old': { language: 'Go', lines: [[{ c: 'kn', t: 'package' }, { t: ' ' }, { c: 'nn', t: 'main' }], [{ c: 'kd', t: 'var' }, { t: ' x ' }, { c: 'o', t: '=' }, { t: ' ' }, { c: 'mi', t: '1' }]] },
  'main.go|new': { language: 'Go', lines: [[{ c: 'kn', t: 'package' }, { t: ' ' }, { c: 'nn', t: 'main' }], [{ c: 'kd', t: 'var' }, { t: ' x ' }, { c: 'o', t: '=' }, { t: ' ' }, { c: 'mi', t: '2' }]] },
}

// codeOf is the code cell of side's line n: its coloured runs, as kind:text.
const codeOf = (p, side, n) => {
  const plus = p.$('review-files').querySelector(`[aria-label="Comment on line ${side === 'new' ? 'R' : 'L'}${n}"]`)
  const code = plus.closest('td').nextElementSibling.nextElementSibling
  return Array.from(code.childNodes).map((x) => (x.nodeType === 3 ? ':' : x.className.replace('hl-', '') + ':') + x.textContent).join(' ')
}

test('code is coloured by its language, on both sides', async () => {
  const p = await colourPage(goDiff, goColours)
  try {
    assert.equal(codeOf(p, 'new', 2), 'kw:var : x  op:= :  num:2')
    assert.equal(codeOf(p, 'old', 2), 'kw:var : x  op:= :  num:1')
    assert.equal(codeOf(p, 'new', 1), 'kw:package :  type:main')
  } finally { p.close() }
})

// A file's colours arriving redraw its own card alone: redrawing every
// file for each would take time growing with the square of the review's
// size (minutes of a frozen tab, for a big one, scrolled through).
test("a file's colours redraw its own card, not the others'", async () => {
  const twoGo = [goDiff[0], { ...goDiff[0], path: 'other.go' }]
  let release
  const held = new Promise((resolve) => { release = resolve })
  const colours = { ...goColours, 'other.go|old': goColours['main.go|old'], 'other.go|new': goColours['main.go|new'] }
  const p = await openPage({
    routes: {
      '/api/state': { sandbox: 'box', sandboxTabs: true, review: true, conversation: true, reviewAt: '', attention: {} },
      '/api/review': { review: { ...none, started: '2026-10-16T10:00:00Z' }, diff: twoGo },
      '/api/review/highlight': async (call) => {
        const q = new URLSearchParams(call.query)
        if (q.get('path') === 'other.go') await held
        return colours[q.get('path') + '|' + q.get('side')] || {}
      },
      '/api/log': { entries: [] },
    },
    storage: { 'vibe-companion-tab': 'review' },
    settled: false,
  })
  try {
    const card = (path) => Array.from(p.$('review-files').querySelectorAll('.review-file')).find((c) => c.dataset.path === path)
    const coloured = (path) => !!card(path) && !!card(path).querySelector('.hl-kw')
    for (let i = 0; i < 200 && !coloured('main.go'); i++) await new Promise((resolve) => setImmediate(resolve))
    assert.ok(coloured('main.go'), 'main.go coloured first')
    const mainCard = card('main.go')
    release()
    await p.settle()
    assert.ok(coloured('other.go'), 'other.go coloured when its colours came')
    assert.equal(card('main.go'), mainCard, "main.go's card wasn't drawn again for other.go's colours")
  } finally { p.close() }
})

// A file is coloured when it comes near the screen. Scrolled past before
// its colours come, it's given up on (vibe stops colouring it), and asked
// for again if it comes back.
test('colouring a file scrolled past is given up, and asked for again on coming back', async () => {
  const twoGo = [goDiff[0], { ...goDiff[0], path: 'other.go' }]
  let release
  let gate = new Promise((resolve) => { release = resolve })
  const colours = { ...goColours, 'other.go|old': goColours['main.go|old'], 'other.go|new': goColours['main.go|new'] }
  const p = await openPage({
    routes: {
      '/api/state': { sandbox: 'box', sandboxTabs: true, review: true, conversation: true, reviewAt: '', attention: {} },
      '/api/review': { review: { ...none, started: '2026-10-16T10:00:00Z' }, diff: twoGo },
      '/api/review/highlight': async (call) => {
        const q = new URLSearchParams(call.query)
        if (q.get('path') === 'other.go') await gate
        return colours[q.get('path') + '|' + q.get('side')] || {}
      },
      '/api/log': { entries: [] },
    },
    storage: { 'vibe-companion-tab': 'review' },
    seeing: true,
  })
  try {
    const card = (path) => Array.from(p.$('review-files').querySelectorAll('.review-file')).find((c) => c.dataset.path === path)
    const asked = (path) => p.calls.filter((c) => c.path === '/api/review/highlight' && new URLSearchParams(c.query).get('path') === path)
    assert.equal(asked('other.go').length, 0, 'nothing asked for before it comes near the screen')
    await p.seen((el) => el.dataset.path === 'other.go')
    assert.equal(asked('other.go').length, 2, 'both sides asked for, coming near')
    await p.seen((el) => el.dataset.path === 'other.go', false)
    assert.ok(asked('other.go').every((c) => c.aborted), 'given up on, scrolled past')
    release()
    await p.settle()
    assert.equal(card('other.go').querySelector('.hl-kw'), null, 'not coloured by what was given up on')
    gate = Promise.resolve()
    await p.seen((el) => el.dataset.path === 'other.go')
    await p.settle()
    assert.equal(asked('other.go').length, 4, 'asked for again, coming back')
    assert.ok(card('other.go').querySelector('.hl-kw'), 'coloured this time')
  } finally { p.close() }
})

test("a line that no longer reads as it was coloured is shown plain", async () => {
  const stale = { ...goColours, 'main.go|new': { language: 'Go', lines: [goColours['main.go|new'].lines[0], [{ c: 'kd', t: 'var' }, { t: ' y = 3' }]] } }
  const p = await colourPage(goDiff, stale)
  try {
    assert.equal(codeOf(p, 'new', 2), ':var x = 2')
  } finally { p.close() }
})

test("a file too long to colour says so; an added one is asked for its new side alone", async () => {
  const diff = [{ path: 'big.go', status: 'added', hunks: [{ oldStart: 0, newStart: 1, header: '', lines: [{ kind: 'add', new: 1, text: 'x' }] }] }]
  const p = await colourPage(diff, { 'big.go|new': { skipped: 'too long to colour (20000 lines)' } })
  try {
    assert.match(p.$('review-files').querySelector('[data-path="big.go"] header').textContent, /not coloured: too long to colour/)
    const asked = p.calls.filter((c) => c.path === '/api/review/highlight').map((c) => new URLSearchParams(c.query).get('side'))
    assert.deepEqual(asked, ['new'])
  } finally { p.close() }
})

// --- the comments overlay ---------------------------------------------------------------------------

// overlaid is the commented review, with a thread of the user's (#5, the
// newest), on twoFiles.
const overlaid = { ...commented, lastId: 5, threads: [...commented.threads,
  { id: 5, file: 'b.txt', side: 'new', line: 1, status: 'open', comments: [{ author: 'user', body: 'mine, and newest', at: '2026-10-16T12:00:00Z', sent: true }] }] }
overlaid.threads[1] = { ...overlaid.threads[1], comments: [...overlaid.threads[1].comments, { author: 'user', body: 'a reply', at: '2026-10-16T11:00:00Z', sent: true }] }

const cards = (p) => Array.from(p.$('review-comments-list').querySelectorAll('.comment-card')).map((c) => c.dataset.thread)

test('the comments button opens a pane of every thread over the diff, and closes it again', async () => {
  const p = await reviewPage({ review: overlaid, diff: twoFiles })
  try {
    await p.click('tab-review')
    const pane = p.$('review-comments')
    assert.equal(p.$('review-comments-open').textContent.trim(), '5')
    assert.equal(pane.classList.contains('open'), false)
    await p.click('review-comments-open')
    assert.equal(pane.classList.contains('open'), true)
    assert.equal(pane.getAttribute('aria-hidden'), 'false')
    assert.deepEqual(cards(p), ['1', '3', '2', '4', '5'], 'in the arrows’ order: the whole review, then a.txt by line, then b.txt')
    await p.click('review-comments-close')
    assert.equal(pane.classList.contains('open'), false, '✕')
    await p.click('review-comments-open')
    pane.dispatchEvent(new p.window.KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    assert.equal(pane.classList.contains('open'), false, 'Escape')
  } finally { p.close() }
})

test('each card says where its thread is, its state, how it starts, and its replies', async () => {
  const p = await reviewPage({ review: overlaid, diff: twoFiles })
  try {
    await p.click('tab-review')
    await p.click('review-comments-open')
    const card = p.$('review-comments-list').querySelector('[data-thread="2"]')
    assert.match(card.textContent, /a\.txt:2/)
    assert.match(card.textContent, /on two/)
    assert.match(card.textContent, /1 reply/)
    assert.match(p.$('review-comments-list').querySelector('[data-thread="4"]').textContent, /resolved/)
    assert.match(p.$('review-comments-list').querySelector('[data-thread="1"]').textContent, /the whole review/)
  } finally { p.close() }
})

test("a card whose thread waits on your answer (open, the agent's the last word) stands out", async () => {
  const p = await reviewPage({ review: overlaid, diff: twoFiles })
  try {
    await p.click('tab-review')
    await p.click('review-comments-open')
    const card = (id) => p.$('review-comments-list').querySelector(`[data-thread="${id}"]`)
    assert.equal(card(1).classList.contains('awaiting'), true)
    assert.match(card(1).textContent, /awaiting your answer/)
    assert.equal(card(2).classList.contains('awaiting'), false, 'you answered last')
    assert.equal(card(4).classList.contains('awaiting'), false, 'resolved')
    assert.equal(card(5).classList.contains('awaiting'), false, 'yours, unanswered')
  } finally { p.close() }
})

test('the pane filters to open threads, or yours, and orders by newest', async () => {
  const p = await reviewPage({ review: overlaid, diff: twoFiles })
  try {
    await p.click('tab-review')
    await p.click('review-comments-open')
    await p.click(p.button('Open', p.$('review-comments')))
    assert.deepEqual(cards(p), ['1', '3', '2', '5'], 'the resolved one left out')
    await p.click(p.button('Yours', p.$('review-comments')))
    assert.deepEqual(cards(p), ['5'], 'started by you')
    await p.click(p.button('All', p.$('review-comments')))
    const order = p.$('review-comments-order')
    order.value = 'newest'
    order.dispatchEvent(new p.window.Event('change', { bubbles: true }))
    await p.settle()
    assert.deepEqual(cards(p), ['5', '2', '1', '3', '4'], 'by their latest comment, newest first')
  } finally { p.close() }
})

test('a card takes you to its thread, closing the pane, its reply box ready', async () => {
  const p = await reviewPage({ review: overlaid, diff: twoFiles })
  try {
    const shown = []
    p.window.Element.prototype.scrollIntoView = function () { shown.push(this.id) }
    await p.click('tab-review')
    await p.click('review-comments-open')
    await p.click(p.$('review-comments-list').querySelector('[data-thread="3"]'))
    assert.deepEqual(shown, ['review-thread-3'])
    assert.equal(p.$('review-comments').classList.contains('open'), false)
    assert.equal(p.window.document.activeElement.dataset.thread, '3')
  } finally { p.close() }
})

test('a card for a thread the tree filter hides clears the filter to get there', async () => {
  const p = await reviewPage({ review: overlaid, diff: twoFiles })
  try {
    const shown = []
    p.window.Element.prototype.scrollIntoView = function () { shown.push(this.id) }
    await p.click('tab-review')
    await p.type(p.$('review-filter'), 'b.txt')
    await p.click('review-comments-open')
    await p.click(p.$('review-comments-list').querySelector('[data-thread="2"]'))
    assert.equal(p.$('review-filter').value, '')
    assert.deepEqual(shown, ['review-thread-2'])
  } finally { p.close() }
})

test("the pane offers every order; the first two are the comment arrows' own, kept in step with the count's menu", async () => {
  const p = await reviewPage({ ...stepped })
  try {
    await p.click('tab-review')
    await p.click('review-comments-open')
    const order = p.$('review-comments-order')
    assert.deepEqual(Array.from(order.options).map((o) => o.textContent), ['Order by file path', 'Order by agent reading order', 'Newest first'])
    assert.equal(order.value, 'path')
    await p.click('review-order')
    await p.click(orderItem(p, 'Order by agent reading order'))
    assert.equal(order.value, 'reading', 'chosen under the count')
    order.value = 'path'
    order.dispatchEvent(new p.window.Event('change', { bubbles: true }))
    await p.settle()
    assert.match(p.$('review-order').title, /path order/, 'chosen in the pane: the arrows follow')
    assert.equal(p.window.localStorage.getItem('vibe-review-comment-order'), 'path')
  } finally { p.close() }
})

test("with no reading order, the pane can't offer it either", async () => {
  const p = await reviewPage({ ...stepped, review: { ...stepped.review, order: [] } })
  try {
    await p.click('tab-review')
    await p.click('review-comments-open')
    assert.equal(p.$('review-comments-order').options[1].disabled, true)
  } finally { p.close() }
})

// --- what the review found in the tab itself --------------------------------------------------

const started = { threads: [], lastId: 0, started: '2026-10-16T10:00:00Z' }
const oneFile = (text) => [{ path: 'a.txt', status: 'modified', hunks: [{ oldStart: 1, newStart: 1, header: '@@ -1 +1 @@', lines: [
  { kind: 'del', old: 1, text: 'one' },
  { kind: 'add', new: 1, text },
] }] }]
const codeShown = (p) => Array.from(p.$('review-files').querySelectorAll('td.code')).map((td) => td.textContent)

// changingPage is a review whose diff (now.diff) can change under it, as
// the folder's code does.
async function changingPage(now, extra = {}) {
  return openPage({
    routes: {
      '/api/state': { sandbox: 'box', sandboxTabs: true, review: true, conversation: true, reviewAt: '', attention: {} },
      '/api/review': (call) => ({ review: now.review || started, diff: call.query.includes('diff=1') ? now.diff : undefined }),
      '/api/review/do': () => ({ review: now.review || started }),
      '/api/log': { entries: [] },
      ...extra,
    },
    storage: { 'vibe-companion-tab': 'review' },
  })
}

test('Refresh shows the code as it is now, the files the same', async () => {
  const now = { diff: oneFile('first version') }
  const p = await changingPage(now)
  try {
    assert.ok(codeShown(p).includes('first version'))
    now.diff = oneFile('second version')
    await p.click('review-refresh')
    assert.ok(codeShown(p).includes('second version'), codeShown(p).join('|'))
  } finally { p.close() }
})

test('a gap opened before Refresh is closed after it, not drawn blank', async () => {
  const now = { diff: gapped }
  const p = await changingPage(now, { '/api/review/file': { lines: fileText } })
  try {
    // Side by side, an added line's old side is empty: those aside.
    const blanks = () => codeShown(p).filter((t) => t === '').length
    const before = blanks()
    const down = Array.from(p.$('review-files').querySelectorAll('.gap-open')).find((b) => b.textContent === '↓')
    await p.click(down)
    assert.ok(codeShown(p).includes('line 12'), 'opened')
    await p.click('review-refresh')
    assert.ok(!codeShown(p).includes('line 12'), 'closed again')
    assert.equal(blanks(), before, 'no blank lines')
  } finally { p.close() }
})

const longFile = [{ path: 'long.txt', status: 'added', hunks: [{ oldStart: 0, newStart: 1, header: '@@ -0,0 +1,700 @@',
  lines: Array.from({ length: 700 }, (_, i) => ({ kind: 'add', new: i + 1, text: 'line ' + (i + 1) })) }] }]

test('a long file shown stays shown: a comment can be written on it', async () => {
  const now = { diff: longFile }
  const p = await changingPage(now)
  try {
    await p.click(p.button('Show 700 lines', p.$('review-files')))
    await p.click(p.$('review-files').querySelector('[aria-label="Comment on line R5"]'))
    assert.ok(p.window.document.querySelector('.review-compose-text'), 'the comment box is there')
    assert.equal(p.button('Show 700 lines', p.$('review-files')), undefined, 'not folded again')
    now.review = { ...started, threads: [{ id: 1, status: 'open', comments: said('news') }], lastId: 1 }
    await p.poll()
    assert.equal(p.button('Show 700 lines', p.$('review-files')), undefined, 'nor by a poll')
  } finally { p.close() }
})

test('with no review open, what can be reviewed is asked once, not every poll', async () => {
  const p = await reviewPage({ review: none })
  try {
    await p.click('tab-review')
    await p.poll()
    await p.poll()
    await p.poll()
    assert.equal(p.calls.filter((c) => c.path === '/api/review/options').length, 1)
    await p.click('review-refresh')
    assert.equal(p.calls.filter((c) => c.path === '/api/review/options').length, 2, 'Refresh asks again')
  } finally { p.close() }
})

test("an empty comment box with focus doesn't hold back what the agent says, and keeps focus", async () => {
  const now = { diff: twoFiles }
  const p = await changingPage(now)
  try {
    await p.click(p.$('review-files').querySelector('[aria-label="Comment on line R2"]'))
    const box = () => p.window.document.querySelector('.review-compose-text')
    box().focus()
    now.review = { ...started, threads: [{ id: 1, status: 'open', comments: said('news from the agent') }], lastId: 1 }
    await p.poll()
    assert.ok(p.$('review-general').textContent.includes('news from the agent'), 'shown')
    assert.equal(p.window.document.activeElement, box(), 'the comment box keeps focus')
  } finally { p.close() }
})

test('editing another comment asks before losing an edit in progress', async () => {
  const two = { lastId: 2, threads: [
    { id: 1, status: 'open', comments: [{ author: 'user', body: 'first', at: '2026-10-16T10:05:00Z' }] },
    { id: 2, status: 'open', comments: [{ author: 'user', body: 'second', at: '2026-10-16T10:06:00Z' }] },
  ] }
  const p = await reviewPage({ review: two })
  try {
    await p.click('tab-review')
    await p.click(p.button('Edit', p.$('review-thread-1')))
    await p.type(p.$('review-thread-1').querySelector('textarea[aria-label="Edit your comment"]'), 'first, better')
    await p.click(p.button('Edit', p.$('review-thread-2')))
    assert.ok(p.$('confirm').open, 'asked')
    await p.confirm(false)
    assert.equal(p.$('review-thread-1').querySelector('textarea[aria-label="Edit your comment"]').value, 'first, better', 'kept')
    await p.click(p.button('Edit', p.$('review-thread-2')))
    await p.confirm(true)
    assert.ok(p.$('review-thread-2').querySelector('textarea[aria-label="Edit your comment"]'), 'editing the second')
  } finally { p.close() }
})

test("a comment half-written on a review doesn't turn up on the next one", async () => {
  const now = { diff: twoFiles }
  const p = await changingPage(now)
  try {
    await p.click(p.$('review-files').querySelector('[aria-label="Comment on line R2"]'))
    await p.type(p.window.document.querySelector('.review-compose-text'), 'half')
    // A click on a button takes the focus there, in a browser.
    p.window.document.activeElement.blur()
    now.review = { ...started, closed: 'You closed it.' }
    await p.click('review-close')
    now.review = { ...started, started: '2026-10-16T11:00:00Z' }
    await p.click('review-reopen')
    assert.equal(p.window.document.querySelector('.review-compose-text'), null)
  } finally { p.close() }
})

test("Delete isn't offered on a comment with replies: they'd have to go first", async () => {
  const replied = { lastId: 1, threads: [{ id: 1, status: 'open', comments: [
    { author: 'user', body: 'mine', at: '2026-10-16T10:05:00Z' },
    { author: 'user', body: 'and more', at: '2026-10-16T10:06:00Z' },
  ] }] }
  const p = await reviewPage({ review: replied })
  try {
    await p.click('tab-review')
    const deletes = Array.from(p.$('review-thread-1').querySelectorAll('button')).filter((b) => b.textContent === 'Delete')
    assert.equal(deletes.length, 1, 'only on the reply')
    assert.ok(deletes[0].closest('.comment').textContent.includes('and more'))
  } finally { p.close() }
})

test('a poll that set out before a reply lands without undoing it', async () => {
  const before = { lastId: 1, threads: [{ id: 1, status: 'open', comments: said('on the whole review') }] }
  const after = { lastId: 1, threads: [{ id: 1, status: 'open', comments: [...said('on the whole review'), { author: 'user', body: 'my reply', at: '2026-10-16T10:07:00Z' }] }] }
  let answerPoll
  let slow = false
  const p = await openPage({
    routes: {
      '/api/state': { sandbox: 'box', sandboxTabs: true, review: true, conversation: true, reviewAt: '', attention: {} },
      '/api/review': () => (!slow ? { review: before, diff: [] } : new Promise((resolve) => { answerPoll = () => resolve({ review: before }) })),
      '/api/review/do': () => ({ review: after }),
      '/api/log': { entries: [] },
    },
    storage: { 'vibe-companion-tab': 'review' },
  })
  try {
    // A poll sets out, and is slow to answer.
    slow = true
    const pollGone = p.poll().catch(() => {})
    await new Promise((resolve) => setImmediate(resolve))
    const box = p.$('review-thread-1').querySelector('textarea[data-thread]')
    box.value = 'my reply'
    box.dispatchEvent(new p.window.Event('input', { bubbles: true }))
    box.dispatchEvent(new p.window.KeyboardEvent('keydown', { key: 'Enter', ctrlKey: true, bubbles: true }))
    for (let i = 0; i < 20; i++) await new Promise((resolve) => setImmediate(resolve))
    answerPoll()
    await pollGone
    await p.settle()
    assert.ok(p.$('review-thread-1').textContent.includes('my reply'), 'the reply is still shown')
  } finally { p.close() }
})

test('a reply sent twice at once (Ctrl+Enter held) is posted once', async () => {
  const p = await reviewPage({ review: commented, diff: twoFiles })
  try {
    await p.click('tab-review')
    const box = p.$('review-thread-1').querySelector('textarea[data-thread]')
    box.focus()
    await p.type(box, 'once')
    box.dispatchEvent(new p.window.KeyboardEvent('keydown', { key: 'Enter', ctrlKey: true, bubbles: true }))
    box.dispatchEvent(new p.window.KeyboardEvent('keydown', { key: 'Enter', ctrlKey: true, bubbles: true }))
    await p.settle()
    assert.equal(p.posted('/api/review/do').length, 1)
  } finally { p.close() }
})

test("a Viewed tick vibe couldn't keep is unticked again", async () => {
  const p = await openPage({
    routes: {
      '/api/state': { sandbox: 'box', sandboxTabs: true, review: true, conversation: true, reviewAt: '', attention: {} },
      '/api/review': { review: started, diff: twoFiles },
      '/api/log': { entries: [] },
    },
    storage: { 'vibe-companion-tab': 'review' },
  })
  try {
    p.window.fetch = ((real) => async (url, options) => (String(url).includes('/api/review/do')
      ? new Response(JSON.stringify({ error: 'disk full' }), { status: 500, headers: { 'Content-Type': 'application/json' } })
      : real(url, options)))(p.window.fetch)
    const tick = p.$('review-files').querySelector('.review-viewed input')
    tick.checked = true
    tick.dispatchEvent(new p.window.Event('change', { bubbles: true }))
    await p.settle()
    assert.equal(p.$('review-files').querySelector('.review-viewed input').checked, false)
  } finally { p.close() }
})

test("an untracked file that couldn't be read says so", async () => {
  const unreadable = [{ path: 'gone.txt', status: 'added', unreadable: 'no such file or directory', hunks: [] }]
  const p = await reviewPage({ review: started, diff: unreadable })
  try {
    await p.click('tab-review')
    assert.match(p.$('review-files').textContent, /couldn't be read: no such file or directory/)
  } finally { p.close() }
})

// --- comments named in the conversation -----------------------------------------------

// talkPage is the Conversation tab open on what the agent said, with a
// review whose threads are commented's.
async function talkPage(text) {
  const p = await openPage({
    routes: {
      '/api/state': { sandbox: 'box', sandboxTabs: true, review: true, conversation: true, reviewAt: '', attention: {} },
      '/api/review': { review: { ...commented, started: '2026-10-16T10:00:00Z' }, diff: twoFiles },
      '/api/conversation': () => ({ events: [{ seq: 1, kind: 'user', text: 'please fix' }, agentSaid(2, text)], last: 2, running: false }),
      '/api/log': { entries: [] },
    },
    storage: { 'vibe-companion-tab': 'conversation' },
  })
  await p.every('pollConversation')
  return p
}
const refsIn = (p) => Array.from(p.$('convo-log').querySelectorAll('.thread-ref'))

test('a comment the agent names, #2, takes you to it on the Review tab', async () => {
  const p = await talkPage('Fixed #2, and see #3.')
  try {
    assert.deepEqual(refsIn(p).map((r) => r.textContent), ['#2', '#3'])
    const shown = []
    p.window.Element.prototype.scrollIntoView = function () { shown.push(this.id) }
    await p.click(refsIn(p)[0])
    assert.equal(p.$('tab-review').getAttribute('aria-selected'), 'true')
    assert.deepEqual(shown, ['review-thread-2'])
    assert.equal(p.window.document.activeElement.dataset.thread, '2', 'its reply box ready')
  } finally { p.close() }
})

test("a comment that isn't on the review says so", async () => {
  const p = await talkPage('Fixed #9.')
  try {
    await p.click(refsIn(p)[0])
    assert.match(p.$('toast').textContent, /no comment #9/)
  } finally { p.close() }
})

test('a number in code, or in a word, is left alone', async () => {
  const p = await talkPage('Run `git show #2`, see issue#3 and C#4.')
  try {
    assert.deepEqual(refsIn(p), [])
  } finally { p.close() }
})

// --- leaving whitespace changes out ---------------------------------------------------

test('ticking "Hide whitespace changes" loads the diff without them, and is kept', async () => {
  const now = { diff: twoFiles }
  const p = await changingPage(now)
  try {
    const tick = p.$('review-whitespace')
    assert.equal(tick.checked, false, 'off unless ticked')
    tick.checked = true
    tick.dispatchEvent(new p.window.Event('change', { bubbles: true }))
    await p.settle()
    const last = p.calls.filter((c) => c.path === '/api/review').pop()
    assert.match(last.query, /diff=1/)
    assert.match(last.query, /whitespace=ignore/)
    assert.equal(p.window.localStorage.getItem('vibe-review-whitespace'), 'ignore')
  } finally { p.close() }
})

test('a page opened with whitespace hidden asks for the diff so', async () => {
  const p = await reviewPage({ review: started, diff: twoFiles, storage: { 'vibe-review-whitespace': 'ignore' } })
  try {
    await p.click('tab-review')
    assert.equal(p.$('review-whitespace').checked, true)
    const asked = p.calls.filter((c) => c.path === '/api/review' && c.query.includes('diff=1'))
    assert.ok(asked.length && asked.every((c) => c.query.includes('whitespace=ignore')), asked.map((c) => c.query).join(' '))
  } finally { p.close() }
})

// --- the second review's findings on the page ---------------------------------------

// slowDiffPage is a review whose diff loads answer only when let go:
// release() answers the oldest waiting one.
async function slowDiffPage() {
  const waiting = []
  let diffLoads = 0
  const p = await openPage({
    routes: {
      '/api/state': { sandbox: 'box', sandboxTabs: true, review: true, conversation: true, reviewAt: '', attention: {} },
      '/api/review': (call) => {
        if (!call.query.includes('diff=1')) return { review: started }
        diffLoads++
        return new Promise((resolve) => waiting.push(() => resolve({ review: started, diff: twoFiles })))
      },
      '/api/log': { entries: [] },
    },
    storage: { 'vibe-companion-tab': 'log' },
  })
  const turns = async (n = 20) => { for (let i = 0; i < n; i++) await new Promise((resolve) => setImmediate(resolve)) }
  return Object.assign(p, { waiting, diffLoads: () => diffLoads, turns,
    release: async () => { waiting.shift()(); await turns() } })
}

test('a poll while the diff is on its way asks for it no second time', async () => {
  const p = await slowDiffPage()
  try {
    p.$('tab-review').click()
    await p.turns()
    assert.equal(p.diffLoads(), 1)
    p.every('tick').catch(() => {})
    await p.turns()
    await p.release()
    await p.turns()
    assert.equal(p.diffLoads(), 1, 'one diff load')
  } finally { p.close() }
})

test('Refresh while the diff is on its way loads it again once that one is in', async () => {
  const p = await slowDiffPage()
  try {
    p.$('tab-review').click()
    await p.turns()
    p.$('review-refresh').click()
    await p.turns()
    await p.release()
    await p.turns()
    assert.equal(p.diffLoads(), 2, 'the stale one followed by a fresh one')
    await p.release()
  } finally { p.close() }
})

test("a comment named on a closed review says the review's closed", async () => {
  const p = await openPage({
    routes: {
      '/api/state': { sandbox: 'box', sandboxTabs: true, review: true, conversation: true, reviewAt: '', attention: {} },
      '/api/review': { review: { ...commented, started: '2026-10-16T10:00:00Z', closed: 'You closed it.' }, diff: twoFiles },
      '/api/conversation': () => ({ events: [agentSaid(1, 'Fixed #2.')], last: 1, running: false }),
      '/api/log': { entries: [] },
    },
    storage: { 'vibe-companion-tab': 'conversation' },
  })
  try {
    await p.every('pollConversation')
    await p.click(p.$('convo-log').querySelector('.thread-ref'))
    assert.match(p.$('toast').textContent, /closed/)
    assert.doesNotMatch(p.$('toast').textContent, /viewed/)
  } finally { p.close() }
})

test('with an edit open, the empty comment box keeps the focus through a redraw', async () => {
  const mine = { lastId: 1, started: '2026-10-16T10:00:00Z', threads: [{ id: 1, status: 'open', comments: [{ author: 'user', body: 'first', at: '2026-10-16T10:05:00Z' }] }] }
  const now = { diff: twoFiles, review: mine }
  const p = await changingPage(now)
  try {
    await p.click(p.button('Edit', p.$('review-thread-1')))
    await p.click(p.$('review-files').querySelector('[aria-label="Comment on line R2"]'))
    const box = () => p.window.document.querySelector('textarea[aria-label="Your comment"]')
    box().focus()
    now.review = { ...mine, threads: [...mine.threads, { id: 2, status: 'open', comments: said('news') }], lastId: 2 }
    await p.poll()
    assert.equal(p.window.document.activeElement, box(), 'the comment box, not the edit box')
  } finally { p.close() }
})

test('a deleted comment stays deleted when a poll set out before it lands', async () => {
  const mine = { lastId: 1, started: '2026-10-16T10:00:00Z', threads: [{ id: 1, status: 'open', comments: [{ author: 'user', body: 'oops', at: '2026-10-16T10:05:00Z' }] }] }
  const gone = { lastId: 1, started: '2026-10-16T10:00:00Z', threads: [] }
  let answerPoll
  let slow = false
  const p = await openPage({
    routes: {
      '/api/state': { sandbox: 'box', sandboxTabs: true, review: true, conversation: true, reviewAt: '', attention: {} },
      '/api/review': () => (!slow ? { review: mine, diff: [] } : new Promise((resolve) => { answerPoll = () => resolve({ review: mine }) })),
      '/api/review/do': () => ({ review: gone }),
      '/api/log': { entries: [] },
    },
    storage: { 'vibe-companion-tab': 'review' },
  })
  try {
    slow = true
    const pollGone = p.poll().catch(() => {})
    await new Promise((resolve) => setImmediate(resolve))
    p.button('Delete', p.$('review-thread-1')).click()
    p.$('confirm').close('run')
    for (let i = 0; i < 20; i++) await new Promise((resolve) => setImmediate(resolve))
    answerPoll()
    await pollGone
    await p.settle()
    assert.equal(p.$('review-thread-1'), null, 'still gone')
  } finally { p.close() }
})

test('a comment named as the agent wrote it is shown so, and only a plain #N', async () => {
  const p = await talkPage('See #007, not #1.5; [see #3](http://localhost:1/x); and example.com/#4.')
  try {
    assert.deepEqual(refsIn(p).map((r) => r.textContent), ['#007'])
    assert.equal(p.$('convo-log').querySelector('a .thread-ref'), null, 'none inside a link')
  } finally { p.close() }
})

test("a comment asked for when vibe can't be reached isn't gone to later, out of the blue", async () => {
  const p = await talkPage('Fixed #2.')
  try {
    const real = p.window.fetch
    p.window.fetch = async (url, options) => (String(url).includes('/api/review') ? Promise.reject(new Error('down')) : real(url, options))
    await p.click(refsIn(p)[0])
    p.window.fetch = real
    await p.click('tab-conversation')
    const shown = []
    p.window.Element.prototype.scrollIntoView = function () { shown.push(this.id) }
    await p.click('tab-review')
    assert.deepEqual(shown, [], 'not scrolled to #2')
  } finally { p.close() }
})

test('a long file shown stays shown through a poll, with no comment being written', async () => {
  const now = { diff: longFile }
  const p = await changingPage(now)
  try {
    await p.click(p.button('Show 700 lines', p.$('review-files')))
    now.review = { ...started, threads: [{ id: 1, status: 'open', comments: said('news') }], lastId: 1 }
    await p.poll()
    assert.equal(p.button('Show 700 lines', p.$('review-files')), undefined, 'not folded again')
  } finally { p.close() }
})

// Every change to the review goes through doReview, which takes the review
// vibe gives back: a poll that set out before can't undo it.
test('the review is changed from one place only', () => {
  const src = require('node:fs').readFileSync(require('node:path').join(__dirname, '..', 'ui', 'app.js'), 'utf8')
  // Every call that changes the review (do, ask, submit, start) goes
  // through changeReview, which takes the review vibe gives back.
  const calls = src.match(/[\w.]+\(\s*'\/api\/review\/(do|ask|submit|start)'/g) || []
  assert.ok(calls.length >= 4, calls.join(' | '))
  for (const call of calls) assert.match(call, /^changeReview\(/, call)
  assert.equal(src.split('await post(path, body)').length - 1, 1, 'changeReview posts')
})

test("a prompt never given to the agent says so, not that it was sent", async () => {
  const p = await convoPage({ events: [{ seq: 1, kind: 'user', text: 'review it', receipt: 'undelivered' }, { seq: 2, kind: 'error', text: "the review couldn't be started" }], running: false })
  try {
    const ticks = p.$('convo-log').querySelector('.ticks')
    assert.equal(ticks.textContent, '✕')
    assert.match(ticks.title, /not given to the agent/i)
  } finally { p.close() }
})

test('a question asked stays shown when a poll that set out before it lands', async () => {
  const before = { lastId: 1, started: '2026-10-16T10:00:00Z', threads: [{ id: 1, status: 'open', comments: said('why?') }] }
  const after = { ...before, threads: [{ id: 1, status: 'open', comments: [...said('why?'), { author: 'user', body: 'my question', at: '2026-10-16T10:07:00Z', sent: true }] }] }
  let answerPoll
  let slow = false
  const p = await openPage({
    routes: {
      '/api/state': { sandbox: 'box', sandboxTabs: true, review: true, conversation: true, reviewAt: '', attention: {} },
      '/api/review': () => (!slow ? { review: before, diff: [] } : new Promise((resolve) => { answerPoll = () => resolve({ review: before }) })),
      '/api/review/ask': () => ({ ok: true, review: after }),
      '/api/log': { entries: [] },
    },
    storage: { 'vibe-companion-tab': 'review' },
  })
  try {
    slow = true
    const pollGone = p.poll().catch(() => {})
    await new Promise((resolve) => setImmediate(resolve))
    const box = p.$('review-thread-1').querySelector('textarea[data-thread]')
    box.value = 'my question'
    box.dispatchEvent(new p.window.Event('input', { bubbles: true }))
    p.button('Ask the agent', p.$('review-thread-1')).click()
    for (let i = 0; i < 20; i++) await new Promise((resolve) => setImmediate(resolve))
    answerPoll()
    await pollGone
    await p.settle()
    assert.ok(p.$('review-thread-1').textContent.includes('my question'), 'still shown')
  } finally { p.close() }
})

test('a comment named in bold link text is no button in the link either', async () => {
  const p = await talkPage('[**see #3**](http://localhost:1/x) and [_#2_](http://localhost:1/y)')
  try {
    assert.equal(p.$('convo-log').querySelector('a .thread-ref'), null)
  } finally { p.close() }
})

test('a diff that set out before Refresh is not shown when it lands: the fresh one is', async () => {
  const waiting = []
  let loads = 0
  const p = await openPage({
    routes: {
      '/api/state': { sandbox: 'box', sandboxTabs: true, review: true, conversation: true, reviewAt: '', attention: {} },
      '/api/review': (call) => {
        if (!call.query.includes('diff=1')) return { review: started }
        const n = ++loads
        return new Promise((resolve) => waiting.push(() => resolve({ review: started, diff: oneFile('load ' + n) })))
      },
      '/api/log': { entries: [] },
    },
    storage: { 'vibe-companion-tab': 'log' },
  })
  const turns = async () => { for (let i = 0; i < 20; i++) await new Promise((resolve) => setImmediate(resolve)) }
  try {
    p.$('tab-review').click()
    await turns()
    p.$('review-refresh').click()
    await turns()
    waiting.shift()()
    await turns()
    assert.ok(!codeShown(p).includes('load 1'), 'the stale diff was drawn')
    waiting.shift()()
    await turns()
    assert.ok(codeShown(p).includes('load 2'), codeShown(p).join('|'))
  } finally { p.close() }
})

// A review that can't be read is said to be on the tab, with a way out:
// start afresh, the old one kept.
test('an unreadable review offers to start afresh, and does, asking first', async () => {
  let broken = true
  const p = await openPage({
    routes: {
      '/api/state': { sandbox: 'box', sandboxTabs: true, review: true, conversation: true, reviewAt: '', attention: {} },
      '/api/review': () => (broken
        ? { status: 500, body: { error: "the review in review.json can't be read", code: 'review-unreadable' } }
        : { review: started, diff: [] }),
      '/api/review/afresh': () => { broken = false; return { review: started } },
      '/api/review/options': { uncommitted: true },
      '/api/log': { entries: [] },
    },
    storage: { 'vibe-companion-tab': 'review' },
  })
  try {
    assert.equal(p.$('review-broken').hidden, false)
    assert.match(p.$('review-broken').textContent, /can't be read/)
    await p.click(p.button('Start afresh', p.$('review-broken')))
    assert.ok(p.$('confirm').open, 'asked first')
    await p.confirm(true)
    assert.deepEqual(p.posted('/api/review/afresh'), [{}])
    assert.equal(p.$('review-broken').hidden, true)
  } finally { p.close() }
})

// held is an answer from vibe that's on its way until the test sends it.
function held() {
  let send
  const answer = new Promise((resolve) => { send = resolve })
  return { route: () => answer, send }
}

// turns lets the page get on with what it has, while answers are held back.
async function turns(n = 20) {
  for (let i = 0; i < n; i++) await new Promise((resolve) => setImmediate(resolve))
}

test('the page says it is starting until the tab it opens on has its data', async () => {
  const state = held()
  const log = held()
  const p = await openPage({ routes: { '/api/state': state.route, '/api/log': log.route }, storage: { 'vibe-companion-tab': 'log' }, settled: false })
  try {
    await turns()
    assert.equal(p.$('starting').hidden, false, 'shown from the start')
    state.send({ sandbox: 'box', sandboxTabs: true, attention: {} })
    await turns()
    assert.equal(p.$('starting').hidden, false, "the tabs are known, but the log isn't there yet")
    log.send({ entries: [] })
    await p.settle()
    assert.equal(p.$('starting').hidden, true)
  } finally { p.close() }
})

test('opening on the Conversation tab, the page is starting until the conversation is there', async () => {
  const p = await openPage({
    routes: { '/api/state': { sandbox: 'box', sandboxTabs: true, conversation: true, attention: {} }, '/api/conversation': { events: [], last: 0 } },
    storage: { 'vibe-companion-tab': 'conversation' },
  })
  try {
    assert.equal(p.$('panel-conversation').hidden, false)
    assert.equal(p.$('starting').hidden, false)
    await p.every('pollConversation')
    assert.equal(p.$('starting').hidden, true)
  } finally { p.close() }
})

test("an error from vibe as the page starts isn't hidden behind Starting", async () => {
  const p = await openPage({ routes: { '/api/state': { status: 500, body: { error: 'something broke' } } } })
  try {
    assert.equal(p.$('starting').hidden, true)
  } finally { p.close() }
})

test('with vibe not there, Disconnected takes over from Starting', async () => {
  const p = await openPage({ routes: { '/api/state': () => { throw new Error('refused') }, '/api/log': () => { throw new Error('refused') } } })
  try {
    assert.equal(p.$('offline').hidden, false)
    assert.equal(p.$('starting').hidden, true)
  } finally { p.close() }
})

// logPage opens on the network log, which answers with log (a body or a
// function of the call), with a conversation that answers fine.
function logPage(log) {
  return openPage({
    routes: {
      '/api/state': { sandbox: 'box', sandboxTabs: true, conversation: true, attention: {} },
      '/api/log': log,
      '/api/rules': { rules: [] },
      '/api/conversation': { events: [], last: 0 },
    },
    storage: { 'vibe-companion-tab': 'log' },
  })
}

const broken = { status: 500, body: { error: 'the log broke' } }

test("the log's error stays up while the conversation is polled", async () => {
  const p = await logPage(broken)
  try {
    assert.equal(p.$('banner').textContent, 'the log broke')
    await p.every('pollConversation')
    assert.equal(p.$('banner').hidden, false)
    assert.equal(p.$('banner').textContent, 'the log broke')
  } finally { p.close() }
})

test("the log's error goes once the log loads", async () => {
  let answer = broken
  const p = await logPage(() => answer)
  try {
    assert.equal(p.$('banner').hidden, false)
    answer = { entries: [] }
    await p.poll()
    assert.equal(p.$('banner').hidden, true)
  } finally { p.close() }
})

test("a tab's error is shown on that tab, not on the others", async () => {
  const p = await logPage(broken)
  try {
    await p.click('tab-rules')
    assert.equal(p.$('banner').hidden, true, 'on the rules tab')
    await p.click('tab-log')
    assert.equal(p.$('banner').textContent, 'the log broke')
  } finally { p.close() }
})

test("an error from the conversation is shown on every tab", async () => {
  const p = await openPage({
    routes: {
      '/api/state': { sandbox: 'box', sandboxTabs: true, conversation: true, attention: {} },
      '/api/log': { entries: [] },
      '/api/conversation': { status: 500, body: { error: 'the conversation broke' } },
    },
    storage: { 'vibe-companion-tab': 'log' },
  })
  try {
    await p.every('pollConversation')
    assert.equal(p.$('banner').textContent, 'the conversation broke')
    await p.poll()
    assert.equal(p.$('banner').textContent, 'the conversation broke', "the log loading fine doesn't clear it")
  } finally { p.close() }
})

test("the log says when it couldn't be checked against the rules", async () => {
  const p = await logPage({ entries: [], unresolved: "The rules couldn't be read" })
  try {
    assert.equal(p.$('banner').textContent, "The rules couldn't be read")
  } finally { p.close() }
})

// --- "New conversation" ---------------------------------------------------------------------

// freshPage is a page whose "New conversation" dialog is offered options;
// fetched is what checking origin says, and answer what starting anew does.
// A function for options or fetched answers when the test says (a promise).
// A call left unanswered keeps the page from settling: such a test clicks
// with clickNow, which lets the page run a few turns (see turns).
const clickNow = async (p, id) => { p.$(id).click(); await turns() }

async function freshPage({ options = { review: false }, fetched = { checked: true, local: true, ahead: 0, behind: 0 }, answer = { ok: true }, open = true } = {}) {
  const p = await openPage({
    routes: {
      '/api/state': { sandbox: 'box', sandboxTabs: true, conversation: true, attention: {} },
      '/api/conversation': { events: [], last: 0, running: false },
      '/api/conversation/new/options': typeof options === 'function' ? options : () => options,
      '/api/conversation/new/fetch': typeof fetched === 'function' ? fetched : () => fetched,
      '/api/conversation/new': () => answer,
      '/api/log': { entries: [] },
    },
    storage: { 'vibe-companion-tab': 'conversation' },
  })
  if (open) await p.click('convo-new')
  return p
}

const shown = (p, id) => { const n = p.$(id); return !!n && !n.hidden && !n.closest('[hidden]') }
const clean = { current: 'feat/a', dirty: false, conflicted: false, default: 'master' }

test('New conversation asks first, offering only what applies', async () => {
  const p = await freshPage()
  try {
    assert.ok(p.$('fresh').open)
    assert.ok(!shown(p, 'fresh-clipboard-row'), 'no clipboard')
    assert.ok(!shown(p, 'fresh-review-row'), 'no review')
    assert.ok(!shown(p, 'fresh-branch-row'), 'no repository')
    await p.click('fresh-run')
    assert.deepEqual(p.posted('/api/conversation/new'), [{ clipboard: '', review: false, branch: null }])
    assert.ok(!p.$('fresh').open)
  } finally { p.close() }
})

test('cancelling starts nothing', async () => {
  const p = await freshPage()
  try {
    p.$('fresh').close('cancel')
    await p.settle()
    assert.deepEqual(p.posted('/api/conversation/new'), [])
  } finally { p.close() }
})

test('the clipboard and the review are ticked to be cleared and closed', async () => {
  const p = await freshPage({ options: { review: true, clipboard: { sent: 2, unsent: 1 } } })
  try {
    assert.ok(shown(p, 'fresh-clipboard-row') && p.$('fresh-clipboard').checked)
    assert.ok(shown(p, 'fresh-unsent-row') && p.$('fresh-unsent').checked, 'unsent items, so asked about')
    assert.match(p.$('fresh-unsent-row').textContent, /1 /)
    assert.ok(shown(p, 'fresh-review-row') && p.$('fresh-review').checked)
    await p.click('fresh-run')
    assert.deepEqual(p.posted('/api/conversation/new'), [{ clipboard: 'all', review: true, branch: null }])
  } finally { p.close() }
})

test('unsent items are kept when that box is unticked', async () => {
  const p = await freshPage({ options: { clipboard: { sent: 2, unsent: 1 } } })
  try {
    await p.click('fresh-unsent')
    await p.click('fresh-run')
    assert.equal(p.posted('/api/conversation/new')[0].clipboard, 'sent')
  } finally { p.close() }
})

test('with nothing unsent, only the sent items are offered for clearing', async () => {
  const p = await freshPage({ options: { clipboard: { sent: 2, unsent: 0 } } })
  try {
    assert.ok(shown(p, 'fresh-clipboard-row'))
    assert.ok(!shown(p, 'fresh-unsent-row'))
    await p.click('fresh-run')
    assert.equal(p.posted('/api/conversation/new')[0].clipboard, 'sent')
  } finally { p.close() }
})

test('an empty clipboard is not offered for clearing', async () => {
  const p = await freshPage({ options: { clipboard: { sent: 0, unsent: 0 } } })
  try {
    assert.ok(!shown(p, 'fresh-clipboard-row'))
  } finally { p.close() }
})

test('the dialog waits for the sandbox before it can start, saying so', async () => {
  let answer
  const p = await freshPage({ options: () => new Promise((resolve) => { answer = resolve }), open: false })
  try {
    await clickNow(p, 'convo-new')
    assert.ok(p.$('fresh-run').disabled)
    assert.ok(shown(p, 'fresh-loading'))
    assert.match(p.$('fresh-loading').textContent, /sandbox/)
    answer({ review: true })
    await p.settle()
    assert.ok(!p.$('fresh-run').disabled)
    assert.ok(!shown(p, 'fresh-loading'))
  } finally { p.close() }
})

test("what the sandbox can't do is said, not offered", async () => {
  const why = 'The sandbox was stopped, so it is left stopped.'
  const p = await freshPage({ options: { clipboard: { sent: 0, unsent: 0, unavailable: why }, branch: { unavailable: why } } })
  try {
    assert.ok(shown(p, 'fresh-clipboard-row') && p.$('fresh-clipboard').disabled && !p.$('fresh-clipboard').checked)
    assert.ok(shown(p, 'fresh-branch-row') && p.$('fresh-branch').disabled)
    assert.ok(p.$('fresh').textContent.includes(why))
    await p.click('fresh-run')
    assert.deepEqual(p.posted('/api/conversation/new'), [{ clipboard: '', review: false, branch: null }])
  } finally { p.close() }
})

test('a new branch is unticked; ticking it offers a name starting feat/, with the cursor at its end', async () => {
  const p = await freshPage({ options: { branch: clean } })
  try {
    assert.ok(shown(p, 'fresh-branch-row') && !p.$('fresh-branch').checked)
    assert.ok(!shown(p, 'fresh-branch-details'))
    await p.click('fresh-branch')
    assert.ok(shown(p, 'fresh-branch-details'))
    const name = p.$('fresh-branch-name')
    assert.equal(name.value, 'feat/')
    assert.equal(p.window.document.activeElement, name)
    assert.equal(name.selectionStart, 5)
    assert.equal(name.selectionEnd, 5)
    assert.ok(p.$('fresh-from-here').checked, 'from here by default')
    assert.match(p.$('fresh-from-default-row').textContent, /origin\/master/)
  } finally { p.close() }
})

test('the new branch is asked for as named, from where it was chosen', async () => {
  const p = await freshPage({ options: { branch: clean } })
  try {
    await p.click('fresh-branch')
    await p.type(p.$('fresh-branch-name'), 'feat/thing')
    await p.click('fresh-from-default')
    await p.click('fresh-run')
    assert.deepEqual(p.posted('/api/conversation/new')[0].branch, { name: 'feat/thing', from: 'default', update: false })
  } finally { p.close() }
})

test('on the default branch, branching from it is not offered, and origin is not asked', async () => {
  const p = await freshPage({ options: { branch: Object.assign({}, clean, { current: 'master' }) } })
  try {
    await p.click('fresh-branch')
    assert.ok(!shown(p, 'fresh-from-default-row'))
    assert.deepEqual(p.posted('/api/conversation/new/fetch'), [])
  } finally { p.close() }
})

test("without origin/HEAD the default branch isn't guessed: the fix is given", async () => {
  const p = await freshPage({ options: { branch: { current: 'feat/a', noDefault: 'origin/HEAD isn\'t set. `git remote set-head origin --auto` sets it.' } } })
  try {
    await p.click('fresh-branch')
    assert.ok(!shown(p, 'fresh-from-default-row'))
    assert.ok(p.$('fresh-branch-details').textContent.includes('git remote set-head origin --auto'))
    assert.deepEqual(p.posted('/api/conversation/new/fetch'), [])
  } finally { p.close() }
})

test('a dirty tree: warned that the changes come along, and branching from the default branch is disabled', async () => {
  const p = await freshPage({ options: { branch: Object.assign({}, clean, { dirty: true }) } })
  try {
    await p.click('fresh-branch')
    assert.ok(shown(p, 'fresh-dirty'))
    assert.match(p.$('fresh-dirty').textContent, /come with you/)
    assert.ok(p.$('fresh-from-default').disabled)
    assert.match(p.$('fresh-from-default-row').textContent, /uncommitted changes/)
  } finally { p.close() }
})

test('a clean tree: no warning', async () => {
  const p = await freshPage({ options: { branch: clean } })
  try {
    await p.click('fresh-branch')
    assert.ok(!shown(p, 'fresh-dirty'))
    assert.ok(!p.$('fresh-from-default').disabled)
  } finally { p.close() }
})

test('ticking a new branch checks origin once, saying so while it does', async () => {
  let answer
  const p = await freshPage({ options: { branch: clean }, fetched: () => new Promise((resolve) => { answer = resolve }) })
  try {
    await clickNow(p, 'fresh-branch')
    assert.match(p.$('fresh-fetch').textContent, /Checking whether origin\/master is up to date/)
    answer({ checked: true, local: true, ahead: 0, behind: 0 })
    await p.settle()
    await p.click('fresh-branch')
    await p.click('fresh-branch')
    assert.equal(p.posted('/api/conversation/new/fetch').length, 1)
    assert.ok(!shown(p, 'fresh-fetch'), 'up to date: nothing to say')
    assert.ok(!shown(p, 'fresh-update-row'))
  } finally { p.close() }
})

test("a check that couldn't be made says so, and why, and no more", async () => {
  const p = await freshPage({ options: { branch: clean }, fetched: { checked: false, why: 'could not read Username' } })
  try {
    await p.click('fresh-branch')
    assert.ok(shown(p, 'fresh-fetch'))
    assert.match(p.$('fresh-fetch').textContent, /Couldn't check whether origin\/master is up to date: could not read Username/)
    assert.match(p.$('fresh-fetch').textContent, /could not read Username\.$/)
    assert.ok(!shown(p, 'fresh-update-row'))
  } finally { p.close() }
})

test('a local default branch behind origin is offered, ticked, for a fast-forward', async () => {
  const p = await freshPage({ options: { branch: clean }, fetched: { checked: true, local: true, ahead: 0, behind: 3 } })
  try {
    await p.click('fresh-branch')
    assert.ok(shown(p, 'fresh-update-row') && p.$('fresh-update').checked)
    assert.match(p.$('fresh-update-row').textContent, /master is 3 commits behind origin/)
    await p.type(p.$('fresh-branch-name'), 'feat/x')
    await p.click('fresh-run')
    assert.deepEqual(p.posted('/api/conversation/new')[0].branch, { name: 'feat/x', from: 'here', update: true })
  } finally { p.close() }
})

test('a local default branch with its own commits is left alone, saying so', async () => {
  const p = await freshPage({ options: { branch: clean }, fetched: { checked: true, local: true, ahead: 2, behind: 1 } })
  try {
    await p.click('fresh-branch')
    assert.ok(!shown(p, 'fresh-update-row'))
    assert.ok(shown(p, 'fresh-fetch'))
    assert.match(p.$('fresh-fetch').textContent, /master has 2 commits not on origin/)
  } finally { p.close() }
})

test('a refusal keeps the dialog open, saying why', async () => {
  const p = await freshPage({ options: { branch: clean }, answer: { status: 400, body: { error: 'There\'s already a branch called feat/x.' } } })
  try {
    await p.click('fresh-branch')
    await p.type(p.$('fresh-branch-name'), 'feat/x')
    await p.click('fresh-run')
    assert.ok(p.$('fresh').open)
    assert.ok(shown(p, 'fresh-error'))
    assert.match(p.$('fresh-error').textContent, /already a branch called feat\/x/)
  } finally { p.close() }
})

test('a branch with no name is asked for before anything is sent', async () => {
  const p = await freshPage({ options: { branch: clean } })
  try {
    await p.click('fresh-branch')
    await p.type(p.$('fresh-branch-name'), '  ')
    await p.click('fresh-run')
    assert.deepEqual(p.posted('/api/conversation/new'), [])
    assert.ok(shown(p, 'fresh-error'))
  } finally { p.close() }
})

test('opening it again starts from the defaults', async () => {
  const p = await freshPage({ options: { review: true, branch: clean } })
  try {
    await p.click('fresh-review')
    await p.click('fresh-branch')
    await p.type(p.$('fresh-branch-name'), 'feat/x')
    p.$('fresh').close('cancel')
    await p.settle()
    await p.click('convo-new')
    assert.ok(p.$('fresh-review').checked)
    assert.ok(!p.$('fresh-branch').checked)
    assert.ok(!shown(p, 'fresh-branch-details'))
    assert.equal(p.$('fresh-branch-name').value, 'feat/')
  } finally { p.close() }
})

// More of the form follows the name, so Enter there would start before the
// rest was seen; and the form's first button, which Enter would otherwise
// press, is Cancel.
test('Enter in the branch name neither starts nor cancels', async () => {
  const p = await freshPage({ options: { branch: clean } })
  try {
    await p.click('fresh-branch')
    const name = p.$('fresh-branch-name')
    const enter = new p.window.KeyboardEvent('keydown', { key: 'Enter', bubbles: true, cancelable: true })
    name.dispatchEvent(enter)
    await p.settle()
    assert.ok(enter.defaultPrevented, "the browser's own Enter (pressing Cancel) is stopped")
    assert.deepEqual(p.posted('/api/conversation/new'), [])
    assert.ok(p.$('fresh').open)
  } finally { p.close() }
})

test("with no default branch to review against, the Review tab says why", async () => {
  const why = "Can't tell the default branch: origin/HEAD isn't set. `git remote set-head origin --auto` sets it."
  const p = await startPage({ options: { uncommitted: true, noDefaultBranch: why } })
  try {
    assert.deepEqual(startButtons(p), ['Review uncommitted changes', 'Show uncommitted changes', 'Set'])
    assert.ok(p.$('review-start-options').textContent.includes(why))
  } finally { p.close() }
})

test('with nothing to review and no default branch, the Review tab says both', async () => {
  const why = "Can't tell the default branch: there's no origin remote."
  const p = await startPage({ options: { noDefaultBranch: why } })
  try {
    assert.deepEqual(startButtons(p), ['Set'])
    assert.match(p.$('review-start-options').textContent, /Nothing to review/)
    assert.ok(p.$('review-start-options').textContent.includes(why))
  } finally { p.close() }
})

test('a default branch to review against needs no explaining', async () => {
  const p = await startPage()
  try {
    assert.ok(!p.$('review-start-options').textContent.includes("Can't tell the default branch"))
  } finally { p.close() }
})

test('with no default branch, the Review tab offers to set one, suggesting the name last set', async () => {
  const why = "Can't tell the default branch: there's no origin remote."
  const options = { current: { uncommitted: true, noDefaultBranch: why } }
  const p = await startPage({
    options: () => options.current,
    defaultBranch: (call) => {
      if (call.method === 'POST') { options.current = { uncommitted: true, defaultBranch: call.body.name }; return { name: call.body.name, set: true, preferred: call.body.name } }
      return { why, branches: ['feat', 'trunk'], preferred: 'master' }
    },
  })
  try {
    const name = p.$('review-start-options').querySelector('.default-setter input')
    assert.equal(name.value, 'master')
    const suggested = Array.from(p.$('review-start-options').querySelectorAll('datalist option')).map((o) => o.value)
    assert.deepEqual(suggested, ['feat', 'trunk'])
    await p.type(name, 'trunk')
    await p.click(p.button('Set', p.$('review-start-options')))
    assert.deepEqual(p.posted('/api/default-branch'), [{ name: 'trunk' }])
    assert.deepEqual(startButtons(p), ['Review uncommitted changes', 'Review against trunk', 'Show uncommitted changes', 'Diff against trunk'])
  } finally { p.close() }
})

test('a default branch that cannot be set says why, keeping what was typed', async () => {
  const p = await startPage({
    options: { uncommitted: true, noDefaultBranch: "Can't tell the default branch: there's no origin remote." },
    defaultBranch: (call) => (call.method === 'POST' ? { status: 400, body: { error: '"a b" isn\'t a branch name' } } : { why: 'x', preferred: '' }),
  })
  try {
    const name = p.$('review-start-options').querySelector('.default-setter input')
    await p.type(name, 'a b')
    await p.click(p.button('Refresh'))
    assert.equal(p.$('review-start-options').querySelector('.default-setter input').value, 'a b', 'kept across a redraw')
    await p.click(p.button('Set', p.$('review-start-options')))
    assert.match(p.$('review-start-options').textContent, /isn't a branch name/)
    assert.equal(p.$('review-start-options').querySelector('.default-setter input').value, 'a b')
  } finally { p.close() }
})

test('a default branch with no commits yet: reviewing against it is shown, but not offered, saying why', async () => {
  const p = await startPage({ options: { uncommitted: true, emptyDefaultBranch: 'master' } })
  try {
    const against = p.button('Review against master')
    assert.ok(against && against.disabled)
    assert.ok(p.button('Diff against master').disabled)
    assert.match(p.$('review-start-options').textContent, /master has no commits yet/)
  } finally { p.close() }
})

test('with no default branch, the dialog offers to set one; then branching from it is offered, as it is here', async () => {
  const noDefault = { current: 'feat/a', dirty: false, noDefault: "Can't tell the default branch: there's no origin remote." }
  const options = { current: { branch: noDefault } }
  const p = await openPage({
    routes: {
      '/api/state': { sandbox: 'box', sandboxTabs: true, conversation: true, attention: {} },
      '/api/conversation': { events: [], last: 0, running: false },
      '/api/conversation/new/options': () => options.current,
      '/api/default-branch': (call) => {
        if (call.method !== 'POST') return { why: noDefault.noDefault, preferred: 'master' }
        options.current = { branch: { current: 'feat/a', dirty: false, default: call.body.name, defaultSet: true } }
        return { name: call.body.name, set: true, preferred: call.body.name }
      },
      '/api/conversation/new': () => ({ ok: true }),
      '/api/log': { entries: [] },
    },
    storage: { 'vibe-companion-tab': 'conversation' },
  })
  try {
    await p.click('convo-new')
    await p.click('fresh-branch')
    assert.ok(!shown(p, 'fresh-from-default-row'))
    const name = p.$('fresh-branch-details').querySelector('.default-setter input')
    assert.equal(name.value, 'master')
    await p.click(p.button('Set', p.$('fresh-branch-details')))
    assert.deepEqual(p.posted('/api/default-branch'), [{ name: 'master' }])
    assert.ok(p.$('fresh-branch').checked, 'still ticked')
    assert.ok(shown(p, 'fresh-from-default-row'))
    assert.match(p.$('fresh-from-default-row').textContent, /Branch from master \(local\)/)
    assert.deepEqual(p.posted('/api/conversation/new/fetch'), [], 'no origin to check')
    assert.ok(!shown(p, 'fresh-fetch'))
    await p.type(p.$('fresh-branch-name'), 'feat/x')
    await p.click('fresh-from-default')
    await p.click('fresh-run')
    assert.deepEqual(p.posted('/api/conversation/new')[0].branch, { name: 'feat/x', from: 'default', update: false })
  } finally { p.close() }
})

test('a set default branch with no commits yet: branching from it is disabled, saying so', async () => {
  const p = await freshPage({ options: { branch: { current: 'feat/a', dirty: false, default: 'main', defaultSet: true, defaultEmpty: true } } })
  try {
    await p.click('fresh-branch')
    assert.ok(shown(p, 'fresh-from-default-row'))
    assert.ok(p.$('fresh-from-default').disabled)
    assert.match(p.$('fresh-from-default-row').textContent, /main has no commits yet/)
  } finally { p.close() }
})

test('a set default branch is branched from as it is here: origin is not checked', async () => {
  const p = await freshPage({ options: { branch: { current: 'feat/a', dirty: false, default: 'master', defaultSet: true } } })
  try {
    await p.click('fresh-branch')
    assert.match(p.$('fresh-from-default-row').textContent, /Branch from master \(local\)/)
    assert.deepEqual(p.posted('/api/conversation/new/fetch'), [])
    assert.ok(!shown(p, 'fresh-fetch'))
  } finally { p.close() }
})

// --- network log ------------------------------------------------------------

// The hosts in the log are whatever the sandbox asked to reach, so one can
// be a pattern of the agent's making: Allow is offered only for a single
// host, never one that would let in a whole domain, range or port.
test('a logged host is offered Allow only when it names one host', async () => {
  const entries = ['api.example.com', 'example.com:443', '[2001:db8::1]:443', '10.0.0.1',
    '**.com:443', '*.example.com', 'api?.example.com', 'api[12].example.com', '0.0.0.0/0']
    .map((host) => ({ host, decision: 'blocked', hits: 1 }))
  const p = await openPage({ routes: {
    '/api/state': () => ({ sandbox: 'box', sandboxTabs: true, attention: {} }),
    '/api/log': { entries },
  }, storage: { 'vibe-companion-tab': 'log' } })
  try {
    const offered = [...p.$('log-table').tBodies[0].rows]
      .filter((row) => [...row.querySelectorAll('button')].some((b) => b.textContent === 'Allow'))
      .map((row) => row.querySelector('.host').textContent)
    assert.deepEqual(offered, ['api.example.com', 'example.com:443', '[2001:db8::1]:443', '10.0.0.1'])
  } finally { p.close() }
})

// The name of the new branch is checked as it is typed: against the branches there already are,
// here and at origin, and against git's rules for a name. Starting checks again, as the branches
// may have changed since the dialog opened.
async function branchPage(branch) {
  const p = await freshPage({ options: { review: false, branch: Object.assign({}, clean, branch) } })
  await p.click('fresh-branch')
  return p
}

async function nameIs(p, name) { await p.type(p.$('fresh-branch-name'), name) }
const noteOf = (p) => (shown(p, 'fresh-branch-note') ? p.$('fresh-branch-note').textContent : '')
const flagged = (p) => p.$('fresh-branch-name').getAttribute('aria-invalid') === 'true'

test('a branch name that is taken here is said so as it is typed, and flagged', async () => {
  const p = await branchPage({ branches: ['master', 'feat/a', 'feat/done'] })
  try {
    await nameIs(p, 'feat/done')
    assert.match(noteOf(p), /already a branch called feat\/done/)
    assert.equal(flagged(p), true)
    assert.equal(p.$('fresh-run').disabled, false, 'starting still checks, and says')
    await nameIs(p, 'feat/new')
    assert.equal(noteOf(p), '')
    assert.equal(flagged(p), false)
  } finally { p.close() }
})

test('a branch name that origin has already is a warning, not an error', async () => {
  const p = await branchPage({ branches: ['master'], remote: ['master', 'feat/theirs'] })
  try {
    await nameIs(p, 'feat/theirs')
    assert.match(noteOf(p), /origin has a branch called feat\/theirs/)
    assert.match(noteOf(p), /refused/)
    assert.equal(flagged(p), false, 'it may be what they mean')
    assert.equal(p.$('fresh-branch-note').classList.contains('warn-text'), true)
    await p.click('fresh-run')
    assert.equal(p.posted('/api/conversation/new').length, 1, 'it can still be started')
  } finally { p.close() }
})

test('a name taken here and at origin is the error', async () => {
  const p = await branchPage({ branches: ['master', 'feat/x'], remote: ['feat/x'] })
  try {
    await nameIs(p, 'feat/x')
    assert.match(noteOf(p), /already a branch called feat\/x/)
    assert.equal(flagged(p), true)
  } finally { p.close() }
})

test('a name is compared as typed, case and all', async () => {
  const p = await branchPage({ branches: ['Feat/A'] })
  try {
    await nameIs(p, 'feat/a')
    assert.equal(noteOf(p), '')
  } finally { p.close() }
})

test('names git would refuse are said so as they are typed', async () => {
  const p = await branchPage({ branches: [] })
  try {
    for (const bad of ['feat/a b', 'a..b', 'feat/', '/a', 'a//b', '-x', 'x.lock', 'a/x.lock/b', '@', 'a@{b', 'a~b', 'a^b', 'a:b', 'a?b', 'a*b', 'a[b', 'a\\b', '.hidden', 'a/.b', 'ends.', 'tab\tbed']) {
      await nameIs(p, bad)
      assert.equal(flagged(p), true, JSON.stringify(bad))
      assert.match(noteOf(p), /can't be a branch name/, JSON.stringify(bad))
    }
    for (const good of ['feat/new-thing', 'feat/a.b', 'hotfix_1', 'v1.2.3', 'a/b/c', 'ünï']) {
      await nameIs(p, good)
      assert.equal(flagged(p), false, JSON.stringify(good))
      assert.equal(noteOf(p), '', JSON.stringify(good))
    }
  } finally { p.close() }
})

test('the starting text, feat/, is not scolded before anything has been typed', async () => {
  const p = await branchPage({ branches: [] })
  try {
    assert.equal(p.$('fresh-branch-name').value, 'feat/')
    assert.equal(noteOf(p), '')
    assert.equal(flagged(p), false)
  } finally { p.close() }
})

test('an emptied name is not scolded either: starting says to name it', async () => {
  const p = await branchPage({ branches: [] })
  try {
    await nameIs(p, 'x')
    await nameIs(p, '')
    assert.equal(noteOf(p), '')
  } finally { p.close() }
})
