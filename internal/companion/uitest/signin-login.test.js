// The Sign-in card's buttons: signing in from the page, by the commands vibe runs for it, with the address to open, a
// field for the code the agent's sign-in asks to be pasted back, and the state of it as it goes.
'use strict'
const test = require('node:test')
const assert = require('node:assert/strict')
const { openPage } = require('./page.js')

const OUT = { state: 'signed-out', manual: ['by hand'] }
const IN = { state: 'signed-in', detail: 'me@example.com', manual: ['by hand'] }

// pageWith gives a page whose host says docker and agent as given, and whose sign-in job is whatever job.now says.
async function pageWith({ start = true, docker = OUT, agent = OUT, job = {}, startAnswer } = {}) {
  const host = { job: Object.assign({ kind: '', running: false, done: false, ok: false, lines: [] }, job), docker, agent }
  const p = await openPage({
    routes: {
      '/api/state': { sandbox: 'box', sandboxTabs: true, conversation: true, admin: true, signin: true, signinStart: start, attention: {} },
      '/api/conversation': { events: [], last: 0, running: false },
      '/api/admin': { state: 'running', info: '' },
      '/api/signin': () => ({ agent: 'claude', docker: host.docker, agentSignIn: host.agent }),
      '/api/signin/start': (call) => {
        if (startAnswer) return startAnswer(call, host)
        host.job = { kind: call.body.kind, running: true, done: false, ok: false, lines: [] }
        return host.job
      },
      '/api/signin/job': () => host.job,
      '/api/signin/code': { sent: true },
      '/api/signin/cancel': { cancelled: true },
    },
    storage: { 'vibe-companion-tab': 'admin' },
  })
  await p.settle()
  p.host = host
  return p
}

const buttons = (p) => [...p.$('admin-signin').querySelectorAll('button.signin-go')].map((b) => b.dataset.kind)
const panel = (p) => p.$('signin-job')
const calls = (p, path) => p.calls.filter((c) => c.path === path)

test('there are no sign-in buttons where the host cannot run a sign-in', async () => {
  const p = await pageWith({ start: false })
  try {
    assert.deepEqual(buttons(p), [])
    assert.equal(panel(p).hidden, true)
  } finally { p.close() }
})

test('a button is offered for what is signed out, and not for what is signed in or cannot be told', async () => {
  const both = await pageWith()
  const one = await pageWith({ docker: IN })
  const unknown = await pageWith({ docker: { state: 'unknown', manual: [] }, agent: { state: 'needs-policy', manual: [] } })
  try {
    assert.deepEqual(buttons(both), ['docker', 'agent'])
    assert.deepEqual(buttons(one), ['agent'])
    assert.deepEqual(buttons(unknown), [])
  } finally { both.close(); one.close(); unknown.close() }
})

test('pressing it starts that sign-in and shows the address to open and the code to confirm', async () => {
  const p = await pageWith({ docker: OUT, agent: IN })
  try {
    await p.click(p.$('admin-signin').querySelector('button.signin-go[data-kind=docker]'))
    assert.deepEqual(calls(p, '/api/signin/start').map((c) => c.body), [{ kind: 'docker' }])
    assert.equal(panel(p).hidden, false)
    p.host.job = { kind: 'docker', running: true, lines: ['Waiting...'], urlKnown: true, url: 'https://login.docker.com/activate?user_code=AB-CD', code: 'AB-CD' }
    await p.every('pollSignInJob')
    const link = panel(p).querySelector('a')
    assert.equal(link.href, 'https://login.docker.com/activate?user_code=AB-CD')
    assert.equal(link.target, '_blank')
    assert.match(link.rel, /noopener/)
    assert.match(link.rel, /noreferrer/)
    assert.match(panel(p).textContent, /AB-CD/)
    assert.equal(panel(p).querySelector('input'), null, "sbx's sign-in takes no code")
  } finally { p.close() }
})

