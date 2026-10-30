// The notice above the conversation when sign-in is needed, so that a user who lands on the Conversation tab is told, and
// the card on the Admin tab for an agent whose sign-in vibe can't read.
'use strict'
const test = require('node:test')
const assert = require('node:assert/strict')
const { openPage } = require('./page.js')

const IN = { state: 'signed-in', manual: [] }
const OUT = { state: 'signed-out', manual: ['by hand'] }

async function pageWith({ signin = true, answer, tab = 'conversation' }) {
  const host = { answer }
  const p = await openPage({
    routes: {
      '/api/state': { sandbox: 'box', sandboxTabs: true, conversation: true, admin: true, signin, signinStart: signin, attention: {} },
      '/api/admin': { state: 'running', info: '' },
      '/api/signin': () => host.answer,
      '/api/signin/job': { kind: '', lines: [] },
      '/api/conversation': { events: [], last: 0, running: false },
      '/api/log': { entries: [] },
    },
    storage: { 'vibe-companion-tab': tab },
  })
  await p.settle()
  p.host = host
  return p
}

const notice = (p) => p.$('convo-signin')

test('the Conversation tab says the agent must be signed in, with a way to the Sign-in card, without opening the Admin tab', async () => {
  const p = await pageWith({ answer: { agent: 'claude', docker: IN, agentSignIn: OUT } })
  try {
    assert.equal(notice(p).hidden, false)
    assert.match(p.$('convo-signin-text').textContent, /claude/i)
    assert.match(p.$('convo-signin-text').textContent, /sign/i)
    await p.click('convo-signin-open')
    assert.equal(p.$('panel-admin').hidden, false)
  } finally { p.close() }
})

test('it says sbx is signed out when that is what is wrong, since nothing can start without it', async () => {
  const p = await pageWith({ answer: { agent: 'claude', docker: OUT, agentSignIn: { state: 'unknown', manual: [] } } })
  try {
    assert.equal(notice(p).hidden, false)
    assert.match(p.$('convo-signin-text').textContent, /Docker/)
  } finally { p.close() }
})

test('with everything signed in there is no notice', async () => {
  const p = await pageWith({ answer: { agent: 'claude', docker: IN, agentSignIn: { state: 'signed-in', detail: 'me@x', manual: [] } } })
  try {
    assert.equal(notice(p).hidden, true)
  } finally { p.close() }
})

test('what could not be told is no reason for a notice', async () => {
  const p = await pageWith({ answer: { agent: 'claude', docker: IN, agentSignIn: { state: 'unknown', detail: 'the sandbox is stopped', manual: [] } } })
  try {
    assert.equal(notice(p).hidden, true)
  } finally { p.close() }
})

test('a host with no sign-in card says nothing, and is not asked', async () => {
  const p = await pageWith({ signin: false, answer: { agent: 'claude', docker: OUT, agentSignIn: OUT } })
  try {
    assert.equal(notice(p).hidden, true)
    assert.equal(p.calls.filter((c) => c.path === '/api/signin').length, 0)
  } finally { p.close() }
})

test('the notice goes once a check finds the agent signed in', async () => {
  const p = await pageWith({ answer: { agent: 'claude', docker: IN, agentSignIn: OUT } })
  try {
    assert.equal(notice(p).hidden, false)
    p.host.answer = { agent: 'claude', docker: IN, agentSignIn: { state: 'signed-in', manual: [] } }
    await p.click('signin-check')
    assert.equal(notice(p).hidden, true)
  } finally { p.close() }
})

test("an agent vibe can't read the sign-in of shows under its own name as not checked, with no sign-in button and nothing of Claude's", async () => {
  const p = await pageWith({ tab: 'admin', answer: { agent: 'cursor', docker: IN, agentSignIn: { state: 'unsupported', detail: "vibe can't check cursor's sign-in yet", manual: [] } } })
  try {
    const row = p.$('signin-agent')
    assert.match(row.textContent, /cursor/)
    assert.match(row.querySelector('.pill').textContent, /not checked/i)
    assert.equal(row.querySelector('button'), null)
    assert.doesNotMatch(row.textContent, /claude/i)
    assert.equal(notice(p).hidden, true, 'a notice for an agent that is only unchecked')
  } finally { p.close() }
})
