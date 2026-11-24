// The agent's permission mode on the Admin tab: a drop-down that keeps the choice, what is in force beside it, and a warning, on the
// card and in the Conversation tab's status balloon, when the mode the agent reports isn't the one it was asked for
// (docs/plan-agent-permission-mode.md). In jsdom (see page.js), run with page.test.js.
'use strict'
const test = require('node:test')
const assert = require('node:assert/strict')
const { openPage } = require('./page.js')

const MODES = ['auto', 'bypassPermissions', 'acceptEdits', 'manual', 'dontAsk', 'plan']

// pageWith opens a tab on a page whose vibe answers the permission mode with at (changed by the test), and the conversation's poll
// with poll.
async function pageWith({ tab = 'admin', choice = true, at = {}, poll = {} } = {}) {
  const now = Object.assign({ mode: '', from: '', default: 'bypassPermissions', modes: MODES }, at)
  const p = await openPage({
    routes: {
      '/api/state': { sandbox: 'box', sandboxTabs: true, conversation: true, admin: true, permissionModeChoice: choice, attention: {} },
      '/api/conversation': () => Object.assign({ events: [], last: 0, running: false }, poll),
      '/api/admin': { instructions: {} },
      '/api/log': { entries: [] },
      '/api/permission-mode': (call) => {
        if (call.method === 'POST' && now.refuse) return { status: 500, body: { error: now.refuse } }
        if (call.method === 'POST') { now.mode = call.body.mode; now.from = call.body.mode ? '~/.vibe/profiles/p/user-preferences.yaml' : '' }
        return now
      },
    },
    storage: { 'vibe-companion-tab': tab },
  })
  return Object.assign(p, { now })
}

const select = (p) => p.$('admin-permission-mode')
const card = (p) => select(p).closest('section.card')

test('the Admin tab has a labelled drop-down of the modes, showing the default when nothing is chosen', async () => {
  const p = await pageWith()
  try {
    assert.ok(!card(p).hidden, 'the card is hidden')
    assert.match(card(p).querySelector('h2').textContent, /permission/i)
    assert.match(p.window.document.querySelector('label[for="admin-permission-mode"]').textContent, /mode/i)
    assert.deepEqual(Array.from(select(p).options).map((o) => o.value), MODES)
    assert.equal(select(p).value, 'bypassPermissions')
    assert.match(select(p).selectedOptions[0].textContent, /default/i)
  } finally { p.close() }
})

test('a mode chosen in a settings file is shown, with the file it comes from', async () => {
  const p = await pageWith({ at: { mode: 'acceptEdits', from: '~/.vibe/settings.yaml' } })
  try {
    assert.equal(select(p).value, 'acceptEdits')
    assert.match(card(p).textContent, /~\/\.vibe\/settings\.yaml/)
  } finally { p.close() }
})

test('choosing a mode keeps it, and says it applies from the next prompt', async () => {
  const p = await pageWith()
  try {
    select(p).value = 'bypassPermissions'
    select(p).dispatchEvent(new p.window.Event('change', { bubbles: true }))
    await p.settle()
    assert.deepEqual(p.posted('/api/permission-mode'), [{ mode: 'bypassPermissions' }])
    assert.match(card(p).textContent, /next prompt/i)
  } finally { p.close() }
})