test('an address that is not an https one is shown as text, never as a link', async () => {
  const p = await pageWith({ docker: OUT, agent: IN })
  try {
    await p.click(p.$('admin-signin').querySelector('button.signin-go'))
    p.host.job = { kind: 'docker', running: true, lines: [], url: 'javascript:alert(1)' }
    await p.every('pollSignInJob')
    assert.equal(panel(p).querySelector('a'), null)
  } finally { p.close() }
})

test('the agent\'s sign-in has a field for the code, which goes to the host and is cleared', async () => {
  const p = await pageWith({ docker: IN, agent: OUT })
  try {
    await p.click(p.$('admin-signin').querySelector('button.signin-go'))
    p.host.job = { kind: 'agent', running: true, lines: [], urlKnown: true, url: 'https://claude.ai/oauth/authorize?x=1' }
    await p.every('pollSignInJob')
    const field = panel(p).querySelector('input')
    assert.ok(field, 'there is no field for the code')
    assert.equal(field.type, 'password', 'the code is shown on the page')
    assert.equal(field.autocomplete, 'off')
    await p.type(field, 'the-code-123')
    await p.click(panel(p).querySelector('button.signin-send'))
    assert.deepEqual(calls(p, '/api/signin/code').map((c) => c.body), [{ code: 'the-code-123' }])
    assert.equal(field.value, '')
  } finally { p.close() }
})

test('nothing is sent for an empty field', async () => {
  const p = await pageWith({ docker: IN, agent: OUT })
  try {
    await p.click(p.$('admin-signin').querySelector('button.signin-go'))
    p.host.job = { kind: 'agent', running: true, lines: [], urlKnown: true, url: 'https://claude.ai/oauth/authorize?x=1' }
    await p.every('pollSignInJob')
    await p.click(panel(p).querySelector('button.signin-send'))
    assert.equal(calls(p, '/api/signin/code').length, 0)
  } finally { p.close() }
})

test('when the sign-in succeeds the page says so and looks at the sign-ins again', async () => {
  const p = await pageWith({ docker: OUT, agent: IN })
  try {
    await p.click(p.$('admin-signin').querySelector('button.signin-go'))
    const before = calls(p, '/api/signin').length
    p.host.docker = IN
    p.host.job = { kind: 'docker', running: false, done: true, ok: true, lines: ['Signed in as me.'] }
    await p.every('pollSignInJob')
    assert.match(panel(p).textContent, /signed in/i)
    assert.equal(calls(p, '/api/signin').length, before + 1)
    assert.match(p.$('signin-docker').querySelector('.pill').textContent, /signed in/i)
  } finally { p.close() }
})

test('when it fails the page says why and shows what it printed', async () => {
  const p = await pageWith({ docker: OUT, agent: IN })
  try {
    await p.click(p.$('admin-signin').querySelector('button.signin-go'))
    p.host.job = { kind: 'docker', running: false, done: true, ok: false, error: 'exit status 1', lines: ['error: that code has expired'] }
    await p.every('pollSignInJob')
    assert.match(panel(p).textContent, /exit status 1/)
    assert.match(panel(p).textContent, /that code has expired/)
    assert.doesNotMatch(panel(p).textContent, /signed in\.$/i)
  } finally { p.close() }
})

test('Cancel stops it', async () => {
  const p = await pageWith({ docker: OUT, agent: IN })
  try {
    await p.click(p.$('admin-signin').querySelector('button.signin-go'))
    p.host.job = { kind: 'docker', running: true, lines: [] }
    await p.every('pollSignInJob')
    await p.click(panel(p).querySelector('button.signin-cancel'))
    assert.equal(calls(p, '/api/signin/cancel').length, 1)
  } finally { p.close() }
})

