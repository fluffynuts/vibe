// The Settings card on the Admin tab: the page's own preferences, kept in this browser. The first is whether what you send is
// shown as Markdown (as it is read by the agent, and as its replies are shown).
'use strict'
const test = require('node:test')
const assert = require('node:assert/strict')
const { openPage } = require('./page.js')

const PROMPT = 'please **look** at this'

async function pageWith(storage = {}) {
  const convo = { events: [{ seq: 1, kind: 'user', text: PROMPT }], running: false }
  const p = await openPage({
    routes: {
      '/api/state': { sandbox: 'box', sandboxTabs: true, conversation: true, admin: true, attention: {} },
      '/api/conversation': (call) => {
        const after = Number(new URLSearchParams(call.query).get('after')) || 0
        return { events: convo.events.filter((e) => e.seq > after), last: 1, running: false }
      },
      '/api/log': { entries: [] },
    },
    storage: Object.assign({ 'vibe-companion-tab': 'conversation' }, storage),
  })
  await p.every('pollConversation')
  return p
}

const shownAs = (p) => {
  const bubble = p.$('convo-log').querySelector('.msg.user')
  return bubble.querySelector('strong') ? 'markdown' : bubble.querySelector('.plain') ? 'plain' : 'unknown'
}

test('the setting is on the Admin tab, in a Settings card, and not in the conversation bar', async () => {
  const p = await pageWith()
  try {
    const box = p.$('settings-prompts-markdown')
    assert.ok(box, 'there is no such setting')
    const card = box.closest('section.card')
    assert.ok(card && p.$('panel-admin').contains(card), 'it is not in a card of the Admin tab')
    assert.equal(card.querySelector('h2').textContent, 'Settings')
    assert.match(box.closest('label').textContent, /My prompts as Markdown/)
    assert.equal(p.$('panel-conversation').querySelector('input[type=checkbox]#settings-prompts-markdown'), null)
    assert.doesNotMatch(p.$('panel-conversation').textContent, /My prompts as Markdown/, 'the setting is still offered in the conversation')
  } finally { p.close() }
})

test('prompts are shown as Markdown unless it was turned off in this browser', async () => {
  const on = await pageWith()
  const off = await pageWith({ 'vibe-prompts-markdown': 'off' })
  try {
    assert.equal(on.$('settings-prompts-markdown').checked, true)
    assert.equal(shownAs(on), 'markdown')
    assert.equal(off.$('settings-prompts-markdown').checked, false)
    assert.equal(shownAs(off), 'plain')
  } finally { on.close(); off.close() }
})

test('changing the setting shows the prompts already on the page the new way, and is remembered', async () => {
  const p = await pageWith()
  try {
    const box = p.$('settings-prompts-markdown')
    box.checked = false
    box.dispatchEvent(new p.window.Event('change', { bubbles: true }))
    assert.equal(shownAs(p), 'plain')
    assert.equal(p.window.localStorage.getItem('vibe-prompts-markdown'), 'off')
    box.checked = true
    box.dispatchEvent(new p.window.Event('change', { bubbles: true }))
    assert.equal(shownAs(p), 'markdown')
    assert.equal(p.window.localStorage.getItem('vibe-prompts-markdown'), 'on')
    assert.equal(p.$('convo-log').querySelector('.msg.user').prompt, PROMPT, 'what was sent is what was typed, either way')
  } finally { p.close() }
})
