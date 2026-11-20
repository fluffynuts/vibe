// The Conversation tab's status, in a balloon from its own info button left of New conversation (docs/plan-status-balloon.md): what
// resolves itself pops up and closes itself after 5 s, with a pulsing dot while background work runs; what needs the user stays, with a
// steady amber dot. In jsdom (see page.js), run with page.test.js.
'use strict'
const test = require('node:test')
const assert = require('node:assert/strict')
const { openPage } = require('./page.js')

const CLOSE_AFTER_MS = 5000

// convoPage opens the Conversation tab on a conversation answering agent (changed through at.agent), with the page's 5 s timers held
// back until the test runs them (at.closeTimers()). at.stopAnswer, when set, is how vibe answers a stop.
async function convoPage(agent) {
  const at = { agent, held: [] }
  at.closeTimers = () => { const fns = at.held.splice(0); for (const fn of fns) fn() }
  const p = await openPage({
    routes: {
      '/api/state': { sandbox: 'box', sandboxTabs: true, conversation: true, attention: {} },
      '/api/conversation': () => Object.assign({ events: [], last: 0 }, at.agent),
      '/api/conversation/stop': () => at.stopAnswer || { stopped: true },
      '/api/packages': { requests: [], config: null },
      '/api/packages/sync/check': { count: 0, actionable: 0, running: false, auto: false, missing: [] },
      '/api/log': { entries: [] },
    },
    storage: { 'vibe-companion-tab': 'conversation' },
    before: (w) => {
      const real = w.setTimeout
      w.setTimeout = (fn, ms, ...rest) => ms === CLOSE_AFTER_MS ? (at.held.push(() => fn(...rest)), 0) : real(fn, ms, ...rest)
    },
  })
  await p.every('pollConversation')
  return Object.assign(p, { at })
}

const button = (p) => p.$('status-info')
const balloon = (p) => p.$('status-balloon')
const shown = (p) => !balloon(p).hidden
const said = (p) => balloon(p).textContent
const dot = (p) => button(p).classList.contains('warn') ? 'warn' : button(p).classList.contains('busy') ? 'busy' : ''
const poll = async (p, agent) => { p.at.agent = agent; await p.every('pollConversation') }

const npmTest = { task_id: 't1', type: 'local_bash', description: 'npm test' }
const watch = { task_id: 't2', type: 'local_bash', description: 'watch the build' }

test('the info button sits just left of New conversation, and nothing else says the status in the bar', async () => {
  const p = await convoPage({ running: false })
  try {
    assert.equal(button(p).nextElementSibling, p.$('convo-new'))
    assert.equal(p.$('convo-status'), null, 'the old status line is still there')
    assert.equal(shown(p), false, 'the balloon is open with nothing to say')
    assert.equal(dot(p), '')
  } finally { p.close() }
})

test('background work pops the balloon up, listing each task, and it closes itself after 5 s; the dot pulses while the work runs', async () => {
  const p = await convoPage({ running: false })
  try {
    await poll(p, { running: false, background: true, tasks: [npmTest, watch] })
    assert.ok(shown(p), 'background work did not pop the balloon up')
    assert.match(said(p), /npm test/)
    assert.match(said(p), /watch the build/)
    assert.match(said(p), /carries on/)
    assert.equal(dot(p), 'busy')
    assert.equal(p.at.held.length, 1, 'no 5 s close was set')
    p.at.closeTimers()
    assert.equal(shown(p), false, 'it did not close itself')
    assert.equal(dot(p), 'busy', 'the dot went with the balloon')
    assert.match(button(p).getAttribute('aria-label'), /2 things running in the background/)

    await poll(p, { running: false, background: true, tasks: [npmTest, watch] })
    assert.equal(shown(p), false, 'nothing changed, yet it popped up again')

    await poll(p, { running: false, background: true, tasks: [watch] })
    assert.ok(shown(p), 'a task finishing while another runs did not pop it up')
    assert.doesNotMatch(said(p), /npm test/)
    p.at.closeTimers()

    await poll(p, { running: false, background: false, tasks: [] })
    assert.equal(dot(p), '', 'the dot stayed after the work ended')
  } finally { p.close() }
})

