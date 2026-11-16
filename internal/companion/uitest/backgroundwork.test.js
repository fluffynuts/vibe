// The agent lives while it has work: a prompt sent while it answers, or while it has something running in the background, goes to it and
// leaves that work running. A stop interrupts the turn and spares the background work, which is then offered to be stopped too; a stop
// that does not take effect can be forced. In jsdom (see page.js), run with page.test.js.
'use strict'
const test = require('node:test')
const assert = require('node:assert/strict')
const { openPage } = require('./page.js')

async function convoPage(agent, stop) {
  const at = { agent, sent: [], stops: [] }
  const p = await openPage({
    routes: {
      '/api/state': { sandbox: 'box', sandboxTabs: true, conversation: true, attention: {} },
      '/api/conversation': () => Object.assign({ events: [], last: 0 }, at.agent),
      '/api/conversation/send': (call) => { at.sent.push(call.body.prompt); return { ok: true, seq: 9 } },
      '/api/conversation/stop': (call) => { at.stops.push(call.body); return (stop && stop(call.body)) || { stopped: true } },
      '/api/packages': { requests: [], config: null },
      '/api/packages/sync/check': { count: 0, actionable: 0, running: false, auto: false, missing: [] },
    },
    storage: { 'vibe-companion-tab': 'conversation' },
  })
  await p.every('pollConversation')
  return Object.assign(p, { at })
}

const tests = [{ task_id: 't1', type: 'local_bash', description: 'npm test' }, { task_id: 't2', type: 'local_bash', description: 'watch the build' }]
const offer = (p) => p.window.document.querySelector('#convo-log .tasks-offer')
const button = (row, name) => [...row.querySelectorAll('button')].find((b) => b.textContent === name)

test('while the agent has work in the background a prompt goes without asking, and the status says the work carries on', async () => {
  const p = await convoPage({ running: false, background: true, tasks: tests })
  try {
    assert.match(p.$('convo-status').textContent, /2 things running in the background: "npm test", "watch the build"/)
    assert.match(p.$('convo-status').textContent, /carries on/)
    assert.doesNotMatch(p.$('convo-status').textContent, /ends it/)
    await p.type(p.$('convo-input'), 'something else, meanwhile')
    await p.click('convo-send')
    assert.equal(p.$('confirm').open, false, 'asked before sending a prompt that no longer ends anything')
    assert.deepEqual(p.at.sent, ['something else, meanwhile'])
    assert.equal(p.$('convo-input').value, '')
  } finally { p.close() }
})

test('while the agent answers, Send stays open and a prompt goes to it', async () => {
  const p = await convoPage({ running: true })
  try {
    assert.equal(p.$('convo-send').disabled, false, 'a prompt cannot be sent while the agent answers')
    await p.type(p.$('convo-input'), 'also this')
    await p.click('convo-send')
    assert.deepEqual(p.at.sent, ['also this'])
  } finally { p.close() }
})

test('each prompt in flight shows its own receipt, as the agent takes it', async () => {
  const p = await convoPage({ running: true, events: [{ seq: 1, kind: 'user', text: 'first', receipt: 'seen' }, { seq: 2, kind: 'user', text: 'second', receipt: 'sent' }], last: 2 })
  try {
    p.at.agent = { running: true, receipts: [{ seq: 1, state: 'seen' }, { seq: 2, state: 'delivered' }], last: 2 }
    await p.every('pollConversation')
    const second = [...p.window.document.querySelectorAll('.msg.user')].find((m) => m.textContent.includes('second'))
    assert.match(second.querySelector('.ticks').title, /Delivered/)
  } finally { p.close() }
})

test('a stop that has taken effect offers to stop what the agent still has running in the background, naming it', async () => {
  const p = await convoPage({ running: true })
  try {
    await p.click('convo-stop')
    assert.deepEqual(p.at.stops, [{}])
    p.at.agent = { running: false, background: true, tasks: tests }
    await p.every('pollConversation')
    const row = offer(p)
    assert.ok(row, 'nothing offered to stop the background work')
    assert.match(row.textContent, /still has 2 tasks running in the background: "npm test", "watch the build"/)
    await p.click(button(row, 'Stop them too'))
    assert.deepEqual(p.at.stops, [{}, { tasks: true }])
    assert.match(row.textContent, /Stopped what the agent had running/)
  } finally { p.close() }
})

