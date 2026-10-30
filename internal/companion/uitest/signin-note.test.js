// What the Conversation tab says when the agent isn't signed in: where to sign in from the page, not a terminal.
'use strict'
const test = require('node:test')
const assert = require('node:assert/strict')
const { openPage } = require('./page.js')

const signedOut = { seq: 2, kind: 'agent', agent: { type: 'assistant', error: 'authentication_failed', message: { content: [{ type: 'text', text: 'Not logged in · Please run /login' }] } } }

async function pageWith({ signin }) {
  const events = [{ seq: 1, kind: 'user', text: 'hi' }, signedOut]
  const p = await openPage({
    routes: {
      '/api/state': { sandbox: 'box', sandboxTabs: true, conversation: true, admin: true, signin, signinStart: signin, attention: {} },
      '/api/admin': { state: 'running', info: '' },
      '/api/signin': { agent: 'claude', docker: { state: 'signed-in', manual: [] }, agentSignIn: { state: 'signed-out', manual: [] } },
      '/api/signin/job': { kind: '', lines: [] },
      '/api/conversation': (call) => {
        const after = Number(new URLSearchParams(call.query).get('after') || 0)
        return { events: events.filter((e) => e.seq > after), last: 2, running: false }
      },
      '/api/log': { entries: [] },
    },
    storage: { 'vibe-companion-tab': 'conversation' },
  })
  await p.every('pollConversation')
  return p
}

const errorNote = (p) => p.$('convo-log').querySelector('.msg.error')

test('a signed-out agent is told to sign in from the Admin tab, with a button that goes there', async () => {
  const p = await pageWith({ signin: true })
  try {
    const note = errorNote(p)
    assert.match(note.textContent, /isn't signed in/i)
    assert.doesNotMatch(note.textContent, /terminal/i, 'it still sends the user to a terminal')
    await p.click(note.querySelector('button'))
    assert.equal(p.$('panel-admin').hidden, false)
    assert.equal(p.$('panel-conversation').hidden, true)
  } finally { p.close() }
})

test('where the host has no sign-in card, the note still says to sign in in a terminal', async () => {
  const p = await pageWith({ signin: false })
  try {
    const note = errorNote(p)
    assert.match(note.textContent, /terminal/i)
    assert.equal(note.querySelector('button'), null)
  } finally { p.close() }
})
