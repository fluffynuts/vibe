// Tests of the Admin tab's card for making what the sandbox restored visible to
// the user's IDE, in jsdom (see page.js), run with page.test.js.
'use strict'
const test = require('node:test')
const assert = require('node:assert/strict')
const { openPage } = require('./page.js')

const missing = (extra) => Object.assign({
  count: 3, running: false, skipped: 0, problems: [],
  missing: [{ framework: 'net8.0', packages: [{ id: 'Dapper', version: '2.1.86' }, { id: 'Acornima', version: '1.1.0' }] },
    { framework: 'net462', packages: [{ id: 'Legacy.Lib', version: '1.2.3' }] }],
}, extra)

const nothing = (extra) => Object.assign({ count: 0, running: false, missing: [], skipped: 0, problems: [] }, extra)

function syncPage(answer) {
  const at = { answer, runs: 0 }
  const routes = {
    '/api/state': () => ({ sandbox: 'box', sandboxTabs: true, admin: true, attention: {} }),
    '/api/admin': { status: { exists: true, running: true }, upgrades: {} },
    '/api/packages/sync': () => at.answer,
    '/api/packages/sync/run': () => { at.runs++; return at.afterRun || Object.assign({}, at.answer, { running: true }) },
  }
  return openPage({ routes, storage: { 'vibe-companion-tab': 'admin' } }).then((p) => Object.assign(p, { at }))
}

test('a sandbox with nothing to fetch for an IDE has no such card', async () => {
  const p = await openPage({ routes: {
    '/api/state': { sandbox: 'box', sandboxTabs: true, admin: true, attention: {} },
    '/api/admin': { status: {}, upgrades: {} },
    '/api/packages/sync': { status: 404, body: { error: 'this sandbox has no packages to make visible to an IDE' } },
  }, storage: { 'vibe-companion-tab': 'admin' } })
  try { assert.equal(p.$('admin-sync-card').hidden, true) } finally { p.close() }
})

test('everything being in the cache says so, with nothing to press', async () => {
  const p = await syncPage(nothing())
  try {
    assert.equal(p.$('admin-sync-card').hidden, false)
    assert.match(p.$('admin-sync-summary').textContent, /in your package cache/)
    assert.equal(p.$('admin-sync-run').disabled, true)
  } finally { p.close() }
})

test('what is missing is counted, and listed by framework to read before fetching', async () => {
  const p = await syncPage(missing())
  try {
    assert.match(p.$('admin-sync-summary').textContent, /3 packages/)
    assert.match(p.$('admin-sync-summary').textContent, /IDE/)
    const list = p.$('admin-sync-list').textContent
    assert.match(list, /net8\.0/)
    assert.match(list, /Dapper 2\.1\.86/)
    assert.match(list, /net462/)
    assert.match(list, /Legacy\.Lib 1\.2\.3/)
    assert.equal(p.$('admin-sync-run').disabled, false)
  } finally { p.close() }
})

test('one is a package, not packages', async () => {
  const p = await syncPage(missing({ count: 1, missing: [{ framework: 'net8.0', packages: [{ id: 'Dapper', version: '2.1.86' }] }] }))
  try { assert.match(p.$('admin-sync-summary').textContent, /1 package /) } finally { p.close() }
})

test('Fetch them asks the host once, and the card shows it under way, with the button off', async () => {
  const p = await syncPage(missing())
  try {
    await p.click('admin-sync-run')
    assert.equal(p.at.runs, 1)
    assert.equal(p.$('admin-sync-run').disabled, true)
    assert.match(p.$('admin-sync-status').textContent, /Fetching/)
  } finally { p.close() }
})

test('a run under way is shown as one, however the page came by it', async () => {
  const p = await syncPage(missing({ running: true }))
  try {
    assert.equal(p.$('admin-sync-run').disabled, true)
    assert.match(p.$('admin-sync-status').textContent, /Fetching/)
  } finally { p.close() }
})

