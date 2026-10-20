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
    '/api/state': () => ({ sandbox: 'box', sandboxTabs: true, conversation: true, attention: {}, blockedHosts: at.blocked }),
    '/api/conversation': { events: [], last: 0, running: false },
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

// The conversation says so too when a host is newly blocked, with a way to the Network log tab, where
// it can be unblocked; and a notice dismissed stays dismissed for those hosts, however often they are
// blocked again (a host that is called every few minutes isn't news each time).
const notice = (p) => (p.$('convo-blocked').hidden ? null : p.$('convo-blocked-text').textContent)

test('a newly blocked host is said on the conversation, with where to unblock it', async () => {
  const p = await blockedPage(['a.example'])
  try {
    assert.equal(notice(p), null)
    p.at.blocked = ['a.example', 'new.example']
    await p.poll()
    assert.match(notice(p), /new\.example/)
    assert.match(notice(p), /Network log/)
  } finally { p.close() }
})

test('several hosts are counted and named up to a few', async () => {
  const p = await blockedPage([])
  try {
    p.at.blocked = ['a.example', 'b.example', 'c.example', 'd.example', 'e.example']
    await p.poll()
    assert.match(notice(p), /5 hosts/)
    assert.match(notice(p), /a\.example, b\.example, c\.example and 2 more/)
  } finally { p.close() }
})

test('the button goes to the Network log tab, which is where it is seen to', async () => {
  const p = await blockedPage(['a.example'], { tab: 'conversation' })
  try {
    p.at.blocked = ['a.example', 'new.example']
    await p.poll()
    assert.equal(p.$('panel-log').hidden, true, 'on the conversation to begin with')
    await p.click('convo-blocked-open')
    assert.equal(p.$('panel-log').hidden, false)
    assert.equal(notice(p), null)
    assert.equal(dot(p), null)
  } finally { p.close() }
})

test('dismissing takes the notice and the dot away, and neither comes back for those hosts', async () => {
  const p = await blockedPage(['a.example'])
  try {
    p.at.blocked = ['a.example', 'noisy.example']
    await p.poll()
    await p.click('convo-blocked-dismiss')
    assert.equal(notice(p), null)
    assert.equal(dot(p), null)
    p.at.blocked = ['a.example'] // it stopped being blocked
    await p.poll()
    p.at.blocked = ['a.example', 'noisy.example'] // and was blocked again, as it is every few minutes
    await p.poll()
    assert.equal(notice(p), null, 'no reminder')
    assert.equal(dot(p), null)
  } finally { p.close() }
})

test('a host that arrives after others were dismissed is still news, and only it is named', async () => {
  const p = await blockedPage(['a.example'])
  try {
    p.at.blocked = ['a.example', 'noisy.example']
    await p.poll()
    await p.click('convo-blocked-dismiss')
    p.at.blocked = ['a.example', 'noisy.example', 'other.example']
    await p.poll()
    assert.match(notice(p), /other\.example/)
    assert.doesNotMatch(notice(p), /noisy\.example/)
  } finally { p.close() }
})

test('a dismissal is kept for the next page load', async () => {
  const first = await blockedPage(['a.example'])
  let kept
  try {
    first.at.blocked = ['a.example', 'noisy.example']
    await first.poll()
    await first.click('convo-blocked-dismiss')
    kept = {
      'vibe-companion-blocked-seen': first.window.localStorage.getItem('vibe-companion-blocked-seen') || '[]',
      'vibe-companion-blocked-dismissed': first.window.localStorage.getItem('vibe-companion-blocked-dismissed'),
    }
  } finally { first.close() }
  kept['vibe-companion-blocked-seen'] = '[]'
  const second = await blockedPage(['noisy.example'], { storage: kept })
  try {
    assert.equal(notice(second), null)
    assert.equal(dot(second), null)
  } finally { second.close() }
})