test('a choice vibe refused puts the drop-down back and says why', async () => {
  const p = await pageWith({ at: { refuse: "profile 'p' is vibe's own" } })
  try {
    select(p).value = 'plan'
    select(p).dispatchEvent(new p.window.Event('change', { bubbles: true }))
    await p.settle()
    assert.equal(select(p).value, 'bypassPermissions')
    assert.match(card(p).textContent, /vibe's own/)
  } finally { p.close() }
})

test('the card says the mode in force, and warns, with Claude Code\'s words, when it is not the one asked for', async () => {
  const refused = 'Cannot set permission mode to bypassPermissions because it is disabled by settings or configuration'
  const p = await pageWith({ at: { mode: 'bypassPermissions', asked: 'bypassPermissions', inForce: 'auto', refused } })
  try {
    const warning = p.$('admin-permission-warning')
    assert.ok(!warning.hidden, 'no warning')
    assert.match(warning.textContent, /running in Auto/)
    assert.match(warning.textContent, /Bypass permissions/)
    assert.ok(warning.textContent.includes(refused))
  } finally { p.close() }
})

test('no warning when the mode in force is the one asked for', async () => {
  const p = await pageWith({ at: { mode: 'acceptEdits', asked: 'acceptEdits', inForce: 'acceptEdits' } })
  try {
    assert.ok(p.$('admin-permission-warning').hidden)
    assert.match(card(p).textContent, /in force: Accept edits/i)
  } finally { p.close() }
})

test('a page whose vibe cannot choose the mode has no such card', async () => {
  const p = await pageWith({ choice: false })
  try {
    assert.ok(card(p).hidden)
    assert.equal(p.calls.filter((c) => c.path === '/api/permission-mode').length, 0)
  } finally { p.close() }
})

test('the status balloon warns, and stays, when the agent runs in another mode than it was asked for', async () => {
  const p = await pageWith({ tab: 'conversation', poll: { running: true, permissionMode: { asked: 'bypassPermissions', inForce: 'manual' } } })
  try {
    await p.every('pollConversation')
    const balloon = p.$('status-balloon')
    assert.ok(!balloon.hidden, 'the warning did not pop up')
    assert.match(balloon.textContent, /running in Manual/)
    assert.match(balloon.textContent, /Bypass permissions/)
    assert.match(balloon.textContent, /refused/i, 'it does not say what Manual means here')
    assert.ok(p.$('status-info').classList.contains('warn'))
  } finally { p.close() }
})

test('no balloon warning when the poll says nothing of the mode', async () => {
  const p = await pageWith({ tab: 'conversation', poll: { running: true, permissionMode: null } })
  try {
    await p.every('pollConversation')
    assert.ok(!p.$('status-info').classList.contains('warn'))
  } finally { p.close() }
})

// Bypass refused (an organisation's settings disable it): the warning offers the fix, one click, rather than vibe choosing a mode
// by itself.
test('when bypass is not in force, the card offers Use Auto, which chooses Auto', async () => {
  const p = await pageWith({ at: { asked: 'bypassPermissions', inForce: 'manual' } })
  try {
    const use = p.button('Use Auto', card(p))
    assert.ok(use, 'no Use Auto')
    await p.click(use)
    assert.deepEqual(p.posted('/api/permission-mode'), [{ mode: 'auto' }])
    assert.match(card(p).textContent, /next prompt/i)
  } finally { p.close() }
})

test('Use Auto is offered only when it was bypass that was refused', async () => {
  const p = await pageWith({ at: { mode: 'acceptEdits', asked: 'acceptEdits', inForce: 'manual' } })
  try {
    assert.ok(!p.$('admin-permission-warning').hidden)
    assert.equal(p.button('Use Auto', card(p)), undefined)
  } finally { p.close() }
})

test('the balloon offers Use Auto too, when bypass is not in force', async () => {
  const p = await pageWith({ tab: 'conversation', poll: { running: true, permissionMode: { asked: 'bypassPermissions', inForce: 'manual' } } })
  try {
    await p.every('pollConversation')
    const use = p.button('Use Auto', p.$('status-balloon'))
    assert.ok(use, 'no Use Auto in the balloon')
    await p.click(use)
    assert.deepEqual(p.posted('/api/permission-mode'), [{ mode: 'auto' }])
  } finally { p.close() }
})

const agentSaid = (seq, text) => ({ seq, kind: 'agent', agent: { type: 'assistant', message: { content: [{ type: 'text', text }] } } })

test('a permission Claude Code refused is a line in the conversation, with a way to the Admin tab\'s choice', async () => {
  const said = "Claude Code didn't let the agent use Bash: Output redirection to '/tmp/x' needs approval. (the agent is in Manual)"
  const p = await pageWith({ tab: 'conversation', poll: { events: [agentSaid(1, 'Writing it.'), { seq: 2, kind: 'denied', text: said }], last: 2 } })
  try {
    await p.every('pollConversation')
    const line = p.$('convo-log').querySelector('.msg.denied')
    assert.ok(line, 'no line for the refusal')
    assert.ok(line.textContent.includes(said))
    await p.click(p.button('Agent permissions…', line))
    assert.equal(p.$('tab-admin').getAttribute('aria-selected'), 'true')
    assert.equal(p.window.document.activeElement, select(p))
  } finally { p.close() }
})

// The Conversation tab says the mode the agent is in, as Claude Code reports it, beside the model and effort.
test('the Conversation tab says the permission mode the agent is in, and opens its card when pressed', async () => {
  const p = await pageWith({ tab: 'conversation', poll: { running: true, agentMode: { asked: 'manual', inForce: 'manual' } } })
  try {
    await p.every('pollConversation')
    const label = p.$('agent-mode')
    assert.ok(!label.hidden, 'not shown')
    assert.match(label.textContent, /Permissions: Manual/)
    assert.ok(!label.classList.contains('differs'))
    await p.click(label)
    assert.equal(p.$('tab-admin').getAttribute('aria-selected'), 'true')
    assert.equal(p.window.document.activeElement, select(p))
  } finally { p.close() }
})

test('the label is amber, and says so, when the mode in force is not the one chosen', async () => {
  const p = await pageWith({ tab: 'conversation', poll: { running: true, agentMode: { asked: 'bypassPermissions', inForce: 'auto' } } })
  try {
    await p.every('pollConversation')
    const label = p.$('agent-mode')
    assert.match(label.textContent, /Permissions: Auto/)
    assert.ok(label.classList.contains('differs'))
    assert.match(label.title, /Bypass permissions/)
  } finally { p.close() }
})

test('the label is not shown until the agent has said its mode', async () => {
  const p = await pageWith({ tab: 'conversation', poll: { running: false, agentMode: null } })
  try {
    await p.every('pollConversation')
    assert.ok(p.$('agent-mode').hidden)
  } finally { p.close() }
})

// A refusal offers the mode that would let it through, as vibe worked it out (the event's suggest).
for (const [suggest, label] of [['bypassPermissions', 'Use Bypass permissions'], ['auto', 'Use Auto']]) {
  test('a refusal suggesting ' + suggest + ' offers ' + label + ', which chooses it', async () => {
    const p = await pageWith({ tab: 'conversation', poll: { events: [{ seq: 1, kind: 'denied', text: 'refused', suggest }], last: 1 } })
    try {
      await p.every('pollConversation')
      const line = p.$('convo-log').querySelector('.msg.denied')
      await p.click(p.button(label, line))
      assert.deepEqual(p.posted('/api/permission-mode'), [{ mode: suggest }])
    } finally { p.close() }
  })
}

test('a refusal with nothing to suggest offers only the way to the card', async () => {
  const p = await pageWith({ tab: 'conversation', poll: { events: [{ seq: 1, kind: 'denied', text: 'refused' }], last: 1 } })
  try {
    await p.every('pollConversation')
    const line = p.$('convo-log').querySelector('.msg.denied')
    assert.deepEqual([...line.querySelectorAll('button')].map((b) => b.textContent), ['Agent permissions…'])
  } finally { p.close() }
})