test('the last run says how many came', async () => {
  const p = await syncPage(nothing({ last: { fetched: 199, at: '2026-10-19T12:00:00Z' } }))
  try { assert.match(p.$('admin-sync-status').textContent, /199 packages fetched/) } finally { p.close() }
})

test('what no feed has is named, and not offered as something to fetch again', async () => {
  const p = await syncPage(missing({ count: 1, missing: [{ framework: 'net8.0', packages: [{ id: 'Gone.Pkg', version: '9.9.9' }] }],
    last: { fetched: 2, unresolved: [{ id: 'Gone.Pkg', version: '9.9.9' }] } }))
  try {
    assert.match(p.$('admin-sync-status').textContent, /no feed has.*Gone\.Pkg 9\.9\.9/s)
  } finally { p.close() }
})

test('a failed run says why, with what NuGet said to read', async () => {
  const p = await syncPage(missing({ last: { fetched: 0, error: 'fetching for net8.0: exit status 1', detail: 'error NU1301: the feed isn\'t reachable' } }))
  try {
    assert.match(p.$('admin-sync-status').textContent, /fetching for net8\.0/)
    assert.match(p.$('admin-sync-status').textContent, /NU1301/)
    assert.ok(p.$('admin-sync-status').classList.contains('bad-text'))
  } finally { p.close() }
})

test('assets files that couldn\'t be read are said, so a missing package isn\'t put down to nothing', async () => {
  const p = await syncPage(nothing({ problems: ['BLL/obj/project.assets.json: not json'] }))
  try { assert.match(p.$('admin-sync-problems').textContent, /BLL\/obj\/project\.assets\.json/) } finally { p.close() }
})

test('the card follows the host as the page polls', async () => {
  const p = await syncPage(missing())
  try {
    p.at.answer = nothing({ last: { fetched: 3 } })
    await p.poll()
    assert.match(p.$('admin-sync-summary').textContent, /in your package cache/)
    assert.equal(p.$('admin-sync-run').disabled, true)
  } finally { p.close() }
})

// conversationPage is the Conversation tab of a sandbox whose state says
// fetchable packages could be fetched for the IDE.
function conversationPage(fetchable) {
  const at = { fetchable }
  const routes = {
    '/api/state': () => ({ sandbox: 'box', sandboxTabs: true, admin: true, conversation: true, packagesToFetch: at.fetchable, attention: {} }),
    '/api/admin': { status: { exists: true, running: true }, upgrades: {} },
    '/api/packages': { requests: [], config: null },
    '/api/packages/sync': () => missing(),
    '/api/conversation': { entries: [] },
  }
  return openPage({ routes, storage: { 'vibe-companion-tab': 'conversation' } }).then((p) => Object.assign(p, { at }))
}

