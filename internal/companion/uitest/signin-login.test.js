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
      '/api/state': { sandbox: 'box', sandboxTabs: true, admin: true, signin: true, signinStart: start, attention: {} },
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
    p.host.job = { kind: 'docker', running: true, lines: ['Waiting...'], url: 'https://login.docker.com/activate?user_code=AB-CD', code: 'AB-CD' }
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
    p.host.job = { kind: 'agent', running: true, lines: [], url: 'https://claude.ai/oauth/authorize?x=1' }
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
    p.host.job = { kind: 'agent', running: true, lines: [] }
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
  const p = await pageWith({ docker: OUT, agent: IN, job: { kind: 'docker', running: true, lines: [], url: 'https://login.docker.com/x', code: 'ZZ-YY' } })
  try {
    assert.equal(panel(p).hidden, false)
    assert.match(panel(p).textContent, /ZZ-YY/)
  } finally { p.close() }
})
