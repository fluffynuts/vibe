// Tests of the agent's activity light in the tab bar, in jsdom (see page.js),
// run with page.test.js.
'use strict'
const test = require('node:test')
const assert = require('node:assert/strict')
const { openPage } = require('./page.js')

// lightPage is a page for a sandbox whose agent can be talked to, and is doing
// what at.agent says: { running, background, quietMinutes, compacting }.
function lightPage(agent, { conversation = true, tab = 'rules', usage = false } = {}) {
  const at = { agent }
  const routes = {
    '/api/state': () => ({ sandbox: 'box', sandboxTabs: true, conversation, usage, attention: {} }),
    '/api/conversation': () => Object.assign({ events: [], last: 0 }, at.agent),
    '/api/usage': { state: 'stopped' },
    '/api/rules': { rules: [] },
    '/api/log': { entries: [] },
  }
  return openPage({ routes, storage: { 'vibe-companion-tab': tab } }).then(async (p) => {
    await p.every('pollConversation')
    return Object.assign(p, { at })
  })
}

const lit = (p) => p.$('agent-light').classList.contains('on')

test('a page whose agent can\'t be talked to has no light', async () => {
  const p = await lightPage({}, { conversation: false })
  try { assert.equal(p.$('agent-light').hidden, true) } finally { p.close() }
})

test('an idle agent is an unlit light that says what it is for', async () => {
  const p = await lightPage({ running: false })
  try {
    assert.equal(p.$('agent-light').hidden, false)
    assert.equal(lit(p), false)
    assert.match(p.$('agent-light').title, /working/)
    assert.match(p.$('agent-light').title, /idle|not working|off/i)
  } finally { p.close() }
})

test('the light is on while the agent answers, whichever tab is showing', async () => {
  const p = await lightPage({ running: true }, { tab: 'fs' })
  try {
    assert.equal(lit(p), true)
    assert.ok(p.$('agent-light').classList.contains('busy'))
    assert.match(p.$('agent-light').title, /working on a prompt/)
  } finally { p.close() }
})

test('the light stays on for what the agent left running after it answered, and says so', async () => {
  const p = await lightPage({ running: false, background: true })
  try {
    assert.equal(lit(p), true)
    assert.ok(p.$('agent-light').classList.contains('background'))
    assert.ok(!p.$('agent-light').classList.contains('busy'))
    assert.match(p.$('agent-light').title, /left something running/)
  } finally { p.close() }
})

test('it goes off when the agent is done, and on again with the next prompt', async () => {
  const p = await lightPage({ running: true })
  try {
    assert.equal(lit(p), true)
    p.at.agent = { running: false }
    await p.every('pollConversation')
    assert.equal(lit(p), false)
    p.at.agent = { running: true }
    await p.every('pollConversation')
    assert.equal(lit(p), true)
  } finally { p.close() }
})

test('compacting and a long quiet are said in the tooltip', async () => {
  let p = await lightPage({ running: true, compacting: true })
  try { assert.match(p.$('agent-light').title, /compacting/) } finally { p.close() }
  p = await lightPage({ running: true, quietMinutes: 7 })
  try {
    assert.match(p.$('agent-light').title, /no output for 7 minutes/i)
    assert.ok(p.$('agent-light').classList.contains('quiet'), 'a light that might mean stuck looks different')
  } finally { p.close() }
})

test('the light is in the tab bar, outside the tabs, to the left of the CPU and memory bars', async () => {
  const p = await lightPage({ running: false }, { usage: true })
  try {
    assert.ok(p.$('tabbar').contains(p.$('agent-light')))
    assert.ok(!p.$('tabs').contains(p.$('agent-light')))
    const light = p.$('agent-light'), usage = p.$('usage')
    assert.equal(light.parentNode, usage.parentNode, 'together at the bar\'s end')
    assert.ok(light.compareDocumentPosition(usage) & p.window.Node.DOCUMENT_POSITION_FOLLOWING, 'before the bars')
  } finally { p.close() }
})

test('a screen reader is told what the light says', async () => {
  const p = await lightPage({ running: true })
  try {
    const light = p.$('agent-light')
    assert.equal(light.getAttribute('role'), 'img')
    assert.equal(light.getAttribute('aria-label'), light.title)
  } finally { p.close() }
})