test('packages the IDE can\'t see are said in the conversation, with a way to the Admin tab', async () => {
  const p = await conversationPage(405)
  try {
    assert.equal(p.$('convo-sync').hidden, false)
    assert.match(p.$('convo-sync-text').textContent, /405 packages the sandbox restored aren't in your package cache/)
    assert.match(p.$('convo-sync-text').textContent, /IDE/)
    assert.equal(p.$('panel-conversation').hidden, false)
    await p.click('convo-sync-review')
    assert.equal(p.$('panel-admin').hidden, false, 'the button goes to the Admin tab')
    assert.equal(p.$('panel-conversation').hidden, true)
    assert.equal(p.$('admin-sync-run').disabled, false, 'it fetches nothing itself: the card\'s own button does')
    assert.equal(p.at.runs, undefined)
  } finally { p.close() }
})

test('one package is said in the singular', async () => {
  const p = await conversationPage(1)
  try {
    assert.match(p.$('convo-sync-text').textContent, /^1 package the sandbox restored isn't in your package cache/)
  } finally { p.close() }
})

test('nothing to fetch, nothing said; and it goes when they are fetched', async () => {
  const p = await conversationPage(0)
  try {
    assert.equal(p.$('convo-sync').hidden, true)
    p.at.fetchable = 3
    await p.poll()
    assert.equal(p.$('convo-sync').hidden, false, 'it appears when there are some')
    p.at.fetchable = 0
    await p.poll()
    assert.equal(p.$('convo-sync').hidden, true, 'and goes when there are none')
  } finally { p.close() }
})

// A restore of hundreds of packages says nothing until it ends, so a bar across the
// top of the card shows how many have come (counted by the host, in the cache).
test('a fetch under way shows a bar of how far it has got, across the top of the card', async () => {
  const p = await syncPage(missing({ count: 405, running: true, progress: { done: 120, total: 405 } }))
  try {
    const bar = p.$('admin-sync-progress')
    assert.equal(bar.hidden, false)
    assert.equal(bar.max, 405)
    assert.equal(bar.value, 120)
    assert.match(p.$('admin-sync-status').textContent, /120 of 405/)
    assert.equal(p.$('admin-packages-all').firstElementChild, bar, 'it runs across the top of the card')
    assert.ok(bar.getAttribute('aria-label'), 'it has a name for a screen reader')
  } finally { p.close() }
})

test('the bar moves as packages come, and goes when the run ends', async () => {
  const p = await syncPage(missing({ count: 405, running: true, progress: { done: 10, total: 405 } }))
  try {
    assert.equal(p.$('admin-sync-progress').value, 10)
    p.at.answer = missing({ count: 405, running: true, progress: { done: 250, total: 405 } })
    await p.poll()
    assert.equal(p.$('admin-sync-progress').value, 250)
    assert.match(p.$('admin-sync-status').textContent, /250 of 405/)
    p.at.answer = nothing({ last: { fetched: 405, at: '2026-10-19T12:00:00Z' } })
    await p.poll()
    assert.equal(p.$('admin-sync-progress').hidden, true)
  } finally { p.close() }
})

test('with no bar to show, nothing is claimed: no bar when idle, and a bar of unknown length if the host says no more than that it is fetching', async () => {
  let p = await syncPage(missing())
  try { assert.equal(p.$('admin-sync-progress').hidden, true) } finally { p.close() }
  p = await syncPage(missing({ running: true }))
  try {
    const bar = p.$('admin-sync-progress')
    assert.equal(bar.hidden, false)
    assert.equal(bar.hasAttribute('value'), false, 'no value is an unknown length, not zero')
    assert.match(p.$('admin-sync-status').textContent, /Fetching/)
  } finally { p.close() }
})

test('the bar is full, and says it is finishing, when every package is in but the restore has not ended', async () => {
  const p = await syncPage(missing({ count: 3, running: true, progress: { done: 3, total: 3 } }))
  try {
    assert.equal(p.$('admin-sync-progress').value, 3)
    assert.match(p.$('admin-sync-status').textContent, /finishing/i)
  } finally { p.close() }
})

// NuGet can fail on one package and fetch none of the others; vibe finds the one and
// fetches the rest, and says which, with NuGet's words.
test('packages NuGet failed on are listed, each with what it said', async () => {
  const p = await syncPage(missing({ count: 1, actionable: 1, last: { fetched: 404, at: '2026-10-19T12:00:00Z',
    failed: [{ package: { id: 'Bad.Pkg', version: '1.0.0' }, message: 'Central Directory corrupt' }] } }))
  try {
    const list = p.$('admin-sync-failed')
    assert.equal(list.hidden, false)
    assert.match(list.textContent, /Bad\.Pkg 1\.0\.0/)
    assert.match(list.textContent, /Central Directory corrupt/)
    assert.match(p.$('admin-sync-status').textContent, /404 packages fetched/)
  } finally { p.close() }
})

test('no failures, no list of them', async () => {
  const p = await syncPage(missing({ last: { fetched: 3, at: '2026-10-19T12:00:00Z' } }))
  try { assert.equal(p.$('admin-sync-failed').hidden, true) } finally { p.close() }
})

// What failed once is no news when everything has come since, however it did (a retry,
// the IDE's own restore): the red message would say the card is wrong.
test('a failure from before is not shown once nothing is missing any more', async () => {
  const p = await syncPage(nothing({ last: { fetched: 0, at: '2026-10-19T12:00:00Z', error: 'fetching for net8.0: exit status 1', detail: 'NU1605 …',
    failed: [{ package: { id: 'Bad.Pkg', version: '1.0.0' }, message: 'x' }] } }))
  try {
    assert.equal(p.$('admin-sync-status').classList.contains('bad-text'), false)
    assert.doesNotMatch(p.$('admin-sync-status').textContent, /NU1605|exit status/)
    assert.equal(p.$('admin-sync-failed').hidden, true)
    assert.match(p.$('admin-sync-summary').textContent, /in your package cache/)
  } finally { p.close() }
})

test('a failure is still shown while packages are missing', async () => {
  const p = await syncPage(missing({ last: { fetched: 0, at: '2026-10-19T12:00:00Z', error: 'fetching for net8.0: exit status 1', detail: 'NU1301 unreachable' } }))
  try {
    assert.equal(p.$('admin-sync-status').classList.contains('bad-text'), true)
    assert.match(p.$('admin-sync-status').textContent, /exit status 1/)
    assert.match(p.$('admin-sync-status').textContent, /NU1301 unreachable/)
  } finally { p.close() }
})

// --- the agent's requests and what the IDE can't see are one card ---------------------------

function bothPage({ sync, requests = [] } = {}) {
  const routes = {
    '/api/state': { sandbox: 'box', sandboxTabs: true, admin: true, attention: {} },
    '/api/admin': { status: { exists: true, running: true }, upgrades: {} },
    '/api/packages': { requests, config: null },
    '/api/packages/sync': sync || { status: 404, body: { error: 'no sync' } },
  }
  return openPage({ routes, storage: { 'vibe-companion-tab': 'admin' } })
}

test('what the agent asked for, and what the IDE can\'t see, are in one card, the agent\'s first', async () => {
  const p = await bothPage({ sync: missing() })
  try {
    const card = p.$('admin-packages-all')
    assert.equal(card.hidden, false)
    assert.ok(card.contains(p.$('admin-packages-card')) && card.contains(p.$('admin-sync-card')), 'both are in it')
    assert.ok(p.$('admin-packages-card').compareDocumentPosition(p.$('admin-sync-card')) & 4, 'the agent\'s requests come first')
    assert.match(p.$('admin-packages-all-h').textContent, /Packages/)
  } finally { p.close() }
})

test('the packages card is there when only one of its parts is', async () => {
  let p = await bothPage({ sync: missing() })
  try {
    assert.equal(p.$('admin-packages-all').hidden, false)
  } finally { p.close() }
  p = await openPage({ routes: {
    '/api/state': { sandbox: 'box', sandboxTabs: true, admin: true, attention: {} },
    '/api/admin': { status: {}, upgrades: {} },
    '/api/packages': { requests: [], config: null },
    '/api/packages/sync': { status: 404, body: { error: 'no sync' } },
  }, storage: { 'vibe-companion-tab': 'admin' } })
  try {
    assert.equal(p.$('admin-packages-all').hidden, false, 'requests alone')
    assert.equal(p.$('admin-sync-card').hidden, true)
  } finally { p.close() }
})

test('with neither the card is not there', async () => {
  const p = await openPage({ routes: {
    '/api/state': { sandbox: 'box', sandboxTabs: true, admin: true, attention: {} },
    '/api/admin': { status: {}, upgrades: {} },
    '/api/packages': { status: 404, body: { error: 'no packages' } },
    '/api/packages/sync': { status: 404, body: { error: 'no sync' } },
  }, storage: { 'vibe-companion-tab': 'admin' } })
  try { assert.equal(p.$('admin-packages-all').hidden, true) } finally { p.close() }
})
