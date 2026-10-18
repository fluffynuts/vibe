// Tests of the Admin tab's package requests, in jsdom (see page.js), run
// with page.test.js.
'use strict'
const test = require('node:test')
const assert = require('node:assert/strict')
const { openPage } = require('./page.js')

const request = (id, extra) => Object.assign({ key: 'nuget|' + id + '|1.0.0|net8.0', ecosystem: 'nuget', id, version: '1.0.0', framework: 'net8.0', note: 'new — main doesn\'t use it' }, extra)

// packagesPage is the Admin tab of a sandbox that can ask for packages:
// answer is what /api/packages says, which a test can change.
function packagesPage(answer, { attention = {} } = {}) {
  const at = { answer }
  const routes = {
    '/api/state': () => ({ sandbox: 'box', sandboxTabs: true, admin: true, attention }),
    '/api/admin': { status: { exists: true, running: true }, upgrades: {} },
    '/api/packages': () => at.answer,
    '/api/packages/allow': (call) => { at.allowed = call.body.keys; return { requests: [], config: null } },
    '/api/packages/deny-all': () => { at.denied = true; return { requests: [], config: null } },
    '/api/packages/approve-config': (call) => { at.approved = call.body.hash; return { requests: [], config: null } },
  }
  return openPage({ routes, storage: { 'vibe-companion-tab': 'admin' } }).then((p) => Object.assign(p, { at }))
}

const rows = (p) => Array.from(p.$('admin-packages').querySelectorAll('li'))

test('a sandbox that can\'t ask for packages has no such card', async () => {
  const p = await openPage({ routes: {
    '/api/state': { sandbox: 'box', sandboxTabs: true, admin: true, attention: {} },
    '/api/admin': { status: {}, upgrades: {} },
    '/api/packages': { status: 404, body: { error: 'this sandbox can\'t ask for packages' } },
  }, storage: { 'vibe-companion-tab': 'admin' } })
  try {
    assert.equal(p.$('admin-packages-card').hidden, true)
  } finally { p.close() }
})

test('nothing waiting says so, with no buttons to press', async () => {
  const p = await packagesPage({ requests: [], config: null })
  try {
    assert.equal(p.$('admin-packages-card').hidden, false)
    assert.equal(p.$('admin-packages-none').hidden, false)
    assert.equal(p.$('admin-packages-bar').hidden, true)
  } finally { p.close() }
})

test('each request is listed, ticked, with what vibe says it is', async () => {
  const p = await packagesPage({ requests: [request('A.Lib'), request('B.Lib', { note: 'upgrade — main has B.Lib 0.9.0' })], config: null })
  try {
    assert.equal(rows(p).length, 2)
    assert.match(rows(p)[0].textContent, /nuget A\.Lib 1\.0\.0 \(net8\.0\)/)
    assert.match(rows(p)[1].textContent, /upgrade — main has B\.Lib 0\.9\.0/)
    assert.ok(rows(p).every((r) => r.querySelector('input').checked), 'all ticked by default')
    assert.match(p.$('admin-packages-count').textContent, /2 waiting/)
  } finally { p.close() }
})

test('Allow selected sends only what is ticked, and unticking survives a poll', async () => {
  const p = await packagesPage({ requests: [request('A.Lib'), request('B.Lib')], config: null })
  try {
    const box = rows(p)[1].querySelector('input')
    box.checked = false
    box.dispatchEvent(new p.window.Event('change', { bubbles: true }))
    // Another request arrives, so the list is drawn afresh.
    p.at.answer = { requests: [request('A.Lib'), request('B.Lib'), request('C.Lib')], config: null }
    await p.poll()
    assert.equal(rows(p).length, 3)
    assert.equal(rows(p)[1].querySelector('input').checked, false, 'a poll undid the unticking')
    assert.equal(rows(p)[2].querySelector('input').checked, true, 'a new request is ticked')
    await p.click('admin-packages-allow')
    assert.deepEqual(p.at.allowed, ['nuget|A.Lib|1.0.0|net8.0', 'nuget|C.Lib|1.0.0|net8.0'])
  } finally { p.close() }
})

test('Deny all denies everything held', async () => {
  const p = await packagesPage({ requests: [request('A.Lib')], config: null })
  try {
    await p.click('admin-packages-deny')
    assert.equal(p.at.denied, true)
  } finally { p.close() }
})

test('a request being fetched can no longer be unticked or allowed again', async () => {
  const p = await packagesPage({ requests: [request('A.Lib', { fetching: true })], config: null })
  try {
    assert.equal(rows(p)[0].querySelector('input').disabled, true)
    assert.match(rows(p)[0].textContent, /fetching/)
  } finally { p.close() }
})

const config = (extra) => Object.assign({
  file: 'nuget.config', hash: 'h1', approved: false, text: '<configuration/>',
  sources: [{ name: 'corp', url: 'https://nuget.corp.example/' }],
}, extra)

test('the folder\'s nuget.config is shown with its sources, to be approved', async () => {
  const p = await packagesPage({ requests: [], config: config() })
  try {
    assert.equal(p.$('admin-nugetconfig').hidden, false)
    assert.match(p.$('admin-nugetconfig-sources').textContent, /corp.*nuget\.corp\.example/)
    assert.equal(p.$('admin-nugetconfig-text').textContent, '<configuration/>')
    assert.match(p.$('admin-nugetconfig-note').textContent, /Not approved/)
    await p.click('admin-nugetconfig-approve')
    assert.equal(p.at.approved, 'h1', 'what was approved is the content that was shown')
  } finally { p.close() }
})

test('a config that redefines one of the user\'s sources carries a warning', async () => {
  const p = await packagesPage({ requests: [], config: config({ redefines: ['it gives your source "corp" the address https://evil.example/'] }) })
  try {
    assert.match(p.$('admin-nugetconfig-warnings').textContent, /Warning: .*evil\.example/)
  } finally { p.close() }
})

test('an approved config says so, with nothing left to approve', async () => {
  const p = await packagesPage({ requests: [], config: config({ approved: true }) })
  try {
    assert.match(p.$('admin-nugetconfig-note').textContent, /Approved/)
    assert.equal(p.$('admin-nugetconfig-approve').hidden, true)
  } finally { p.close() }
})

test('a folder with no nuget.config shows no config section', async () => {
  const p = await packagesPage({ requests: [], config: null })
  try {
    assert.equal(p.$('admin-nugetconfig').hidden, true)
  } finally { p.close() }
})
