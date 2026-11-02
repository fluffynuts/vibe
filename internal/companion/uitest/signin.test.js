// The Sign-in card on the Admin tab: whether sbx and the agent are signed in, what to do by hand if not, and a button that
// asks again.
'use strict'
const test = require('node:test')
const assert = require('node:assert/strict')
const { openPage } = require('./page.js')

const SIGNED_IN = { state: 'signed-in', detail: 'me@example.com · Codeo (team)', manual: ['do this', 'then that'] }
const SIGNED_OUT = { state: 'signed-out', manual: ['In a terminal, run: sbx login', 'Open the address it prints.'] }

async function adminPage({ signin = true, answer = { agent: 'claude', docker: SIGNED_IN, agentSignIn: SIGNED_OUT } } = {}) {
  const p = await openPage({
    routes: {
      '/api/state': { sandbox: 'box', sandboxTabs: true, admin: true, signin, attention: {} },
      '/api/admin': { state: 'running', info: '' },
      '/api/signin': (call) => (typeof answer === 'function' ? answer(call) : answer),
    },
    storage: { 'vibe-companion-tab': 'admin' },
  })
  await p.settle()
  return p
}

const asked = (p) => p.calls.filter((c) => c.path === '/api/signin').length
const row = (p, which) => p.$('signin-' + which)

test('there is no card where the host has no sign-in to show', async () => {
  const p = await adminPage({ signin: false })
  try {
    assert.equal(p.$('admin-signin').hidden, true)
    assert.equal(asked(p), 0, 'it asked anyway')
  } finally { p.close() }
})

test('the card says whether sbx and the agent are signed in, and who the agent is signed in as', async () => {
  const p = await adminPage()
  try {
    assert.equal(p.$('admin-signin').hidden, false)
    assert.match(row(p, 'docker').textContent, /Docker \(sbx\)/)
    assert.match(row(p, 'docker').querySelector('.pill').textContent, /signed in/i)
    assert.match(row(p, 'docker').querySelector('.pill').className, /ok/)
    assert.match(row(p, 'agent').textContent, /claude/i)
    assert.match(row(p, 'agent').querySelector('.pill').textContent, /signed out/i)
    assert.match(row(p, 'agent').querySelector('.pill').className, /bad/)
  } finally { p.close() }
})

test('who an agent is signed in as is shown', async () => {
  const p = await adminPage({ answer: { agent: 'claude', docker: SIGNED_IN, agentSignIn: SIGNED_IN } })
  try {
    assert.match(row(p, 'agent').textContent, /me@example\.com · Codeo \(team\)/)
  } finally { p.close() }
})

test('each has the steps for doing it by hand, from the host', async () => {
  const p = await adminPage()
  try {
    const steps = [...row(p, 'docker').querySelectorAll('details li')].map((li) => li.textContent)
    assert.deepEqual(steps, ['do this', 'then that'])
    assert.match(row(p, 'docker').querySelector('summary').textContent, /yourself|by hand/i)
    assert.deepEqual([...row(p, 'agent').querySelectorAll('details li')].map((li) => li.textContent), SIGNED_OUT.manual)
  } finally { p.close() }
})

test('what the host could not tell is said as unknown, with why, and not as signed out', async () => {
  const p = await adminPage({ answer: { agent: 'claude', docker: { state: 'unknown', detail: 'sbx didn’t answer in time', manual: [] }, agentSignIn: { state: 'unknown', detail: 'it can be checked once sbx is signed in', manual: [] } } })
  try {
    assert.match(row(p, 'docker').querySelector('.pill').textContent, /unknown/i)
    assert.match(row(p, 'docker').textContent, /didn’t answer in time/)
    assert.doesNotMatch(row(p, 'docker').querySelector('.pill').className, /bad|ok/)
  } finally { p.close() }
})

test('a missing network policy is said, as the thing to put right', async () => {
  const p = await adminPage({ answer: { agent: 'claude', docker: { state: 'needs-policy', detail: 'sbx hasn’t been told which network policy to use.', manual: ['sbx policy init balanced'] }, agentSignIn: { state: 'unknown', manual: [] } } })
  try {
    assert.match(row(p, 'docker').querySelector('.pill').textContent, /network policy/i)
    assert.match(row(p, 'docker').querySelector('.pill').className, /bad/)
  } finally { p.close() }
})

test('Check now asks again, and shows the new answer', async () => {
  let n = 0
  const p = await adminPage({ answer: () => (++n === 1 ? { agent: 'claude', docker: SIGNED_OUT, agentSignIn: SIGNED_OUT } : { agent: 'claude', docker: SIGNED_IN, agentSignIn: SIGNED_IN }) })
  try {
    assert.match(row(p, 'docker').querySelector('.pill').textContent, /signed out/i)
    await p.click('signin-check')
    await p.settle()
    assert.equal(asked(p), 2)
    assert.match(row(p, 'docker').querySelector('.pill').textContent, /signed in/i)
  } finally { p.close() }
})

test('a check that fails says so, and keeps what was shown', async () => {
  let n = 0
  const p = await adminPage({ answer: () => (++n === 1 ? { agent: 'claude', docker: SIGNED_IN, agentSignIn: SIGNED_IN } : Promise.reject(new Error('down'))) })
  try {
    await p.click('signin-check')
    await p.settle()
    assert.equal(p.$('signin-status').hidden, false)
    assert.match(row(p, 'docker').querySelector('.pill').textContent, /signed in/i)
  } finally { p.close() }
})

test('a row with nothing to say beyond its pill says nothing more: not "null"', async () => {
  const p = await adminPage({ answer: { agent: 'claude', docker: { state: 'signed-in', manual: [] }, agentSignIn: { state: 'signed-in', manual: [] } } })
  try {
    for (const which of ['docker', 'agent']) {
      assert.doesNotMatch(row(p, which).textContent, /null|undefined/, which)
      assert.equal(row(p, which).querySelector('details'), null, which + ' has steps to show')
    }
  } finally { p.close() }
})
