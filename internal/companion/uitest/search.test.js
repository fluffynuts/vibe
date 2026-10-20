// Tests of searching the conversation, in jsdom (see page.js), run with page.test.js. jsdom has
// no CSS highlights, so they're stood in for with a map of what each highlight holds.
'use strict'
const test = require('node:test')
const assert = require('node:assert/strict')
const { openPage } = require('./page.js')

const said = (seq, text) => ({ seq, kind: 'agent', agent: { type: 'assistant', message: { content: [{ type: 'text', text }] } } })
const ran = (seq, command) => ({ seq, kind: 'agent', agent: { type: 'assistant', message: { content: [{ type: 'tool_use', name: 'Bash', input: { command } }] } } })
const output = (seq, text) => ({ seq, kind: 'agent', agent: { type: 'user', message: { content: [{ type: 'tool_result', content: text }] } } })

async function searchPage(events, { tab = 'conversation' } = {}) {
  const at = { events, scrolled: [] }
  const routes = {
    '/api/state': { sandbox: 'box', sandboxTabs: true, conversation: true, attention: {} },
    '/api/conversation': (call) => {
      const after = Number(new URLSearchParams(call.query).get('after')) || 0
      const fresh = at.events.filter((e) => e.seq > after)
      return { events: fresh, last: at.events.reduce((m, e) => Math.max(m, e.seq), 0), running: false }
    },
  }
  const p = await openPage({ routes, storage: { 'vibe-companion-tab': tab } })
  const w = p.window
  w.CSS = { highlights: new Map() }
  w.Highlight = class { constructor(...ranges) { this.ranges = ranges } }
  w.Element.prototype.scrollIntoView = function () { at.scrolled.push(this.textContent) }
  await p.every('pollConversation')
  return Object.assign(p, { at })
}

const matches = (p) => ((p.window.CSS.highlights.get('find-match') || { ranges: [] }).ranges).map((r) => r.toString())
const current = (p) => ((p.window.CSS.highlights.get('find-current') || { ranges: [] }).ranges).map((r) => r.toString())
const count = (p) => p.$('convo-find-count').textContent
const boxShown = (p) => !p.$('convo-find').hidden

async function open(p) { await p.click('convo-find-open') }
async function find(p, query) { await p.type(p.$('convo-find-input'), query) }
function press(p, key, init = {}, target) {
  const ev = new p.window.KeyboardEvent('keydown', Object.assign({ key, bubbles: true, cancelable: true }, init))
  ;(target || p.$('convo-find-input')).dispatchEvent(ev)
  return ev
}

const sample = () => [
  said(1, 'Here is the test methodology I suggest: write the Test first.'),
  said(2, 'Then run the tests, and TEST again.'),
  ran(3, 'go test ./...'),
  output(4, 'ok  all tests passed\nsecret-needle in the output'),
]

test('the search box is closed until its button is pressed, which puts the cursor in it', async () => {
  const p = await searchPage(sample())
  try {
    assert.equal(boxShown(p), false)
    await open(p)
    assert.equal(boxShown(p), true)
    assert.equal(p.window.document.activeElement, p.$('convo-find-input'))
  } finally { p.close() }
})

test('every occurrence is highlighted whatever its case, and counted', async () => {
  const p = await searchPage(sample())
  try {
    await open(p)
    await find(p, 'test')
    assert.deepEqual(matches(p), ['test', 'Test', 'test', 'TEST', 'test'])
    assert.equal(count(p), '1 of 5')
    assert.deepEqual(current(p), ['test'])
  } finally { p.close() }
})

test('what is typed is searched for as it is, not as a pattern', async () => {
  const p = await searchPage([said(1, 'call f(a.b) and f(axb)')])
  try {
    await open(p)
    await find(p, 'f(a.b)')
    assert.deepEqual(matches(p), ['f(a.b)'])
  } finally { p.close() }
})

test('Enter goes to the next match and Shift+Enter to the one before, round the ends', async () => {
  const p = await searchPage([said(1, 'one fish two fish red fish')])
  try {
    await open(p)
    await find(p, 'fish')
    assert.equal(count(p), '1 of 3')
    press(p, 'Enter')
    assert.equal(count(p), '2 of 3')
    press(p, 'Enter')
    press(p, 'Enter')
    assert.equal(count(p), '1 of 3', 'past the last, round to the first')
    press(p, 'Enter', { shiftKey: true })
    assert.equal(count(p), '3 of 3', 'before the first, round to the last')
  } finally { p.close() }
})

test('the buttons go to the next and the previous match', async () => {
  const p = await searchPage([said(1, 'fish fish fish')])
  try {
    await open(p)
    await find(p, 'fish')
    await p.click('convo-find-next')
    assert.equal(count(p), '2 of 3')
    await p.click('convo-find-prev')
    await p.click('convo-find-prev')
    assert.equal(count(p), '3 of 3')
  } finally { p.close() }
})

test('the match gone to is scrolled to, and is the one highlighted as current', async () => {
  const p = await searchPage([said(1, 'alpha needle'), said(2, 'beta needle')])
  try {
    await open(p)
    await find(p, 'needle')
    press(p, 'Enter')
    assert.match(p.at.scrolled[p.at.scrolled.length - 1], /beta needle/)
    assert.equal(current(p).length, 1)
    assert.equal(matches(p).length, 2)
  } finally { p.close() }
})

