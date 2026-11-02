// The Sandbox card on the Admin tab, when the sandbox can't be read: while the page's own action is the reason (a rebuild,
// a re-compose), the card says so, as the action's card beside it does, rather than the raw "didn't answer".
'use strict'
const test = require('node:test')
const assert = require('node:assert/strict')
const { openPage } = require('./page.js')

const RAW = "sbx didn't answer within 20s, so the sandbox's state is unknown"

async function pageWith(job) {
  const p = await openPage({
    routes: {
      '/api/state': { sandbox: 'box', sandboxTabs: true, conversation: true, admin: true, attention: {} },
      '/api/conversation': { events: [], last: 0, running: false },
      '/api/admin': { status: { exists: true, running: true, profile: 'p', error: RAW }, job, upgrades: {} },
      '/api/log': { entries: [] },
    },
    storage: { 'vibe-companion-tab': 'admin' },
  })
  await p.poll()
  return p
}

const said = (p) => p.$('admin-error').textContent

for (const [kind, words] of [['recompose', 're-composing'], ['reinit', 'rebuilding'], ['restore', 'restoring a backup'], ['update', 'updating'], ['delete', 'deleting']]) {
  test(`while ${kind} is under way the sandbox is said to be unavailable for it`, async () => {
    const p = await pageWith({ kind, running: true, lines: [], started: 1 })
    try {
      assert.ok(!p.$('admin-error').hidden)
      assert.match(said(p), new RegExp('unavailable while ' + words))
      assert.doesNotMatch(said(p), /didn't answer/)
    } finally { p.close() }
  })
}

test('with no action under way, the card says what sbx said', async () => {
  const p = await pageWith(null)
  try { assert.equal(said(p), RAW) } finally { p.close() }
})

test('once the action has ended, the card says what sbx said', async () => {
  const p = await pageWith({ kind: 'recompose', running: false, lines: [], started: 1, ended: 2 })
  try { assert.equal(said(p), RAW) } finally { p.close() }
})

test('an action that leaves the sandbox alone is not blamed', async () => {
  const p = await pageWith({ kind: 'memories', running: true, lines: [], started: 1 })
  try { assert.equal(said(p), RAW) } finally { p.close() }
})

// What sbx last said ("running") is not what is so while the page is taking the sandbox away.
test('the pill says what is being done to the sandbox, not that it is running, whether or not sbx answered', async () => {
  for (const status of [{ exists: true, running: true, profile: 'p', error: RAW }, { exists: true, running: true, profile: 'p' }]) {
    const p = await openPage({
      routes: {
        '/api/state': { sandbox: 'box', sandboxTabs: true, conversation: true, admin: true, attention: {} },
        '/api/conversation': { events: [], last: 0, running: false },
        '/api/admin': { status, job: { kind: 'recompose', running: true, lines: [], started: 1 }, upgrades: {} },
        '/api/log': { entries: [] },
      },
      storage: { 'vibe-companion-tab': 'admin' },
    })
    await p.poll()
    try {
      assert.equal(p.$('admin-state').textContent, 're-composing')
      assert.ok(p.$('admin-state').classList.contains('warn'))
      assert.equal(p.$('admin-error').hidden, !status.error, 'the message follows whether sbx answered')
    } finally { p.close() }
  }
})

test('the pill goes back to what sbx says once the action has ended', async () => {
  const p = await pageWith({ kind: 'recompose', running: false, lines: [], started: 1, ended: 2 })
  try { assert.equal(p.$('admin-state').textContent, 'running') } finally { p.close() }
})

test('the pill says running while an action that leaves the sandbox alone is under way', async () => {
  const p = await openPage({
    routes: {
      '/api/state': { sandbox: 'box', sandboxTabs: true, conversation: true, admin: true, attention: {} },
      '/api/conversation': { events: [], last: 0, running: false },
      '/api/admin': { status: { exists: true, running: true, profile: 'p' }, job: { kind: 'memories', running: true, lines: [], started: 1 }, upgrades: {} },
      '/api/log': { entries: [] },
    },
    storage: { 'vibe-companion-tab': 'admin' },
  })
  await p.poll()
  try { assert.equal(p.$('admin-state').textContent, 'running') } finally { p.close() }
})

test('the pill says running when sbx did not answer but nothing here is the reason', async () => {
  const p = await pageWith(null)
  try { assert.equal(p.$('admin-state').textContent, 'running') } finally { p.close() }
})
