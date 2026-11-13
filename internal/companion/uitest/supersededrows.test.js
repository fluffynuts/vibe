// A host the log has twice, once blocked and later allowed, shows the older row as history: its status struck through, the row muted,
// no button (what it offers is not what is so), and not counted. The server says which (policy.Resolve); the page draws it.
// In jsdom (see page.js), run with page.test.js.
'use strict'
const test = require('node:test')
const assert = require('node:assert/strict')
const { openPage } = require('./page.js')

const entry = (host, decision, lastSeen, over = {}) => ({ host, decision, hits: 1, lastSeen, rule: '', ...over })
const entries = () => [
  entry('a.org:443', 'blocked', '2026-11-12T10:00:00Z', { superseded: true }),
  entry('a.org:443', 'allowed', '2026-11-12T11:00:00Z'),
  entry('b.org:443', 'blocked', '2026-11-12T12:00:00Z'),
]

async function logPage(list, routes = {}) {
  const p = await openPage({
    routes: {
      '/api/state': { sandbox: 'box', sandboxTabs: true, conversation: true, attention: {} },
      '/api/conversation': { events: [], last: 0, running: false },
      '/api/log': () => ({ entries: list }),
      ...routes,
    },
    storage: { 'vibe-companion-tab': 'log' },
  })
  await p.poll()
  return p
}

const rows = (p) => [...p.$('log-table').tBodies[0].querySelectorAll('tr.entry')]
const rowFor = (p, host, word) => rows(p).find((r) => r.textContent.includes(host) && r.textContent.includes(word))

test('an old row is marked superseded, its status struck through, and has no button', async () => {
  const p = await logPage(entries())
  try {
    const old = rowFor(p, 'a.org', 'Blocked')
    assert.ok(old.classList.contains('superseded'))
    assert.equal(old.querySelector('button'), null, 'a row that says what was so offers to change it')
    assert.ok(old.querySelector('.status').classList.contains('superseded'), 'the status is not marked for striking through')
    const current = rowFor(p, 'a.org', 'Allowed')
    assert.equal(current.classList.contains('superseded'), false)
    assert.ok(current.querySelector('button'))
    assert.ok(rowFor(p, 'b.org', 'Blocked').querySelector('button'), 'a host with one row, blocked, lost its button')
  } finally { p.close() }
})

test('the old row says why it is struck through', async () => {
  const p = await logPage(entries())
  try {
    assert.match(rowFor(p, 'a.org', 'Blocked').querySelector('.status').title, /later|since|now/i)
  } finally { p.close() }
})

test('the counts are of the rows that are so now, and the old ones are said once', async () => {
  const p = await logPage(entries())
  try {
    const said = p.$('log-summary').textContent.replace(/\s+/g, ' ')
    assert.match(said, /3 total/)
    assert.match(said, /1 allowed/)
    assert.match(said, /1 blocked/, 'the old blocked row is counted as blocked')
    assert.match(said, /1 superseded/)
  } finally { p.close() }
})

test('with none superseded the summary does not mention it', async () => {
  const p = await logPage([entry('b.org:443', 'blocked', '2026-11-12T12:00:00Z')])
  try {
    assert.doesNotMatch(p.$('log-summary').textContent, /superseded/)
  } finally { p.close() }
})

test("an old row is still shown under its own status's filter", async () => {
  const p = await logPage(entries())
  try {
    await p.click(p.$('log-filter').querySelector('[data-filter=blocked]'))
    assert.ok(rowFor(p, 'a.org', 'Blocked'), 'the old blocked row vanished under the Blocked filter')
    assert.equal(rowFor(p, 'a.org', 'Allowed'), undefined)
  } finally { p.close() }
})

test('an old row is not marked updating when its host is: it is not what is waited for', async () => {
  const p = await logPage(entries(), { '/api/allow': () => new Promise(() => {}), '/api/deny': () => new Promise(() => {}) })
  try {
    rowFor(p, 'a.org', 'Allowed').querySelector('button').click()
    await new Promise((resolve) => setImmediate(resolve))
    p.$('confirm').close('run')
    for (let i = 0; i < 30; i++) await new Promise((resolve) => setImmediate(resolve))
    assert.match(rowFor(p, 'a.org', 'Updating').textContent, /Updating…/)
    assert.ok(rowFor(p, 'a.org', 'Blocked'), 'the old row says updating too, or is gone')
    assert.equal(rows(p).filter((r) => /Updating/.test(r.textContent)).length, 1)
  } finally { p.close() }
})