test('leaving the background work running stops nothing more', async () => {
  const p = await convoPage({ running: true })
  try {
    await p.click('convo-stop')
    p.at.agent = { running: false, background: true, tasks: tests.slice(0, 1) }
    await p.every('pollConversation')
    const row = offer(p)
    assert.match(row.textContent, /still has a task running in the background: "npm test"/)
    await p.click(button(row, 'Leave them running'))
    assert.deepEqual(p.at.stops, [{}])
    assert.match(row.textContent, /Left what the agent has running/)
    await p.every('pollConversation')
    assert.equal(p.window.document.querySelectorAll('#convo-log .tasks-offer').length, 1, 'offered again')
  } finally { p.close() }
})

test('a stop with nothing left in the background offers nothing', async () => {
  const p = await convoPage({ running: true })
  try {
    await p.click('convo-stop')
    p.at.agent = { running: false, background: false, tasks: [] }
    await p.every('pollConversation')
    assert.equal(offer(p), null)
  } finally { p.close() }
})

test('the background work ending by itself is not offered to be stopped', async () => {
  const p = await convoPage({ running: false, background: true, tasks: tests })
  try {
    p.at.agent = { running: true, tasks: tests }
    await p.every('pollConversation')
    p.at.agent = { running: false, background: true, tasks: tests }
    await p.every('pollConversation')
    assert.equal(offer(p), null, 'offered to stop work the user never stopped the turn of')
  } finally { p.close() }
})

test('a stop that has not taken effect after a while can be forced, which ends the agent at once and offers nothing after', async () => {
  const p = await convoPage({ running: true })
  let now = 1_000_000
  p.window.Date.now = () => now
  try {
    p.at.agent = { running: true, stopState: '' }
    await p.click('convo-stop')
    p.at.agent = { running: true, stopState: 'stopping' }
    await p.every('pollConversation')
    assert.equal(p.$('convo-stop').disabled, true, 'Stop can be pressed again at once')
    assert.doesNotMatch(p.$('convo-stop').textContent, /Force/)
    now += 9000
    await p.every('pollConversation')
    assert.equal(p.$('convo-stop').disabled, false, 'a stuck stop cannot be forced')
    assert.match(p.$('convo-stop').textContent, /Force stop/)
    assert.match(p.$('convo-status').textContent, /has not stopped/)
    await p.click('convo-stop')
    assert.deepEqual(p.at.stops, [{}, { force: true }])
    p.at.agent = { running: false, background: false, tasks: [] }
    await p.every('pollConversation')
    assert.equal(offer(p), null)
    assert.match(p.$('convo-stop').textContent, /^Stop/)
  } finally { p.close() }
})

test('the result of a turn the user stopped is not shown as the agent failing', async () => {
  const p = await convoPage({ running: false, last: 3, events: [
    { seq: 1, kind: 'user', text: 'count', receipt: 'seen' },
    { seq: 2, kind: 'agent', text: 'stopped', agent: { type: 'result', subtype: 'error_during_execution', is_error: true, duration_ms: 4100 } },
    { seq: 3, kind: 'done', text: 'stopped' },
  ] })
  try {
    const log = p.$('convo-log').textContent
    assert.doesNotMatch(log, /reported an error/)
    assert.match(log, /Stopped\./)
    assert.equal(p.window.document.querySelectorAll('#convo-log .error').length, 0, 'an error is shown for a stop')
  } finally { p.close() }
})

test('a turn that failed on its own is still shown as an error', async () => {
  const p = await convoPage({ running: false, last: 2, events: [
    { seq: 1, kind: 'user', text: 'count', receipt: 'seen' },
    { seq: 2, kind: 'agent', agent: { type: 'result', subtype: 'error_during_execution', is_error: true } },
  ] })
  try {
    assert.match(p.$('convo-log').textContent, /reported an error/)
  } finally { p.close() }
})
