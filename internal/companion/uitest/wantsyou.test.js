// Tests of the Admin tab's cards that ask for the user, in jsdom (see page.js),
// run with page.test.js. A card is ringed in the page's colour when it is why
// the tab has a dot, so the user needn't hunt for it among the others.
'use strict'
const test = require('node:test')
const assert = require('node:assert/strict')
const { openPage } = require('./page.js')

const ringed = (p, id) => p.$(id).classList.contains('wants-you')

function adminPage({ upgrades = {}, requests = [], sync = null } = {}) {
  const routes = {
    '/api/state': { sandbox: 'box', sandboxTabs: true, admin: true, attention: {} },
    '/api/admin': { status: { exists: true, running: true }, upgrades },
    '/api/packages': { requests, config: null },
    '/api/packages/sync': sync || { status: 404, body: { error: 'no sync' } },
  }
  return openPage({ routes, storage: { 'vibe-companion-tab': 'admin' } })
}

const someSync = (extra) => Object.assign({
  count: 3, actionable: 3, running: false, skipped: 0, problems: [],
  missing: [{ framework: 'net8.0', packages: [{ id: 'Dapper', version: '2.1.86' }] }],
}, extra)

// The packages are one card: the agent's requests, and what the IDE can't see. It's
// ringed when either wants the user.
const CARD = 'admin-packages-all'

test('the packages card is ringed when a fetch could bring packages the IDE can\'t see', async () => {
  const p = await adminPage({ sync: someSync() })
  try { assert.equal(ringed(p, CARD), true) } finally { p.close() }
})

test('the packages card is not ringed with nothing to fetch, nor while fetching, nor for what no feed has', async () => {
  for (const [name, answer] of [
    ['nothing missing', someSync({ count: 0, actionable: 0, missing: [] })],
    ['fetching', someSync({ running: true, actionable: 0 })],
    ['only what no feed has', someSync({ actionable: 0 })],
  ]) {
    const p = await adminPage({ sync: answer })
    try { assert.equal(ringed(p, CARD), false, name) } finally { p.close() }
  }
})

test('the packages card is ringed while a request is undecided, and not when all are approved', async () => {
  const request = (id, extra) => Object.assign({ key: id, ecosystem: 'nuget', id, version: '1.0.0', framework: 'net8.0', note: 'new' }, extra)
  let p = await adminPage({ requests: [request('A.Lib'), request('B.Lib', { approved: true })] })
  try { assert.equal(ringed(p, CARD), true) } finally { p.close() }
  p = await adminPage({ requests: [request('B.Lib', { approved: true })] })
  try { assert.equal(ringed(p, CARD), false) } finally { p.close() }
  p = await adminPage({ requests: [] })
  try { assert.equal(ringed(p, CARD), false) } finally { p.close() }
})

test('the packages card is ringed for a request even when there is nothing for the IDE, and the other way round', async () => {
  const request = { key: 'k', ecosystem: 'nuget', id: 'A.Lib', version: '1.0.0', framework: 'net8.0', note: 'new' }
  let p = await adminPage({ requests: [request], sync: someSync({ count: 0, actionable: 0, missing: [] }) })
  try { assert.equal(ringed(p, CARD), true, 'a request alone') } finally { p.close() }
  p = await adminPage({ requests: [], sync: someSync() })
  try { assert.equal(ringed(p, CARD), true, 'the IDE alone') } finally { p.close() }
})

test('the upgrades card is ringed when newer releases are found, and not when up to date', async () => {
  let p = await adminPage({ upgrades: { checked: true, found: ['vibe 0.2 is out'] } })
  try { assert.equal(ringed(p, 'admin-upgrades-card'), true) } finally { p.close() }
  p = await adminPage({ upgrades: { checked: true, found: [] } })
  try { assert.equal(ringed(p, 'admin-upgrades-card'), false) } finally { p.close() }
})

test('the ring is the page\'s own colour: the agent\'s theme when it has one, else the accent', () => {
  const css = require('node:fs').readFileSync(require('node:path').join(__dirname, '..', 'ui', 'app.css'), 'utf8')
  const rule = css.match(/\.card\.wants-you\s*\{([^}]*)\}/)
  assert.ok(rule, 'a .card.wants-you rule')
  // --theme is the agent's colour, or the accent where there is none (see :root).
  assert.match(rule[1], /var\(--theme\)/)
})
