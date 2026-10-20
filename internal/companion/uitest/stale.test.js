// Tests of the notice that the packages the user's cache has are still named in the sandbox's
// own cache by the files it generated, so the IDE can't find them: shown until the files are
// generated again, with a button that asks the agent to do that. In jsdom (see page.js), run with
// page.test.js.
'use strict'
const test = require('node:test')
const assert = require('node:assert/strict')
const { openPage } = require('./page.js')

const syncBody = (extra) => Object.assign({ count: 0, actionable: 0, running: false, auto: false, missing: [], stale: 0, problems: [], skipped: 0 }, extra)

async function stalePage({ stale = 2, running = false, tab = 'conversation', sync } = {}) {
  const at = { sent: [], stale, running }
  const routes = {
    '/api/state': () => ({ sandbox: 'box', sandboxTabs: true, admin: true, conversation: true, attention: {}, packagesStale: at.stale }),
    '/api/conversation': () => ({ events: [], last: 0, running: at.running }),
    '/api/conversation/send': (call) => { at.sent.push(call.body.prompt); return { ok: true } },
    '/api/packages': { requests: [], config: null },
    '/api/packages/sync': () => sync || syncBody({ stale: at.stale }),
    '/api/packages/sync/check': () => syncBody({ stale: at.stale }),
  }
  const p = await openPage({ routes, storage: { 'vibe-companion-tab': tab } })
  if (running) await p.every('pollConversation')
  return Object.assign(p, { at })
}

const shown = (p, id) => !p.$(id).hidden

test('the conversation says the IDE can’t find packages the cache has, until the files are generated again', async () => {
  const p = await stalePage({ stale: 2 })
  try {
    assert.equal(shown(p, 'convo-stale'), true)
    assert.match(p.$('convo-stale-text').textContent, /2 packages/)
    assert.match(p.$('convo-stale-text').textContent, /IDE/)
    assert.match(p.$('convo-stale-text').textContent, /restore in your IDE/, 'the way that needs no agent is said too')
    p.at.stale = 0
    await p.poll()
    assert.equal(shown(p, 'convo-stale'), false)
  } finally { p.close() }
})

test('with none stale there is no notice', async () => {
  const p = await stalePage({ stale: 0 })
  try { assert.equal(shown(p, 'convo-stale'), false) } finally { p.close() }
})

test('a single package is said in the singular', async () => {
  const p = await stalePage({ stale: 1 })
  try { assert.match(p.$('convo-stale-text').textContent, /^1 package\b.*\bit\b/) } finally { p.close() }
})

test('the button asks the agent to tidy up, and sends it at once: the click is the user’s choice', async () => {
  const p = await stalePage({ stale: 2 })
  try {
    await p.click('convo-stale-ask')
    assert.equal(p.at.sent.length, 1)
    assert.match(p.at.sent[0], /vibe-nuget status/)
    assert.match(p.at.sent[0], /tidy/)
  } finally { p.close() }
})

test('while the agent is working the button leaves the message in the box, for the user to send', async () => {
  const p = await stalePage({ stale: 2, running: true })
  try {
    await p.click('convo-stale-ask')
    assert.deepEqual(p.at.sent, [])
    assert.match(p.$('convo-input').value, /vibe-nuget status/)
  } finally { p.close() }
})

test('while the agent is working the button doesn’t replace what the user was typing', async () => {
  const p = await stalePage({ stale: 2, running: true })
  try {
    await p.type(p.$('convo-input'), 'something else')
    await p.click('convo-stale-ask')
    assert.equal(p.$('convo-input').value, 'something else')
    assert.deepEqual(p.at.sent, [])
  } finally { p.close() }
})

test('the Admin tab’s card says so too, instead of saying the IDE can find everything', async () => {
  const p = await stalePage({ stale: 3, tab: 'admin' })
  try {
    assert.equal(shown(p, 'admin-sync-stale'), true)
    assert.match(p.$('admin-sync-stale').textContent, /3 packages/)
    assert.doesNotMatch(p.$('admin-sync-summary').textContent, /your IDE can find it/)
  } finally { p.close() }
})

test('the Admin tab’s button goes to the conversation and asks the agent', async () => {
  const p = await stalePage({ stale: 3, tab: 'admin' })
  try {
    await p.click('admin-sync-stale-ask')
    assert.equal(p.at.sent.length, 1)
    assert.match(p.at.sent[0], /tidy/)
    assert.equal(p.$('panel-conversation').hidden, false)
  } finally { p.close() }
})

test('the Admin tab says the IDE can find everything when nothing is stale', async () => {
  const p = await stalePage({ stale: 0, tab: 'admin' })
  try {
    assert.equal(shown(p, 'admin-sync-stale'), false)
    assert.match(p.$('admin-sync-summary').textContent, /your IDE can find it/)
  } finally { p.close() }
})

test('a message the user was typing is still in the box after the button sends its own', async () => {
  const p = await stalePage({ stale: 2 })
  try {
    await p.type(p.$('convo-input'), 'something else')
    await p.click('convo-stale-ask')
    assert.equal(p.at.sent.length, 1)
    assert.equal(p.$('convo-input').value, 'something else')
  } finally { p.close() }
})

test('the Admin tab says nothing of stale files while there is still something to fetch, or a fetch is running', async () => {
  for (const extra of [{ count: 2, actionable: 2 }, { running: true }]) {
    const p = await stalePage({ stale: 3, tab: 'admin', sync: syncBody(Object.assign({ stale: 3 }, extra)) })
    try { assert.equal(shown(p, 'admin-sync-stale'), false, JSON.stringify(extra)) } finally { p.close() }
  }
})
