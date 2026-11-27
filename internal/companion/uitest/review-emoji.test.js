// :emoji: names complete in the Review tab's boxes (a reply, a new comment, editing one's own), as they do in the prompt: a list at
// the colon, Tab to pick, and a whole :name: becomes its character when its closing colon is typed. In jsdom (see page.js).
'use strict'
const test = require('node:test')
const assert = require('node:assert/strict')
const { openPage } = require('./page.js')

const thread = { id: 1, file: 'a.go', side: 'new', line: 1, endLine: 1, comments: [{ author: 'agent', body: 'found', at: '2026-11-16T10:00:00Z' }], status: 'open' }
const review = { threads: [thread], lastId: 1, started: '2026-11-16T09:00:00Z', ref: 'master', state: 'done' }
const diff = [{ path: 'a.go', status: 'modified', lines: 1, fingerprint: 'f', hunks: [{ oldStart: 1, newStart: 1, lines: [{ kind: 'add', new: 1, text: 'x' }] }] }]

async function reviewPage(storage = {}) {
  const p = await openPage({
    routes: {
      '/api/state': { sandbox: 'box', sandboxTabs: true, review: true, conversation: true, reviewAt: '', attention: {} },
      '/api/review': (call) => (call.query.includes('diff=1') ? { review, diff, diffKey: 'k' } : { review }),
      '/api/review/do': { review },
      '/api/conversation': { events: [], last: 0, running: false },
      '/api/log': { entries: [] },
    },
    storage: Object.assign({ 'vibe-companion-tab': 'review' }, storage),
  })
  await p.settle()
  return p
}

const reply = (p) => p.$('panel-review').querySelector('textarea[data-thread="1"]')
const listOf = (box) => box.closest('.emoji-host').querySelector('ul.emoji')
const names = (box) => [...listOf(box).querySelectorAll('li[role=option]')].map((li) => li.dataset.name)

function type(p, box, text, data) {
  box.focus()
  box.value = text
  box.setSelectionRange(text.length, text.length)
  box.dispatchEvent(new p.window.InputEvent('input', { bubbles: true, inputType: 'insertText', data: data || text.slice(-1) }))
}

function key(p, box, k) {
  const ev = new p.window.KeyboardEvent('keydown', { key: k, bubbles: true, cancelable: true })
  box.dispatchEvent(ev)
  return ev
}

test('a reply offers the emoji for a name being typed, and Tab puts the character in, kept as the draft', async () => {
  const p = await reviewPage()
  try {
    const box = reply(p)
    assert.ok(box, 'no reply box')
    type(p, box, 'nice :roc')
    assert.equal(listOf(box).hidden, false)
    assert.equal(names(box)[0], 'rocket')
    assert.equal(box.getAttribute('aria-expanded'), 'true')
    assert.equal(box.getAttribute('aria-controls'), listOf(box).id)
    const ev = key(p, box, 'Tab')
    assert.equal(ev.defaultPrevented, true)
    assert.equal(box.value, 'nice 🚀 ')
    assert.equal(listOf(box).hidden, true)
    await p.poll()
    assert.equal(reply(p).value, 'nice 🚀 ', 'the draft kept what the completion put in')
  } finally { p.close() }
})

test("typing a whole name's closing colon in a reply makes it the character; Escape closes the list and nothing else", async () => {
  const p = await reviewPage()
  try {
    const box = reply(p)
    type(p, box, 'done :saluting_face:', ':')
    assert.equal(box.value, 'done 🫡')
    type(p, box, 'and :ta')
    assert.equal(listOf(box).hidden, false)
    const ev = key(p, box, 'Escape')
    assert.equal(ev.defaultPrevented, true)
    assert.equal(listOf(box).hidden, true)
    assert.equal(box.value, 'and :ta')
  } finally { p.close() }
})

test('with completion turned off in Settings, a reply is left as typed', async () => {
  const p = await reviewPage({ 'vibe-emoji-complete': 'off' })
  try {
    const box = reply(p)
    type(p, box, 'done :tada:', ':')
    assert.equal(box.value, 'done :tada:')
    type(p, box, 'nice :roc')
    assert.equal(listOf(box).hidden, true)
  } finally { p.close() }
})

test('a new comment on a line completes emoji too, and what it puts in is the comment that is sent', async () => {
  const p = await reviewPage()
  try {
    await p.click(p.$('review-files').querySelector('[aria-label="Comment on line R1"]'))
    const box = p.$('review-files').querySelector('textarea[aria-label="Your comment"]')
    type(p, box, 'ship it :tada:', ':')
    assert.equal(box.value, 'ship it 🎉')
    const send = [...p.$('review-files').querySelectorAll('button')].find((b) => b.textContent === 'Comment')
    assert.equal(send.disabled, false, 'the comment is there to send')
  } finally { p.close() }
})
