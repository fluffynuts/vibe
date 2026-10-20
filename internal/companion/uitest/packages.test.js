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
    '/api/packages/decide': (call) => { at.decided = call.body; return at.afterAllow || { requests: [], config: null } },
    '/api/packages/decline-all': () => { at.declinedAll = true; return { requests: [], config: null } },
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
    assert.deepEqual(p.at.decided.allow, ['nuget|A.Lib|1.0.0|net8.0', 'nuget|C.Lib|1.0.0|net8.0'])
    assert.deepEqual(p.at.decided.notSelected, ['nuget|B.Lib|1.0.0|net8.0'], 'what was unticked is said, so the agent isn\'t left waiting')
  } finally { p.close() }
})

test('Deny them all declines everything held', async () => {
  const p = await packagesPage({ requests: [request('A.Lib')], config: null })
  try {
    assert.equal(p.$('admin-packages-deny').textContent, 'Deny them all')
    await p.click('admin-packages-deny')
    assert.equal(p.at.declinedAll, true)
  } finally { p.close() }
})

test('a request being fetched can no longer be unticked or allowed again', async () => {
  const p = await packagesPage({ requests: [request('A.Lib', { approved: true, fetching: true })], config: null })
  try {
    assert.equal(rows(p)[0].querySelector('input').disabled, true)
    assert.match(rows(p)[0].textContent, /fetching/)
  } finally { p.close() }
})

test('an approved request about to be fetched says so, and can no longer be changed', async () => {
  const p = await packagesPage({ requests: [request('A.Lib', { approved: true }), request('B.Lib')], config: null })
  try {
    assert.match(rows(p)[0].textContent, /approved.*about to be fetched/)
    assert.equal(rows(p)[0].querySelector('input').disabled, true)
    assert.equal(rows(p)[1].querySelector('input').disabled, false, 'the undecided one can still be ticked')
  } finally { p.close() }
})

test('with every request approved, both buttons are off and a line says what is happening', async () => {
  const p = await packagesPage({ requests: [request('A.Lib', { approved: true, fetching: true }), request('B.Lib', { approved: true })], config: null })
  try {
    assert.equal(p.$('admin-packages-allow').disabled, true)
    assert.equal(p.$('admin-packages-deny').disabled, true)
    assert.match(p.$('admin-packages-status').textContent, /2 approved.*fetched together/)
    assert.doesNotMatch(p.$('admin-packages-status').textContent, /a few at a time|each one done/)
  } finally { p.close() }
})

test('new requests arriving after an approval turn the buttons back on, for those only', async () => {
  const p = await packagesPage({ requests: [request('A.Lib', { approved: true })], config: null })
  try {
    assert.equal(p.$('admin-packages-allow').disabled, true)
    p.at.answer = { requests: [request('A.Lib', { approved: true }), request('B.Lib')], config: null }
    await p.poll()
    assert.equal(p.$('admin-packages-allow').disabled, false)
    await p.click('admin-packages-allow')
    assert.deepEqual(p.at.decided.allow, ['nuget|B.Lib|1.0.0|net8.0'], 'the approved one is not sent again')
    assert.deepEqual(p.at.decided.notSelected, [], 'nor is it counted as left out')
  } finally { p.close() }
})

test('allowing disables the buttons at once, before the next poll', async () => {
  const p = await packagesPage({ requests: [request('A.Lib')], config: null })
  try {
    p.at.afterAllow = { requests: [request('A.Lib', { approved: true })], config: null }
    await p.click('admin-packages-allow')
    assert.equal(p.$('admin-packages-allow').disabled, true)
    assert.match(rows(p)[0].textContent, /approved/)
  } finally { p.close() }
})

