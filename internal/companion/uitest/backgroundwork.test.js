// A prompt sent while the agent has answered but left something running ends that work (the server supersedes the turn): the page asks first,
// and the user may still send it, to end the work and talk of something else. In jsdom (see page.js), run with page.test.js.
'use strict'
const test = require('node:test')
const assert = require('node:assert/strict')
const { openPage } = require('./page.js')

async function convoPage(agent) {
  const at = { agent, sent: [] }
  const p = await openPage({
    routes: {
      '/api/state': { sandbox: 'box', sandboxTabs: true, conversation: true, attention: {} },
      '/api/conversation': () => Object.assign({ events: [], last: 0 }, at.agent),
      '/api/conversation/send': (call) => { at.sent.push(call.body.prompt); return { ok: true } },
      '/api/packages': { requests: [], config: null },
      '/api/packages/sync/check': { count: 0, actionable: 0, running: false, auto: false, missing: [] },
    },
    storage: { 'vibe-companion-tab': 'conversation' },
  })
  await p.every('pollConversation')
  return Object.assign(p, { at })
}

const turns = async (n) => { for (let i = 0; i < n; i++) await new Promise((resolve) => setImmediate(resolve)) }

test('sending while the agent has work running asks first, and sends nothing until the user says so', async () => {
  const p = await convoPage({ running: false, background: true })
  try {
    await p.type(p.$('convo-input'), 'something else, meanwhile')
    p.$('convo-send').click()
    await turns(30)
    assert.equal(p.$('confirm').open, true, 'sent a prompt that ends the agent\'s work without asking')
    assert.match(p.$('confirm-title').textContent, /ends?|background|running/i)
    assert.match(p.$('confirm-text').textContent, /still has something running/)
    assert.match(p.$('confirm-text').textContent, /ends it/)
    assert.deepEqual(p.at.sent, [])
  } finally { p.close() }
})

test('Cancel sends nothing and keeps what was typed, and the agent keeps working', async () => {
  const p = await convoPage({ running: false, background: true })
  try {
    await p.type(p.$('convo-input'), 'something else, meanwhile')
    p.$('convo-send').click()
    await turns(30)
    p.$('confirm').close('cancel')
    await turns(30)
    assert.deepEqual(p.at.sent, [])
    assert.equal(p.$('convo-input').value, 'something else, meanwhile', 'the prompt was lost')
  } finally { p.close() }
})

test('the user can still send it, to end the work and talk of something else', async () => {
  const p = await convoPage({ running: false, background: true })
  try {
    await p.type(p.$('convo-input'), 'something else, meanwhile')
    p.$('convo-send').click()
    await turns(30)
    assert.match(p.$('confirm-run').textContent, /Send and end it/)
    p.$('confirm').close('run')
    await p.settle()
    assert.deepEqual(p.at.sent, ['something else, meanwhile'])
    assert.equal(p.$('convo-input').value, '')
    assert.doesNotMatch(p.$('toast').textContent, /Done/, 'a sent prompt was reported as a rule changed')
  } finally { p.close() }
})

test('Cancel is the default: Enter on the dialog does not end the work', async () => {
  const p = await convoPage({ running: false, background: true })
  try {
    await p.type(p.$('convo-input'), 'x')
    p.$('convo-send').click()
    await turns(30)
    const cancel = p.$('confirm').querySelector('[value=cancel]')
    assert.equal(cancel.autofocus, true)
    assert.equal(p.$('confirm-run').autofocus, false)
  } finally { p.close() }
})

test('with nothing running the prompt goes without asking', async () => {
  const p = await convoPage({ running: false, background: false })
  try {
    await p.type(p.$('convo-input'), 'hello')
    await p.click('convo-send')
    assert.equal(p.$('confirm').open, false)
    assert.deepEqual(p.at.sent, ['hello'])
  } finally { p.close() }
})

test('the status line about running work is in the colour of a warning', async () => {
  const p = await convoPage({ running: false, background: true })
  try {
    const status = p.$('convo-status')
    assert.match(status.textContent, /still running/)
    assert.ok(status.classList.contains('warn-text'), 'the line that matters looks like every other')
  } finally { p.close() }
})
