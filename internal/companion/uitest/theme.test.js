// The colour theme on the Settings card of the Admin tab: a label and a drop-down, applied as it is changed. The theme is the
// agent's, and the page's accent follows it.
'use strict'
const test = require('node:test')
const assert = require('node:assert/strict')
const { openPage } = require('./page.js')

async function pageWith({ themes = true, current = 'default', live = true, own = true, set = null } = {}) {
  const at = { current }
  const p = await openPage({
    routes: {
      '/api/state': { sandbox: 'box', sandboxTabs: true, conversation: true, admin: true, themes, themeColor: '', attention: {} },
      '/api/conversation': { events: [], last: 0, running: false },
      '/api/admin': { instructions: {} },
      '/api/log': { entries: [] },
      '/api/theme': (call) => {
        if (call.method === 'GET') return { current: at.current, options: ['default', 'amber', 'teal'], agentThemes: own, live: own && live }
        if (set) return set(call, at)
        at.current = call.body.choice
        return { agentThemes: own, live: own && live, color: call.body.choice === 'teal' ? '#00aaaa' : '#ffaa00' }
      },
    },
    storage: { 'vibe-companion-tab': 'admin' },
    // jsdom has no CSS.supports, which the page asks of a colour before using it.
    before: (w) => { w.CSS = { supports: () => true } },
  })
  return Object.assign(p, { at })
}

const rowOf = (p) => p.$('settings-theme').closest('section.card')

test('the theme is a labelled drop-down on the Settings card, with the default and the themes on offer', async () => {
  const p = await pageWith({ current: 'amber' })
  try {
    const select = p.$('settings-theme')
    assert.equal(rowOf(p).querySelector('h2').textContent, 'Settings')
    assert.match(p.window.document.querySelector('label[for="settings-theme"]').textContent, /theme/i)
    assert.deepEqual(Array.from(select.options).map((o) => o.value), ['default', 'amber', 'teal'])
    assert.equal(select.value, 'amber', 'it does not show the current theme')
    assert.ok(!p.window.document.getElementById('settings-theme-row').hidden)
  } finally { p.close() }
})

test('a page whose agent has no themes does not offer the drop-down', async () => {
  const p = await pageWith({ themes: false })
  try {
    assert.ok(p.window.document.getElementById('settings-theme-row').hidden)
    assert.equal(p.posted('/api/theme').length, 0)
  } finally { p.close() }
})

test('choosing a theme sets it at once, and the page takes its colour at once', async () => {
  const p = await pageWith()
  try {
    p.$('settings-theme').value = 'teal'
    p.$('settings-theme').dispatchEvent(new p.window.Event('change', { bubbles: true }))
    await p.settle()
    assert.deepEqual(p.posted('/api/theme'), [{ choice: 'teal' }])
    assert.equal(p.window.document.documentElement.style.getPropertyValue('--theme'), '#00aaaa')
    assert.ok(p.window.document.documentElement.classList.contains('themed'))
    assert.ok(!p.$('settings-theme').disabled, 'it is left unusable')
  } finally { p.close() }
})

test("choosing a theme in a running sandbox warns that an open Claude terminal keeps its colours until restarted", async () => {
  const p = await pageWith()
  try {
    assert.equal(p.$('settings-theme-note').textContent, '', 'a warning with nothing changed')
    p.$('settings-theme').value = 'teal'
    p.$('settings-theme').dispatchEvent(new p.window.Event('change', { bubbles: true }))
    await p.settle()
    assert.match(p.$('settings-theme-note').textContent, /terminal.*restart/i)
  } finally { p.close() }
})

test('choosing the default takes the page back to its own colour', async () => {
  const p = await pageWith({ current: 'teal', set: () => ({ live: true, color: '' }) })
  try {
    p.$('settings-theme').value = 'default'
    p.$('settings-theme').dispatchEvent(new p.window.Event('change', { bubbles: true }))
    await p.settle()
    assert.ok(!p.window.document.documentElement.classList.contains('themed'))
  } finally { p.close() }
})

test('a sandbox that is not running says the choice waits for its next build', async () => {
  const p = await pageWith({ live: false })
  try {
    assert.match(p.$('settings-theme-note').textContent, /next (time|build)|built/)
    p.$('settings-theme').value = 'teal'
    p.$('settings-theme').dispatchEvent(new p.window.Event('change', { bubbles: true }))
    await p.settle()
    assert.match(p.$('settings-theme-note').textContent, /built/)
  } finally { p.close() }
})

test('a theme that could not be set is said, and the drop-down goes back to what it was', async () => {
  const p = await pageWith({ current: 'amber', set: () => ({ status: 500, body: { error: "settings.json isn't readable as JSON" } }) })
  try {
    p.$('settings-theme').value = 'teal'
    p.$('settings-theme').dispatchEvent(new p.window.Event('change', { bubbles: true }))
    await p.settle()
    assert.equal(p.$('settings-theme').value, 'amber')
    assert.match(p.$('settings-theme-note').textContent, /isn't readable/)
    assert.ok(!p.$('settings-theme').disabled)
  } finally { p.close() }
})

test('for an agent with no themes of its own, the note says a choice colours this page only, before and after choosing', async () => {
  const p = await pageWith({ own: false })
  try {
    const note = () => p.$('settings-theme-note').textContent
    assert.match(note(), /this page only/i)
    p.$('settings-theme').value = 'teal'
    p.$('settings-theme').dispatchEvent(new p.window.Event('change', { bubbles: true }))
    await p.settle()
    assert.match(note(), /this page only/i)
    assert.doesNotMatch(note(), /terminal|built|restart/i, 'a warning for a terminal this change did not touch')
    assert.equal(p.window.document.documentElement.style.getPropertyValue('--theme'), '#00aaaa')
  } finally { p.close() }
})
