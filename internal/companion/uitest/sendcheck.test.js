// Tests of the check the Conversation tab makes when the user sends a prompt: if packages the
// sandbox restored aren't in the user's package cache, the prompt is held and the user is asked
// first, since an agent that builds will download its own copies, which the user's IDE can't see.
// In jsdom (see page.js), run with page.test.js.
'use strict'
const test = require('node:test')
const assert = require('node:assert/strict')
const { openPage } = require('./page.js')

const nothingMissing = (extra) => Object.assign({ count: 0, actionable: 0, running: false, auto: false, missing: [] }, extra)
const missing = (n, extra) => Object.assign({ count: n, actionable: n, running: false, auto: false,
  missing: [{ framework: 'net8.0', packages: [{ id: 'Newtonsoft.Json', version: '13.0.3' }] }] }, extra)

// sendPage is the Conversation tab; check is what the host says each time it's asked to look.
async function sendPage(check, { checkRoute = true } = {}) {
  const at = { check, sent: [], runs: 0 }
  const routes = {
    '/api/state': { sandbox: 'box', sandboxTabs: true, admin: true, conversation: true, attention: {} },
    '/api/conversation': { events: [], last: 0, running: false },
    '/api/conversation/send': (call) => { at.sent.push(call.body.prompt); return { ok: true } },
    '/api/packages/sync/run': () => { at.runs++; return typeof at.afterRun === 'function' ? at.afterRun() : {} },
  }
  if (checkRoute) routes['/api/packages/sync/check'] = () => (typeof at.check === 'function' ? at.check() : at.check)
  const p = await openPage({ routes, storage: { 'vibe-companion-tab': 'conversation' } })
  return Object.assign(p, { at })
}

async function press(p, prompt) {
  await p.type(p.$('convo-input'), prompt)
  await p.click('convo-send')
}

const gateShown = (p) => !p.$('convo-gate').hidden

test('with nothing missing the prompt goes at once, and no notice is shown', async () => {
  const p = await sendPage(nothingMissing())
  try {
    await press(p, 'please build')
    assert.deepEqual(p.at.sent, ['please build'])
    assert.equal(gateShown(p), false)
  } finally { p.close() }
})

test('with no way to tell, the prompt goes: the check never stands in the way', async () => {
  const p = await sendPage({}, { checkRoute: false })
  try {
    await press(p, 'please build')
    assert.deepEqual(p.at.sent, ['please build'])
  } finally { p.close() }
})

test('with packages missing the prompt is held, and the user is asked first', async () => {
  const p = await sendPage(missing(3))
  try {
    await press(p, 'please build')
    assert.deepEqual(p.at.sent, [], 'nothing is sent yet')
    assert.equal(gateShown(p), true)
    assert.match(p.$('convo-gate-text').textContent, /3 packages/)
    assert.match(p.$('convo-gate-text').textContent, /IDE can't see/)
    assert.equal(p.$('convo-input').value, 'please build', 'the prompt stays where it was typed')
    assert.equal(p.$('convo-gate-fetch').hidden, false)
    assert.equal(p.$('convo-gate-send').hidden, false)
  } finally { p.close() }
})

test('Send anyway sends the held prompt', async () => {
  const p = await sendPage(missing(3))
  try {
    await press(p, 'please build')
    await p.click('convo-gate-send')
    assert.deepEqual(p.at.sent, ['please build'])
    assert.equal(gateShown(p), false)
    assert.equal(p.$('convo-input').value, '')
  } finally { p.close() }
})

test('Cancel sends nothing and keeps what was typed', async () => {
  const p = await sendPage(missing(3))
  try {
    await press(p, 'please build')
    await p.click('convo-gate-cancel')
    assert.deepEqual(p.at.sent, [])
    assert.equal(gateShown(p), false)
    assert.equal(p.$('convo-input').value, 'please build')
  } finally { p.close() }
})

test('Fetch them first starts the fetch, shows how it is going, and sends the prompt once it is done', async () => {
  const p = await sendPage(missing(3))
  try {
    await press(p, 'please build')
    p.at.afterRun = () => { p.at.check = missing(3, { running: true, progress: { done: 1, total: 3 } }); return p.at.check }
    await p.click('convo-gate-fetch')
    assert.equal(p.at.runs, 1)
    assert.equal(p.$('convo-gate-progress').hidden, false)
    assert.deepEqual(p.at.sent, [], 'not while it is fetching')
    p.at.check = missing(3, { running: true, progress: { done: 2, total: 3 } })
    await p.poll()
    assert.equal(p.$('convo-gate-progress').value, 2)
    assert.match(p.$('convo-gate-text').textContent, /2 of 3/)
    p.at.check = nothingMissing({ last: { fetched: 3 } })
    await p.poll()
    assert.deepEqual(p.at.sent, ['please build'], 'sent when it finished')
    assert.equal(gateShown(p), false)
  } finally { p.close() }
})

test('a fetch that does not finish the job is said, and the prompt waits for the user\'s word', async () => {
  const p = await sendPage(missing(3))
  try {
    await press(p, 'please build')
    p.at.afterRun = () => { p.at.check = missing(3, { running: true }); return p.at.check }
    await p.click('convo-gate-fetch')
    p.at.check = missing(3, { last: { fetched: 1, error: 'fetching for net8.0: exit status 1', detail: 'NU1301 unreachable' } })
    await p.poll()
    assert.deepEqual(p.at.sent, [], 'a failed fetch is no reason to send')
    assert.equal(gateShown(p), true)
    assert.match(p.$('convo-gate-text').textContent, /exit status 1/)
    assert.equal(p.$('convo-gate-send').hidden, false, 'the user can still send')
    await p.click('convo-gate-send')
    assert.deepEqual(p.at.sent, ['please build'])
  } finally { p.close() }
})

test('when packages are fetched without asking, they are fetched first without being asked about', async () => {
  const p = await sendPage(missing(3, { auto: true }))
  try {
    p.at.afterRun = () => { p.at.check = missing(3, { auto: true, running: true }); return p.at.check }
    await press(p, 'please build')
    assert.equal(p.at.runs, 1, 'the fetch began by itself')
    assert.equal(p.$('convo-gate-fetch').hidden, true, 'nothing to ask')
    assert.deepEqual(p.at.sent, [])
    p.at.check = nothingMissing({ auto: true, last: { fetched: 3 } })
    await p.poll()
    assert.deepEqual(p.at.sent, ['please build'])
  } finally { p.close() }
})

test('a fetch already under way is waited for too', async () => {
  const p = await sendPage(missing(3, { actionable: 0, running: true, progress: { done: 1, total: 3 } }))
  try {
    await press(p, 'please build')
    assert.deepEqual(p.at.sent, [])
    assert.equal(gateShown(p), true)
    assert.equal(p.at.runs, 0, 'it is not started twice')
    p.at.check = nothingMissing({ last: { fetched: 3 } })
    await p.poll()
    assert.deepEqual(p.at.sent, ['please build'])
  } finally { p.close() }
})