test('a start the host refuses is said, and no panel is left waiting', async () => {
  const p = await pageWith({ docker: OUT, agent: IN, startAnswer: () => ({ status: 409, body: { error: 'a sign-in is already under way' } }) })
  try {
    await p.click(p.$('admin-signin').querySelector('button.signin-go'))
    assert.match(p.$('signin-status').textContent, /already under way/)
    assert.equal(p.$('signin-status').hidden, false)
    assert.equal(panel(p).hidden, true)
  } finally { p.close() }
})

test('a sign-in already running when the page opens is shown', async () => {
  const p = await pageWith({ docker: OUT, agent: IN, job: { kind: 'docker', running: true, lines: [], urlKnown: true, url: 'https://login.docker.com/x', code: 'ZZ-YY' } })
  try {
    assert.equal(panel(p).hidden, false)
    assert.match(panel(p).textContent, /ZZ-YY/)
  } finally { p.close() }
})

// Nothing is said about an address the user can't see yet: a spinner stands in until it is known.
test('until the address is known the panel shows only that it is starting, with a spinner, and no instructions', async () => {
  const p = await pageWith({ docker: OUT, agent: IN })
  try {
    await p.click(p.$('admin-signin').querySelector('button.signin-go'))
    assert.ok(panel(p).querySelector('.spinner'), 'there is no spinner')
    assert.match(panel(p).textContent, /starting/i)
    assert.doesNotMatch(panel(p).textContent, /open the address/i, 'it tells of an address nobody can see')
    assert.equal(panel(p).querySelector('a'), null)
    assert.ok(panel(p).querySelector('button.signin-cancel'), 'it can be cancelled while starting')
    p.host.job = { kind: 'docker', running: true, lines: [], urlKnown: true, url: 'https://login.docker.com/activate?user_code=AB-CD', code: 'AB-CD' }
    await p.every('pollSignInJob')
    assert.equal(panel(p).querySelector('.spinner'), null)
    assert.match(panel(p).textContent, /open the address/i)
    assert.ok(panel(p).querySelector('a'))
  } finally { p.close() }
})

test("the agent's panel waits the same way for its address before offering the field for the code", async () => {
  const p = await pageWith({ docker: IN, agent: OUT })
  try {
    await p.click(p.$('admin-signin').querySelector('button.signin-go'))
    assert.ok(panel(p).querySelector('.spinner'))
    assert.equal(panel(p).querySelector('input'), null, 'a field for a code nobody has been given yet')
    p.host.job = { kind: 'agent', running: true, lines: [], urlKnown: true, url: 'https://claude.ai/oauth/authorize?x=1' }
    await p.every('pollSignInJob')
    assert.ok(panel(p).querySelector('input'))
  } finally { p.close() }
})

// The field is cleared when the code is sent, so the card must say what is going on, or it looks as if nothing happened.
test('once the code is sent the panel shows that it is being checked, with a spinner, and no field', async () => {
  const p = await pageWith({ docker: IN, agent: OUT })
  try {
    await p.click(p.$('admin-signin').querySelector('button.signin-go'))
    p.host.job = { kind: 'agent', running: true, lines: [], urlKnown: true, url: 'https://claude.ai/oauth/authorize?x=1' }
    await p.every('pollSignInJob')
    await p.type(panel(p).querySelector('input'), 'the-code')
    await p.click(panel(p).querySelector('button.signin-send'))
    assert.ok(panel(p).querySelector('.spinner'), 'no sign that it is busy')
    assert.match(panel(p).textContent, /checking|signing you in/i)
    assert.equal(panel(p).querySelector('input'), null)
    assert.equal(panel(p).querySelector('a'), null, 'the address is still offered')
    assert.ok(panel(p).querySelector('button.signin-cancel'))
    p.host.job = { kind: 'agent', running: false, done: true, ok: true, lines: [] }
    await p.every('pollSignInJob')
    assert.equal(panel(p).querySelector('.spinner'), null)
    assert.match(panel(p).textContent, /signed in/i)
  } finally { p.close() }
})

