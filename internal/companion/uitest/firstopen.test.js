// Which tab the page opens on, with nothing chosen yet (a first open): the conversation where there is one, since that is
// where the prompt is.
'use strict'
const test = require('node:test')
const assert = require('node:assert/strict')
const { openPage } = require('./page.js')

test('a first open lands on the conversation', async () => {
  const p = await openPage({
    routes: {
      '/api/state': { sandbox: 'box', sandboxTabs: true, conversation: true, admin: true, attention: {} },
      '/api/conversation': { events: [], last: 0, running: false },
      '/api/log': { entries: [] },
    },
  })
  try {
    assert.equal(p.$('panel-conversation').hidden, false)
    assert.equal(p.$('panel-log').hidden, true)
  } finally { p.close() }
})

test('a tab the user chose is the one it opens on again', async () => {
  const p = await openPage({
    routes: {
      '/api/state': { sandbox: 'box', sandboxTabs: true, conversation: true, admin: true, attention: {} },
      '/api/conversation': { events: [], last: 0, running: false },
      '/api/log': { entries: [] },
    },
    storage: { 'vibe-companion-tab': 'log' },
  })
  try {
    assert.equal(p.$('panel-log').hidden, false)
  } finally { p.close() }
})

test('where there is no conversation it opens on the first tab there is, and does not keep that as a choice', async () => {
  const p = await openPage({
    routes: {
      '/api/state': { sandbox: 'box', sandboxTabs: true, conversation: false, admin: true, attention: {} },
      '/api/log': { entries: [] },
    },
  })
  try {
    assert.equal(p.$('panel-conversation').hidden, true)
    const shown = ['review', 'log', 'rules', 'fs', 'admin', 'manage'].filter((t) => !p.$('panel-' + t).hidden)
    assert.equal(shown.length, 1, 'tabs shown: ' + shown)
    assert.equal(p.window.localStorage.getItem('vibe-companion-tab'), null, 'a fallback was kept as the choice')
  } finally { p.close() }
})
