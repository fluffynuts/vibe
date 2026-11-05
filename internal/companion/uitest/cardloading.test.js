// The Admin tab's cards, while what fills them is on its way: each is busy (dimmed, with a spinner, and nothing in it
// can be used) until its own requests have answered once. Buttons that act on what hasn't arrived (Re-compose, with
// no features listed yet) can't be pressed, and a card that never gets its answer says so, with a way to try again.
'use strict'
const test = require('node:test')
const assert = require('node:assert/strict')
const { openPage } = require('./page.js')

const STATE = { sandbox: 'box', sandboxTabs: true, conversation: true, admin: true, signin: true, attention: {} }
const ADMIN = { status: { exists: true, running: true, profile: 'p', guided: true, features: ['go'], availableFeatures: ['go', 'redis'] }, job: null, upgrades: {} }
const SIGNED_IN = { state: 'signed-in' }

// held is an answer that arrives when the test says.
function held() {
  let release
  const promise = new Promise((resolve) => { release = resolve })
  return { promise, release }
}

const turns = async () => { for (let i = 0; i < 30; i++) await new Promise((resolve) => setImmediate(resolve)) }

// The cards that /api/admin fills, by something each has in it.
const adminCards = (p) => ({
  sandbox: p.$('admin-reinit').closest('.card'),
  features: p.$('admin-recompose').closest('.card'),
  upgrades: p.$('admin-upgrades-card'),
  backups: p.$('admin-backups').closest('.card'),
  git: p.$('admin-git').closest('.card'),
})
const busy = (card) => card.getAttribute('aria-busy') === 'true'
const unusable = (node) => node.closest('[inert]') !== null

// open the page on the Admin tab, with the answers the test gives; those it doesn't give are quick ones.
async function adminPage(routes = {}) {
  const p = await openPage({
    settled: false,
    routes: {
      '/api/state': STATE,
      '/api/conversation': { events: [], last: 0, running: false },
      '/api/admin': ADMIN,
      '/api/admin/instructions': { applies: true, agent: 'claude', files: [] },
      '/api/signin': { agent: 'claude', docker: SIGNED_IN, agentSignIn: SIGNED_IN },
      '/api/packages': { requests: [] },
      '/api/packages/sync': { count: 0 },
      '/api/theme': { choices: [] },
      '/api/log': { entries: [] },
      ...routes,
    },
    storage: { 'vibe-companion-tab': 'admin' },
  })
  await turns()
  return p
}

test('the cards /api/admin fills are busy, and nothing in them can be used, until it has answered', async () => {
  const admin = held()
  const p = await adminPage({ '/api/admin': () => admin.promise })
  try {
    for (const [name, card] of Object.entries(adminCards(p))) assert.ok(busy(card), `${name} is busy`)
    assert.ok(unusable(p.$('admin-recompose')), 'Re-compose, with no features listed yet, can not be pressed')
    assert.ok(unusable(p.$('admin-reinit')), 'nor Rebuild')
    assert.equal(unusable(adminCards(p).sandbox.querySelector('h2')), false, "a card's title stays")
    admin.release(ADMIN)
    await turns()
    for (const [name, card] of Object.entries(adminCards(p))) {
      assert.equal(busy(card), false, `${name} is no longer busy`)
      assert.equal(unusable(card.firstElementChild.nextElementSibling || card), false, `${name} can be used`)
    }
    assert.equal(unusable(p.$('admin-recompose')), false)
  } finally { p.close() }
})

test('a card is busy only for its own requests: the others, answered, are not', async () => {
  const admin = held()
  const p = await adminPage({ '/api/admin': () => admin.promise })
  try {
    assert.equal(busy(p.$('admin-instructions').closest('.card')), false, 'the instructions have come')
    assert.equal(busy(p.$('admin-signin')), false, 'so has the sign-in check')
  } finally { admin.release(ADMIN); await turns(); p.close() }
})

test('a card with two requests is busy until both have answered', async () => {
  const sync = held()
  const p = await adminPage({ '/api/packages/sync': () => sync.promise })
  try {
    assert.ok(busy(p.$('admin-packages-all')), 'the packages card waits for what the IDE cannot see too')
    sync.release({ count: 0 })
    await turns()
    assert.equal(busy(p.$('admin-packages-all')), false)
  } finally { p.close() }
})

test('it is the first answer that counts: a card is not busy again when it is refreshed', async () => {
  let asked = 0
  const second = held()
  const p = await adminPage({ '/api/admin': () => (++asked === 1 ? ADMIN : second.promise) })
  try {
    assert.equal(busy(adminCards(p).features), false, 'answered')
    await p.kick('tick')
    assert.ok(asked >= 2, 'it was asked again')
    assert.equal(busy(adminCards(p).features), false, 'while the second answer is on its way, the card is as it was')
    assert.equal(unusable(p.$('admin-recompose')), false)
  } finally { second.release(ADMIN); await turns(); p.close() }
})

test('a request that fails is said on its cards, which stay unusable, with a way to try again', async () => {
  let asked = 0
  const retry = held()
  const p = await adminPage({ '/api/admin': () => (++asked === 1 ? { status: 500, body: { error: 'sbx fell over' } } : retry.promise) })
  try {
    const { features } = adminCards(p)
    assert.equal(busy(features), false, 'it is not spinning: it stopped')
    const failed = features.querySelector('.card-load-failed')
    assert.ok(failed, 'the card says it could not load')
    assert.match(failed.textContent, /Couldn't load this/)
    assert.ok(unusable(p.$('admin-recompose')), 'and what is in it is still not usable')
    assert.equal(unusable(failed), false, 'the way to try again is')
    failed.querySelector('button').click()
    await turns()
    assert.equal(asked, 2, 'it asked again')
    assert.ok(busy(features), 'busy again while it waits')
    assert.equal(features.querySelector('.card-load-failed'), null, 'and no longer says it failed')
    retry.release(ADMIN)
    await turns()
    assert.equal(busy(features), false)
    assert.equal(unusable(p.$('admin-recompose')), false, 'now usable')
  } finally { retry.release(ADMIN); await turns(); p.close() }
})

test('a request that says its card does not apply settles it: the card is not left busy', async () => {
  const p = await adminPage({
    '/api/packages': { status: 404, body: { error: 'this sandbox cannot ask for packages' } },
    '/api/packages/sync': { status: 404, body: { error: 'nor restore them' } },
  })
  try {
    assert.equal(busy(p.$('admin-packages-all')), false)
    assert.equal(p.$('admin-packages-all').hidden, true)
  } finally { p.close() }
})

test('the sign-in card is busy until its check has answered, and not left so when the check fails', async () => {
  const check = held()
  const p = await adminPage({ '/api/signin': () => check.promise })
  try {
    assert.ok(busy(p.$('admin-signin')))
    check.release({ status: 500, body: { error: 'could not check' } })
    await turns()
    assert.equal(busy(p.$('admin-signin')), false, "the card's own message says what went wrong")
  } finally { check.release({ agent: 'claude', docker: SIGNED_IN, agentSignIn: SIGNED_IN }); await turns(); p.close() }
})