test('a code that is not taken is not waited on for good: after a while it can be entered again', async () => {
  const p = await pageWith({ docker: IN, agent: OUT })
  try {
    await p.click(p.$('admin-signin').querySelector('button.signin-go'))
    p.host.job = { kind: 'agent', running: true, lines: [], urlKnown: true, url: 'https://claude.ai/oauth/authorize?x=1' }
    await p.every('pollSignInJob')
    await p.type(panel(p).querySelector('input'), 'the-code')
    await p.click(panel(p).querySelector('button.signin-send'))
    assert.equal(panel(p).querySelector('button.signin-again'), null, 'offered at once')
    for (let i = 0; i < 20; i++) await p.every('pollSignInJob')
    const again = panel(p).querySelector('button.signin-again')
    assert.ok(again, 'there is no way back to the field')
    await p.click(again)
    assert.ok(panel(p).querySelector('input'))
  } finally { p.close() }
})

// The next thing after signing the agent in is to prompt it, which isn't obvious to someone new: the page goes there.
test('when the agent is signed in the page goes to the conversation, and says so', async () => {
  const p = await pageWith({ docker: IN, agent: OUT })
  try {
    await p.click(p.$('admin-signin').querySelector('button.signin-go'))
    p.host.agent = IN
    p.host.job = { kind: 'agent', running: false, done: true, ok: true, lines: [] }
    await p.every('pollSignInJob')
    assert.equal(p.$('panel-conversation').hidden, false)
    assert.equal(p.$('panel-admin').hidden, true)
    assert.match(p.$('toast').textContent, /signed in/i)
  } finally { p.close() }
})

test("signing sbx in to Docker doesn't move the page: the agent still needs signing in", async () => {
  const p = await pageWith({ docker: OUT, agent: OUT })
  try {
    await p.click(p.$('admin-signin').querySelector('button.signin-go[data-kind=docker]'))
    p.host.docker = IN
    p.host.job = { kind: 'docker', running: false, done: true, ok: true, lines: [] }
    await p.every('pollSignInJob')
    assert.equal(p.$('panel-admin').hidden, false)
  } finally { p.close() }
})

test("a user who has gone to another tab while it signed in isn't moved", async () => {
  const p = await pageWith({ docker: IN, agent: OUT })
  try {
    await p.click(p.$('admin-signin').querySelector('button.signin-go'))
    await p.click('tab-log')
    p.host.agent = IN
    p.host.job = { kind: 'agent', running: false, done: true, ok: true, lines: [] }
    await p.every('pollSignInJob')
    assert.equal(p.$('panel-log').hidden, false)
  } finally { p.close() }
})

// The address of the agent's sign-in comes from the sandbox: only one the host knows to be on a sign-in host is a link.
test('an address on a host the host did not recognise is text with a warning, not a link', async () => {
  const p = await pageWith({ docker: IN, agent: OUT })
  try {
    await p.click(p.$('admin-signin').querySelector('button.signin-go'))
    p.host.job = { kind: 'agent', running: true, lines: [], url: 'https://look-alike.example/signin', urlKnown: false }
    await p.every('pollSignInJob')
    assert.equal(panel(p).querySelector('a'), null, 'it was made a link')
    assert.match(panel(p).textContent, /look-alike\.example\/signin/)
    assert.match(panel(p).textContent, /isn't an address vibe recognises/i)
    assert.ok(panel(p).querySelector('input'), 'the code can still be entered, by someone who has checked')
  } finally { p.close() }
})

test('an address the host recognised is a link without a warning', async () => {
  const p = await pageWith({ docker: IN, agent: OUT })
  try {
    await p.click(p.$('admin-signin').querySelector('button.signin-go'))
    p.host.job = { kind: 'agent', running: true, lines: [], url: 'https://claude.ai/oauth/authorize?x=1', urlKnown: true }
    await p.every('pollSignInJob')
    assert.ok(panel(p).querySelector('a'))
    assert.doesNotMatch(panel(p).textContent, /isn't an address vibe recognises/i)
  } finally { p.close() }
})
