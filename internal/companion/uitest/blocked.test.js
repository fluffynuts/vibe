// Tests of the Network log tab's dot for newly blocked hosts, in jsdom (see
// page.js), run with page.test.js.
'use strict'
const test = require('node:test')
const assert = require('node:assert/strict')
const { openPage } = require('./page.js')

// blockedPage is a page whose sandbox has blocked what at.blocked says (null:
// not read yet), on the tab given. storage is the browser's, which a test can
// keep across pages.
function blockedPage(blocked, { storage = {}, tab = 'rules' } = {}) {
  const at = { blocked }
  const routes = {
    '/api/state': () => ({ sandbox: 'box', sandboxTabs: true, attention: {}, blockedHosts: at.blocked }),
    '/api/log': { entries: [] },
    '/api/rules': { rules: [] },
  }
  return openPage({ routes, storage: Object.assign({ 'vibe-companion-tab': tab }, storage) }).then((p) => Object.assign(p, { at }))
}

const dot = (p) => {
  const tab = p.$('tab-log')
  return tab.querySelector('.tab-dot') ? tab.title : null
}

test('the first visit takes what is already blocked as seen: no dot, however long the history', async () => {
  const p = await blockedPage(['a.example', 'b.example'])
  try {
    assert.equal(dot(p), null)
    await p.poll()
    assert.equal(dot(p), null)
  } finally { p.close() }
})

test('a host blocked after that raises the dot, naming it', async () => {
  const p = await blockedPage(['a.example'])
  try {
    p.at.blocked = ['a.example', 'new.example']
    await p.poll()
    assert.match(dot(p), /1 new blocked host: new\.example/)
  } finally { p.close() }
})

test('several are counted, and named up to a few', async () => {
  const p = await blockedPage([])
  try {
    p.at.blocked = ['a.example', 'b.example', 'c.example', 'd.example', 'e.example']
    await p.poll()
    assert.match(dot(p), /5 new blocked hosts: a\.example, b\.example, c\.example and 2 more/)
  } finally { p.close() }
})

test('opening the Network log tab clears the dot, and it stays clear', async () => {
  const p = await blockedPage(['a.example'])
  try {
    p.at.blocked = ['a.example', 'new.example']
    await p.poll()
    assert.ok(dot(p))
    await p.click('tab-log')
    assert.equal(dot(p), null)
    await p.poll()
    assert.equal(dot(p), null, 'seen, and left blocked')
  } finally { p.close() }
})

test('a host that arrives while the Network log tab is open is being looked at, so no dot', async () => {
  const p = await blockedPage(['a.example'], { tab: 'log' })
  try {
    p.at.blocked = ['a.example', 'new.example']
    await p.poll()
    assert.equal(dot(p), null)
    await p.click('tab-rules')
    await p.poll()
    assert.equal(dot(p), null, 'and it was seen')
  } finally { p.close() }
})

test('what was seen and left blocked is not news to the next page load', async () => {
  const first = await blockedPage(['a.example'])
  let kept
  try {
    first.at.blocked = ['a.example', 'seen.example']
    await first.poll()
    await first.click('tab-log')
    kept = { 'vibe-companion-blocked-seen': first.window.localStorage.getItem('vibe-companion-blocked-seen') }
  } finally { first.close() }
  const second = await blockedPage(['a.example', 'seen.example'], { storage: kept })
  try {
    assert.equal(dot(second), null)
  } finally { second.close() }
})

test('a host that stops being blocked, and is blocked again, is news again', async () => {
  const p = await blockedPage(['a.example', 'b.example'])
  try {
    p.at.blocked = ['a.example'] // b was allowed
    await p.poll()
    assert.equal(dot(p), null)
    p.at.blocked = ['a.example', 'b.example'] // and blocked again
    await p.poll()
    assert.match(dot(p), /1 new blocked host: b\.example/)
  } finally { p.close() }
})

test('not knowing yet what is blocked is not "nothing is", and seeds nothing', async () => {
  const p = await blockedPage(null)
  try {
    assert.equal(dot(p), null)
    assert.equal(p.window.localStorage.getItem('vibe-companion-blocked-seen'), null, 'nothing was taken as seen')
    p.at.blocked = ['a.example', 'b.example']
    await p.poll()
    assert.equal(dot(p), null, 'the first answer is the first visit: it seeds')
    p.at.blocked = ['a.example', 'b.example', 'c.example']
    await p.poll()
    assert.match(dot(p), /1 new blocked host: c\.example/)
  } finally { p.close() }
})

test('the dot goes when the hosts are no longer blocked', async () => {
  const p = await blockedPage([])
  try {
    p.at.blocked = ['a.example']
    await p.poll()
    assert.ok(dot(p))
    p.at.blocked = []
    await p.poll()
    assert.equal(dot(p), null)
  } finally { p.close() }
})

test('the other tabs\' dots are left as they are', async () => {
  const p = await openPage({
    routes: { '/api/state': () => ({ sandbox: 'box', sandboxTabs: true, admin: true, attention: { admin: ['Upgrades available'] }, blockedHosts: ['a.example'] }),
      '/api/admin': { status: {}, upgrades: {} }, '/api/log': { entries: [] } },
    storage: { 'vibe-companion-blocked-seen': '[]', 'vibe-companion-tab': 'admin' },
  })
  try {
    assert.match(p.$('tab-admin').title, /Upgrades available/)
    assert.match(dot(p), /1 new blocked host: a\.example/)
  } finally { p.close() }
})
