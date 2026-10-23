// Tests of what vibe says in the conversation while it makes the sandbox ready for a turn, and takes back once
// it is, in jsdom (see page.js), run with page.test.js.
'use strict'
const test = require('node:test')
const assert = require('node:assert/strict')
const { openPage } = require('./page.js')

// conversationPage is a page whose conversation is whatever at.events holds: the events it's asked for are the
// ones after what it has seen, as vibe's are.
async function conversationPage(events = []) {
  const at = { events }
  const routes = {
    '/api/state': { sandbox: 'box', sandboxTabs: true, conversation: true, attention: {} },
    '/api/conversation': (call) => {
      const after = Number(new URLSearchParams(call.query).get('after') || 0)
      const last = at.events.reduce((m, e) => Math.max(m, e.seq), 0)
      return { events: at.events.filter((e) => e.seq > after), last, running: false }
    },
    '/api/log': { entries: [] },
  }
  const p = await openPage({ routes, storage: { 'vibe-companion-tab': 'conversation' } })
  await p.every('pollConversation')
  return Object.assign(p, { at })
}

const lines = (p) => Array.from(p.$('convo-log').querySelectorAll('.msg')).map((m) => m.textContent.trim())
const NOTE = 'starting the sandbox (sbx stops one that\'s left idle) — the agent answers once it\'s up'
const user = (seq, text) => ({ seq, kind: 'user', text, at: 1792650000000 })
const note = (seq, text) => ({ seq, kind: 'note', text })
const retract = (seq, of) => ({ seq, kind: 'retract', of })

test('what vibe says while it makes the sandbox ready is shown', async () => {
  const p = await conversationPage([user(1, 'hello'), note(2, NOTE)])
  try {
    assert.ok(lines(p).some((l) => l.includes('starting the sandbox')))
  } finally { p.close() }
})

test('it is taken away, and nothing else, once vibe takes it back', async () => {
  const p = await conversationPage([user(1, 'hello'), note(2, NOTE)])
  try {
    p.at.events.push(retract(3, 2), { seq: 4, kind: 'agent', agent: { type: 'assistant', message: { content: [{ type: 'text', text: 'hi there' }] } } })
    await p.every('pollConversation')
    assert.ok(!lines(p).some((l) => l.includes('starting the sandbox')), 'gone')
    assert.ok(lines(p).some((l) => l.includes('hello')), 'what the user said stays')
    assert.ok(lines(p).some((l) => l.includes('hi there')), 'and the answer')
  } finally { p.close() }
})

test('taking back a note the page never had changes nothing', async () => {
  const p = await conversationPage([user(1, 'hello')])
  try {
    const before = lines(p)
    p.at.events.push(retract(2, 7))
    await p.every('pollConversation')
    assert.deepEqual(lines(p), before)
  } finally { p.close() }
})

test('only the note taken back goes: another note stays', async () => {
  const p = await conversationPage([note(1, 'one thing'), note(2, 'another thing')])
  try {
    p.at.events.push(retract(3, 1))
    await p.every('pollConversation')
    assert.deepEqual(lines(p).filter((l) => /thing/.test(l)), ['another thing'])
  } finally { p.close() }
})

test('a note that is not taken back stays, as it explains what follows', async () => {
  const p = await conversationPage([user(1, 'hello'), note(2, NOTE), { seq: 3, kind: 'error', text: "sandbox 'box' didn't start" }])
  try {
    assert.ok(lines(p).some((l) => l.includes('starting the sandbox')))
    assert.ok(lines(p).some((l) => l.includes("didn't start")))
  } finally { p.close() }
})

test('a page opened after never has the note, so has nothing to take back', async () => {
  const p = await conversationPage([user(1, 'hello')])
  try {
    assert.ok(!lines(p).some((l) => l.includes('starting the sandbox')))
  } finally { p.close() }
})
