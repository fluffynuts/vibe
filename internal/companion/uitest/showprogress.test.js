// An action begun from the Admin tab says so in a toast whose Show progress takes the user, keyboard focus and all, to its Last
// action card.
'use strict'
const test = require('node:test')
const assert = require('node:assert/strict')
const { openPage } = require('./page.js')

const begun = { yes: false }

test("Show progress, in the toast of an action begun, brings its Last action card into view, with focus on it", async () => {
  const p = await openPage({
    routes: {
      '/api/state': { sandbox: 'box', sandboxTabs: true, conversation: true, admin: true, attention: {} },
      '/api/conversation': { events: [], last: 0, running: false },
      '/api/admin': () => ({ status: { exists: true, running: true, profile: 'p', commands: { stop: 'vibe --stop box' } },
        job: begun.yes ? { kind: 'stop', running: true, lines: [], started: 1 } : null, upgrades: {} }),
      '/api/admin/do': () => { begun.yes = true; return { ok: true } },
      '/api/log': { entries: [] },
    },
    storage: { 'vibe-companion-tab': 'admin' },
  })
  try {
    await p.poll()
    await p.click('admin-stop')
    await p.confirm(true)
    const show = p.$('toast').querySelector('.toast-action')
    assert.ok(show, 'the toast offers no Show progress')
    assert.equal(show.textContent, 'Show progress')
    await p.click(show)
    assert.equal(p.window.document.activeElement, p.$('admin-job-card').querySelector('h2'))
  } finally { p.close() }
})