test('no match is said, with nothing highlighted', async () => {
  const p = await searchPage(sample())
  try {
    await open(p)
    await find(p, 'zebra')
    assert.equal(count(p), 'No matches')
    assert.equal(p.$('convo-find-input').classList.contains('nomatch'), true)
    assert.deepEqual(matches(p), [])
    await find(p, 'test')
    assert.equal(p.$('convo-find-input').classList.contains('nomatch'), false)
  } finally { p.close() }
})

test('an empty search says nothing and highlights nothing', async () => {
  const p = await searchPage(sample())
  try {
    await open(p)
    await find(p, 'test')
    await find(p, '')
    assert.equal(count(p), '')
    assert.deepEqual(matches(p), [])
  } finally { p.close() }
})

test('Escape closes the box and clears the highlights', async () => {
  const p = await searchPage(sample())
  try {
    await open(p)
    await find(p, 'test')
    press(p, 'Escape')
    assert.equal(boxShown(p), false)
    assert.deepEqual(matches(p), [])
    assert.deepEqual(current(p), [])
  } finally { p.close() }
})

test('the close button closes it too, and opening it again finds the last search again', async () => {
  const p = await searchPage(sample())
  try {
    await open(p)
    await find(p, 'test')
    await p.click('convo-find-close')
    assert.equal(boxShown(p), false)
    await open(p)
    assert.equal(p.$('convo-find-input').value, 'test')
    assert.equal(matches(p).length, 5)
  } finally { p.close() }
})

test('a phrase is found across the formatting inside it', async () => {
  const p = await searchPage([said(1, 'a **very** good plan')])
  try {
    await open(p)
    await find(p, 'very good')
    assert.deepEqual(matches(p), ['very good'])
  } finally { p.close() }
})

test('a phrase is not found across two messages', async () => {
  const p = await searchPage([said(1, 'ends with foo'), said(2, 'bar starts this')])
  try {
    await open(p)
    await find(p, 'foobar')
    assert.equal(count(p), 'No matches')
  } finally { p.close() }
})

test('the commands the agent ran are searched, since they are in view', async () => {
  const p = await searchPage(sample())
  try {
    await open(p)
    await find(p, './...')
    assert.equal(matches(p).length, 1)
  } finally { p.close() }
})

test('what is inside a tool result, which is collapsed, is not searched unless the box is ticked', async () => {
  const p = await searchPage(sample())
  try {
    await open(p)
    await find(p, 'secret-needle')
    assert.equal(count(p), 'No matches')
    assert.equal(p.$('convo-find-tools').checked, false)
    p.$('convo-find-tools').checked = true
    p.$('convo-find-tools').dispatchEvent(new p.window.Event('change', { bubbles: true }))
    await p.settle()
    assert.deepEqual(matches(p), ['secret-needle'])
  } finally { p.close() }
})

test('going to a match inside a collapsed tool result opens it', async () => {
  const p = await searchPage(sample())
  try {
    await open(p)
    p.$('convo-find-tools').checked = true
    p.$('convo-find-tools').dispatchEvent(new p.window.Event('change', { bubbles: true }))
    await find(p, 'secret-needle')
    const details = p.window.document.querySelector('details.tool-result')
    assert.equal(details.open, true)
  } finally { p.close() }
})

test('what the agent says afterwards is found as it comes, without moving to it', async () => {
  const p = await searchPage([said(1, 'fish one'), said(2, 'fish two')])
  try {
    await open(p)
    await find(p, 'fish')
    press(p, 'Enter')
    assert.equal(count(p), '2 of 2')
    p.at.events.push(said(3, 'more fish'))
    await p.every('pollConversation')
    assert.equal(count(p), '2 of 3')
    assert.equal(matches(p).length, 3)
  } finally { p.close() }
})

test('F3 opens the search on the conversation tab, and the browser is kept from taking it', async () => {
  const p = await searchPage(sample())
  try {
    const ev = press(p, 'F3', {}, p.window.document.body)
    assert.equal(ev.defaultPrevented, true)
    assert.equal(boxShown(p), true)
    assert.equal(p.window.document.activeElement, p.$('convo-find-input'))
  } finally { p.close() }
})

test('F3 in the open box goes to the next match, and Shift+F3 to the one before', async () => {
  const p = await searchPage([said(1, 'fish fish fish')])
  try {
    await open(p)
    await find(p, 'fish')
    press(p, 'F3')
    assert.equal(count(p), '2 of 3')
    press(p, 'F3', { shiftKey: true })
    press(p, 'F3', { shiftKey: true })
    assert.equal(count(p), '3 of 3')
  } finally { p.close() }
})

test('F3 elsewhere than the conversation is left to the browser', async () => {
  const p = await searchPage(sample(), { tab: 'rules' })
  try {
    const ev = press(p, 'F3', {}, p.window.document.body)
    assert.equal(ev.defaultPrevented, false)
    assert.equal(boxShown(p), false)
  } finally { p.close() }
})

test('typing in the search box is not taken for the prompt', async () => {
  const p = await searchPage(sample())
  try {
    await open(p)
    const ev = press(p, 'a')
    assert.equal(p.window.document.activeElement, p.$('convo-find-input'))
    assert.equal(ev.defaultPrevented, false)
  } finally { p.close() }
})
