// The API key option on the Sign-in card: the other way to sign Claude in, for someone with a key and not a subscription.
'use strict'
const test = require('node:test')
const assert = require('node:assert/strict')
const { openPage } = require('./page.js')

const IN = { state: 'signed-in', manual: [] }
const OUT = { state: 'signed-out', manual: ['by hand'] }

async function pageWith({ key = true, answer = { status: 200, body: { set: true } } } = {}) {
  const p = await openPage({
    routes: {
      '/api/state': { sandbox: 'box', sandboxTabs: true, conversation: true, admin: true, signin: true, signinStart: true, signinKey: key, attention: {} },
      '/api/admin': { state: 'running', info: '' },
      '/api/conversation': { events: [], last: 0, running: false },
      '/api/signin': { agent: 'claude', docker: IN, agentSignIn: OUT },
      '/api/signin/job': { kind: '', lines: [] },
      '/api/signin/apikey': () => (answer.status === 200 ? answer.body : answer),
    },
    storage: { 'vibe-companion-tab': 'admin' },
  })
  await p.settle()
  return p
}

const field = (p) => p.$('signin-agent').querySelector('input[type=password]')
const go = (p) => p.$('signin-agent').querySelector('button.signin-key-go')
const posted = (p) => p.posted('/api/signin/apikey')

test('the agent has an API key option, and sbx does not', async () => {
  const p = await pageWith()
  try {
    assert.ok(field(p), 'there is no field for a key')
    assert.equal(field(p).autocomplete, 'off')
    assert.match(p.$('signin-agent').textContent, /API key/i)
    assert.equal(p.$('signin-docker').querySelector('input'), null)
  } finally { p.close() }
})

test('a host that cannot store a key has no such option', async () => {
  const p = await pageWith({ key: false })
  try {
    assert.equal(field(p), null)
  } finally { p.close() }
})

test('the key is not sent until it is confirmed, and the confirmation says what it changes for every sandbox', async () => {
  const p = await pageWith()
  try {
    await p.type(field(p), 'sk-ant-the-key')
    await p.click(go(p))
    assert.equal(p.$('confirm').open, true)
    assert.match(p.$('confirm-text').textContent, /every sandbox/i)
    assert.match(p.$('confirm-text').textContent, /replaces/i)
    assert.deepEqual(posted(p), [], 'it was sent before it was confirmed')
    await p.confirm(false)
    assert.deepEqual(posted(p), [], 'it was sent after Cancel')
  } finally { p.close() }
})

test('confirmed, the key is sent, the field is cleared, and the sign-ins are looked at again', async () => {
  const p = await pageWith()
  try {
    const before = p.calls.filter((c) => c.path === '/api/signin').length
    await p.type(field(p), 'sk-ant-the-key')
    await p.click(go(p))
    await p.confirm(true)
    await p.settle()
    assert.deepEqual(posted(p), [{ key: 'sk-ant-the-key' }])
    assert.equal(field(p).value, '', 'the key is still in the field')
    assert.equal(p.calls.filter((c) => c.path === '/api/signin').length, before + 1)
    assert.doesNotMatch(p.$('toast').textContent, /sk-ant/)
  } finally { p.close() }
})

test('an empty field asks nothing', async () => {
  const p = await pageWith()
  try {
    await p.click(go(p))
    assert.equal(p.$('confirm').open, false)
    assert.deepEqual(posted(p), [])
  } finally { p.close() }
})

test('a key the host could not keep says why, and is not left shown', async () => {
  const p = await pageWith({ answer: { status: 500, body: { error: 'sbx secret set anthropic: exit status 1: invalid key' } } })
  try {
    await p.type(field(p), 'sk-ant-bad')
    await p.click(go(p))
    await p.confirm(true)
    await p.settle()
    assert.match(p.$('toast').textContent, /invalid key/)
    assert.equal(field(p).value, '')
  } finally { p.close() }
})