test('Allow selected is off while nothing undecided is ticked', async () => {
  const p = await packagesPage({ requests: [request('A.Lib')], config: null })
  try {
    const box = rows(p)[0].querySelector('input')
    box.checked = false
    box.dispatchEvent(new p.window.Event('change', { bubbles: true }))
    assert.equal(p.$('admin-packages-allow').disabled, true)
    box.checked = true
    box.dispatchEvent(new p.window.Event('change', { bubbles: true }))
    assert.equal(p.$('admin-packages-allow').disabled, false)
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

// conversationPage is the Conversation tab of a sandbox with package
// requests waiting, as vibe's state says: waiting of them.
function conversationPage(waiting) {
  const at = { waiting }
  const routes = {
    '/api/state': () => ({ sandbox: 'box', sandboxTabs: true, admin: true, conversation: true, packagesWaiting: at.waiting, attention: {} }),
    '/api/admin': { status: { exists: true, running: true }, upgrades: {} },
    '/api/packages': { requests: [], config: null },
    '/api/conversation': { entries: [] },
  }
  return openPage({ routes, storage: { 'vibe-companion-tab': 'conversation' } }).then((p) => Object.assign(p, { at }))
}

test('requests waiting are said in the conversation, with a way to the Admin tab', async () => {
  const p = await conversationPage(3)
  try {
    assert.equal(p.$('convo-packages').hidden, false)
    assert.match(p.$('convo-packages-text').textContent, /3 package requests are waiting/)
    assert.equal(p.$('panel-conversation').hidden, false)
    await p.click('convo-packages-review')
    assert.equal(p.$('panel-admin').hidden, false, 'the button goes to the Admin tab')
    assert.equal(p.$('panel-conversation').hidden, true)
  } finally { p.close() }
})

test('one request waiting is said in the singular', async () => {
  const p = await conversationPage(1)
  try {
    assert.match(p.$('convo-packages-text').textContent, /1 package request is waiting/)
  } finally { p.close() }
})

test('nothing waiting, nothing said; and it goes when the requests are decided', async () => {
  const p = await conversationPage(0)
  try {
    assert.equal(p.$('convo-packages').hidden, true)
    p.at.waiting = 2
    await p.poll()
    assert.equal(p.$('convo-packages').hidden, false, 'it appears when requests arrive')
    p.at.waiting = 0
    await p.poll()
    assert.equal(p.$('convo-packages').hidden, true, 'and goes when they are decided')
  } finally { p.close() }
})

// --- one button that says what it will do ----------------------------------------------------------

const tick = (p, i, on) => {
  const box = rows(p)[i].querySelector('input')
  box.checked = on
  box.dispatchEvent(new p.window.Event('change', { bubbles: true }))
}
const allowText = (p) => p.$('admin-packages-allow').textContent

test('the button says what it will do: all, one, or some allowed and the rest not', async () => {
  const p = await packagesPage({ requests: [request('A.Lib'), request('B.Lib'), request('C.Lib')], config: null })
  try {
    assert.equal(allowText(p), 'Allow all 3')
    tick(p, 1, false)
    assert.equal(allowText(p), 'Allow 2, deny 1')
    tick(p, 2, false)
    assert.equal(allowText(p), 'Allow 1, deny 2')
    tick(p, 0, false)
    assert.equal(p.$('admin-packages-allow').disabled, true, 'with nothing ticked there is nothing to allow: Deny them all is the button')
    assert.equal(p.$('admin-packages-deny').disabled, false)
  } finally { p.close() }
})

test('one request is Allow it', async () => {
  const p = await packagesPage({ requests: [request('A.Lib')], config: null })
  try { assert.equal(allowText(p), 'Allow it') } finally { p.close() }
})

test('what is already approved is in no count', async () => {
  const p = await packagesPage({ requests: [request('A.Lib', { approved: true }), request('B.Lib'), request('C.Lib')], config: null })
  try {
    assert.equal(allowText(p), 'Allow all 2')
    tick(p, 2, false)
    assert.equal(allowText(p), 'Allow 1, deny 1')
  } finally { p.close() }
})

test('deciding sends both lists at once, and says what was left out, for the agent will ask', async () => {
  const p = await packagesPage({ requests: [request('A.Lib'), request('B.Lib'), request('C.Lib'), request('D.Lib')], config: null })
  try {
    tick(p, 3, false)
    await p.click('admin-packages-allow')
    assert.deepEqual(p.at.decided.allow, ['nuget|A.Lib|1.0.0|net8.0', 'nuget|B.Lib|1.0.0|net8.0', 'nuget|C.Lib|1.0.0|net8.0'])
    assert.deepEqual(p.at.decided.notSelected, ['nuget|D.Lib|1.0.0|net8.0'])
    const line = p.$('admin-packages-decision')
    assert.equal(line.hidden, false)
    assert.match(line.textContent, /Allowed 3; left out D\.Lib 1\.0\.0: the agent will ask you about it\./)
  } finally { p.close() }
})

test('with none left out the line says only what was allowed', async () => {
  const p = await packagesPage({ requests: [request('A.Lib'), request('B.Lib')], config: null })
  try {
    await p.click('admin-packages-allow')
    assert.match(p.$('admin-packages-decision').textContent, /^Allowed 2\.$/)
  } finally { p.close() }
})

test('several left out are all named, and the agent will ask about them', async () => {
  const p = await packagesPage({ requests: [request('A.Lib'), request('B.Lib'), request('C.Lib')], config: null })
  try {
    tick(p, 1, false)
    tick(p, 2, false)
    await p.click('admin-packages-allow')
    assert.match(p.$('admin-packages-decision').textContent, /left out B\.Lib 1\.0\.0, C\.Lib 1\.0\.0: the agent will ask you about them\./)
  } finally { p.close() }
})

test('declining everything is said, and what happens next', async () => {
  const p = await packagesPage({ requests: [request('A.Lib'), request('B.Lib')], config: null })
  try {
    await p.click('admin-packages-deny')
    assert.match(p.$('admin-packages-decision').textContent, /Declined 2: the agent will tell you and ask what you'd like instead\./)
  } finally { p.close() }
})

test('the line stays through the polls and is replaced by the next decision', async () => {
  const p = await packagesPage({ requests: [request('A.Lib'), request('B.Lib')], config: null })
  try {
    tick(p, 1, false)
    p.at.afterAllow = { requests: [], config: null }
    await p.click('admin-packages-allow')
    const first = p.$('admin-packages-decision').textContent
    await p.poll()
    assert.equal(p.$('admin-packages-decision').textContent, first, 'a poll cleared it')
    p.at.answer = { requests: [request('C.Lib')], config: null }
    await p.poll()
    await p.click('admin-packages-allow')
    assert.match(p.$('admin-packages-decision').textContent, /^Allowed 1\.$/)
  } finally { p.close() }
})

test('before any decision there is no line', async () => {
  const p = await packagesPage({ requests: [request('A.Lib')], config: null })
  try { assert.equal(p.$('admin-packages-decision').hidden, true) } finally { p.close() }
})
