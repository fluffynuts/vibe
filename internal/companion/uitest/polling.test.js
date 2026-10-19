// Tests of how the page polls vibe, in jsdom (see page.js), run with page.test.js.
'use strict'
const test = require('node:test')
const assert = require('node:assert/strict')
const { openPage } = require('./page.js')

// A page whose /api/state answers when the test says: held is the answers
// still on their way.
async function slowStatePage() {
  const held = []
  let hold = false
  const routes = {
    '/api/state': () => {
      const answer = { sandbox: 'box', sandboxTabs: true, admin: true, conversation: true, attention: {} }
      if (!hold) return answer
      return new Promise((resolve) => held.push(() => resolve(answer)))
    },
    '/api/admin': { status: { exists: true, running: true }, upgrades: {} },
    '/api/packages': { requests: [], config: null },
    '/api/conversation': { events: [] },
  }
  const p = await openPage({ routes, storage: { 'vibe-companion-tab': 'admin' } })
  return Object.assign(p, {
    held,
    slow() { hold = true },
    // tick runs the page's poll without waiting for what it asks: a held
    // answer would never let the harness settle.
    async tickNow() { await p.every('tick').catch(() => {}) },
    stateCalls() { return p.calls.filter((c) => c.path === '/api/state').length },
  })
}

test('a poll is not started while the last of its kind has not been answered', async () => {
  const p = await slowStatePage()
  try {
    const before = p.stateCalls()
    p.slow()
    await p.tickNow()
    assert.equal(p.stateCalls(), before + 1, 'the first slow poll goes out')
    for (let i = 0; i < 5; i++) await p.tickNow()
    assert.equal(p.stateCalls(), before + 1, 'five more ticks, with the first still on its way, added none')
    assert.equal(p.held.length, 1)
  } finally { p.close() }
})

test('polling goes on once the held answer comes', async () => {
  const p = await slowStatePage()
  try {
    const before = p.stateCalls()
    p.slow()
    await p.tickNow()
    p.held.splice(0).forEach((release) => release())
    await p.settle()
    await p.tickNow()
    assert.equal(p.stateCalls(), before + 2, 'a poll after the answer came goes out')
  } finally { p.close() }
})

test("a slow poll of one kind doesn't hold up the others", async () => {
  const p = await slowStatePage()
  try {
    p.slow()
    await p.tickNow()
    const admins = p.calls.filter((c) => c.path === '/api/admin').length
    await p.tickNow()
    assert.ok(p.calls.filter((c) => c.path === '/api/admin').length > admins, 'the admin poll still goes out')
  } finally { p.close() }
})
