// The Admin tab's GitHub token card lists the other sandboxes whose token has expired or is about to (from vibe's records), and
// says that 'vibe --batch-git-token' renews several at once. In jsdom (see page.js), run with page.test.js.
'use strict'
const test = require('node:test')
const assert = require('node:assert/strict')
const { openPage } = require('./page.js')

const turns = async () => { for (let i = 0; i < 30; i++) await new Promise((resolve) => setImmediate(resolve)) }

async function adminWith(others) {
  const gitToken = { has: true, source: 'own', expiry: 'expires 2026-12-20 (in 30 days)', command: 'vibe --git-token /w/box', others }
  const p = await openPage({
    settled: false,
    routes: {
      '/api/state': { sandbox: 'box', sandboxTabs: true, conversation: true, admin: true, attention: {} },
      '/api/conversation': { events: [], last: 0, running: false },
      '/api/admin': { status: { exists: true, running: true, profile: 'p', guided: true, features: [], gitToken }, job: null, upgrades: {} },
      '/api/admin/instructions': { applies: true, agent: 'claude', files: [] },
      '/api/signin': { agent: 'claude', docker: { state: 'signed-in' }, agentSignIn: { state: 'signed-in' } },
      '/api/packages': { requests: [] },
      '/api/packages/sync': { count: 0 },
      '/api/theme': { choices: [] },
      '/api/log': { entries: [] },
    },
    storage: { 'vibe-companion-tab': 'admin' },
  })
  await turns()
  return p
}

test("the GitHub token card lists other sandboxes' tokens that have expired or are about to, and what renews several at once", async () => {
  const p = await adminWith([
    { name: 'api', expiry: 'expired 2026-11-18', expired: true },
    { name: 'web', expiry: 'expires 2026-11-22 (in 2 days)' },
  ])
  try {
    const list = p.$('admin-git-others')
    assert.equal(list.hidden, false, 'the list is hidden')
    const items = [...list.querySelectorAll('li')].map((li) => li.textContent)
    assert.deepEqual(items, ['api: expired 2026-11-18', 'web: expires 2026-11-22 (in 2 days)'])
    assert.ok(list.querySelector('li.bad-text'), 'an expired one is not marked as a problem')
    assert.match(list.textContent, /vibe --batch-git-token/)
  } finally { p.close() }
})

test('with no other token running out, the card says nothing of others', async () => {
  const p = await adminWith(undefined)
  try {
    assert.equal(p.$('admin-git-others').hidden, true)
  } finally { p.close() }
})