test('opened from the button, the balloon stays until closed by the button, its ✕ or Escape, and says when there is nothing', async () => {
  const p = await convoPage({ running: false })
  try {
    await p.click('status-info')
    assert.ok(shown(p))
    assert.equal(p.at.held.length, 0, 'a balloon the user opened closes itself')
    assert.match(said(p), /Work the agent is doing in the background will appear here/)
    assert.equal(button(p).getAttribute('aria-expanded'), 'true')
    await p.click('status-info')
    assert.equal(shown(p), false, 'the button did not close it')
    await p.click('status-info')
    await p.click('status-balloon-close')
    assert.equal(shown(p), false, 'the ✕ did not close it')
    await p.click('status-info')
    balloon(p).dispatchEvent(new p.window.KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    assert.equal(shown(p), false, 'Escape did not close it')
  } finally { p.close() }
})

test('a warning pops up and stays, says "at least", and its count updating changes the text without opening it again', async () => {
  const p = await convoPage({ running: true })
  try {
    await poll(p, { running: true, quietMinutes: 5 })
    assert.ok(shown(p), 'the warning did not pop up')
    assert.match(said(p), /No output from the agent for at least 5 minutes/)
    assert.equal(p.at.held.length, 0, 'a warning closes itself')
    assert.equal(dot(p), 'warn')
    assert.ok(balloon(p).querySelector('[role="alert"]').textContent.includes('at least 5 minutes'), 'not announced as an alert')

    await p.click('status-balloon-close')
    await poll(p, { running: true, quietMinutes: 6 })
    assert.equal(shown(p), false, 'the minute count re-opened it')
    assert.equal(dot(p), 'warn', 'a warning still true lost its dot when closed')
    await p.click('status-info')
    assert.match(said(p), /at least 6 minutes/)
    await p.click('status-info')

    await poll(p, { running: true, quietMinutes: 0 })
    assert.equal(dot(p), '', 'the warning outlived what it warned of')
  } finally { p.close() }
})

test('a warning that clears itself takes the balloon it was alone in with it', async () => {
  const p = await convoPage({ running: true })
  try {
    await poll(p, { running: true, quietMinutes: 5 })
    assert.ok(shown(p))
    await poll(p, { running: true, quietMinutes: 0 })
    assert.equal(shown(p), false, 'the balloon stayed open saying nothing that is true')
  } finally { p.close() }
})

test('"Stopping the agent…" is listed, but never pops the balloon up', async () => {
  const p = await convoPage({ running: true })
  try {
    await p.click('convo-stop')
    await poll(p, { running: true, stopState: 'stopping' })
    assert.equal(shown(p), false, 'Stopping popped the balloon up')
    await p.click('status-info')
    assert.match(said(p), /Stopping the agent/)
  } finally { p.close() }
})

test('what changed while the Conversation tab was away pops up on coming back to it, once', async () => {
  const p = await convoPage({ running: false })
  try {
    await p.click('tab-log')
    await poll(p, { running: false, background: true, tasks: [npmTest] })
    assert.equal(shown(p), false, 'it popped up out of sight')
    await p.click('tab-conversation')
    assert.ok(shown(p), 'coming back did not show what changed')
    p.at.closeTimers()
    await p.click('tab-log')
    await p.click('tab-conversation')
    assert.equal(shown(p), false, 'coming back with nothing new popped it up again')
  } finally { p.close() }
})

test('what changed while the window had no focus pops up when it has it again', async () => {
  const p = await convoPage({ running: false })
  try {
    let focused = false
    p.window.document.hasFocus = () => focused
    await poll(p, { running: false, background: true, tasks: [npmTest] })
    assert.equal(shown(p), false, 'it popped up in a window nobody is looking at')
    focused = true
    p.window.dispatchEvent(new p.window.Event('focus'))
    assert.ok(shown(p), 'focus coming back did not show what changed')
  } finally { p.close() }
})

const stopButton = (p) => [...balloon(p).querySelectorAll('button')].find((b) => /Stop/.test(b.textContent))

test('the balloon stops what runs in the background, there being no other way between turns', async () => {
  const p = await convoPage({ running: false })
  try {
    await p.click('status-info')
    assert.equal(stopButton(p), undefined, 'offered to stop with nothing running')
    await poll(p, { running: false, background: true, tasks: [npmTest, watch] })
    assert.ok(stopButton(p), 'nothing in the balloon stops the background work')
    await p.click(stopButton(p))
    assert.deepEqual(p.sent('/api/conversation/stop'), [{ tasks: true }])
    assert.ok(shown(p), 'stopping closed the balloon before the work had gone')
    await poll(p, { running: false, background: true, tasks: [npmTest, watch] })
    assert.equal(stopButton(p).textContent, 'Stopping…')
    assert.equal(stopButton(p).disabled, true, 'a stop on its way can be asked for again')
    await poll(p, { running: false, background: false, tasks: [] })
    assert.equal(stopButton(p), undefined, 'still offered to stop once nothing runs')
    assert.equal(dot(p), '')
  } finally { p.close() }
})

test('a stop of the background work that fails says so, and can be tried again', async () => {
  const p = await convoPage({ running: false })
  try {
    p.at.stopAnswer = { status: 500, body: { error: 'the agent did not answer' } }
    await p.click('status-info')
    await poll(p, { running: false, background: true, tasks: [npmTest] })
    await p.click(stopButton(p))
    assert.match(p.$('toast').textContent, /the agent did not answer/)
    assert.equal(stopButton(p).disabled, false, 'it cannot be tried again')
  } finally { p.close() }
})
