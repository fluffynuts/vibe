// A poll that brings the same answers as the last leaves the page as it is, down to the text nodes: someone selecting text in
// a card (to copy it) would otherwise have the selection cleared, as often as the page polls, by text set to what it already
// said. In jsdom (see page.js), run with page.test.js.
'use strict'
const test = require('node:test')
const assert = require('node:assert/strict')
const { openPage } = require('./page.js')

// mutatedBy is what a tab's panel has changed in it while polls bring nothing new, as a count by the element changed.
async function mutatedBy(tab, routes, polls = 3) {
  const p = await openPage({ routes, storage: { 'vibe-companion-tab': tab } })
  try {
    const w = p.window
    const seen = new Map()
    const observer = new w.MutationObserver((records) => {
      for (const r of records) {
        const element = r.target.nodeType === 1 ? r.target : r.target.parentElement
        const named = element.closest('[id]')
        const key = named ? '#' + named.id : element.tagName.toLowerCase()
        seen.set(key, (seen.get(key) || 0) + 1)
      }
    })
    await p.poll()
    observer.observe(w.document.getElementById('panel-' + tab), { childList: true, characterData: true, subtree: true })
    for (let i = 0; i < polls; i++) await p.poll()
    return Object.fromEntries(seen)
  } finally { p.close() }
}

const adminRoutes = {
  '/api/state': { sandbox: 'box', sandboxTabs: true, conversation: true, admin: true, attention: {} },
  '/api/conversation': { events: [], last: 0, running: false },
  '/api/admin': {
    status: {
      exists: true, running: true, profile: 'p', info: 'Name: box\nAgent: claude\nPath: /work', commands: {}, guided: true,
      features: [{ name: 'dotnet', label: '.NET', on: true }, { name: 'node', label: 'Node', on: false }],
      gitToken: { has: true, source: 'own', expiry: 'expires in 20 days', rules: ['push'], renewUrl: 'https://github.com/settings/tokens', command: 'vibe --git-token', tokenName: 'vibe' },
      backups: [{ id: 'b1', label: '12 Nov, 10:00' }],
    },
    job: null,
    upgrades: { checked: true, found: ['vibe 9.9.9'] },
  },
  '/api/admin/instructions': { applies: true, running: true, agent: 'claude', files: [] },
  '/api/packages': { requests: [{ key: 'a@1', name: 'a', version: '1', approved: false }, { key: 'b@2', name: 'b', version: '2', approved: true }], config: null },
  '/api/packages/sync': { count: 2, missing: [], running: false },
  '/api/log': { entries: [] },
}

test('polls that bring nothing new change nothing on the Admin tab', async () => {
  assert.deepEqual(await mutatedBy('admin', adminRoutes), {})
})

const base = { '/api/state': { sandbox: 'box', sandboxTabs: true, conversation: true, manage: true, attention: {} }, '/api/conversation': { events: [], last: 0, running: false } }

test('polls that bring nothing new change nothing on the Network log tab', async () => {
  const entries = [
    { host: 'a.example:443', decision: 'allowed', hits: 3, lastSeen: '2026-11-12T10:00:00Z', rule: '' },
    { host: 'b.example:443', decision: 'blocked', hits: 1, lastSeen: '2026-11-12T10:01:00Z', rule: 'no applicable policies' },
  ]
  assert.deepEqual(await mutatedBy('log', { ...base, '/api/log': { entries } }), {})
})

test('polls that bring nothing new change nothing on the Network rules and Filesystem rules tabs', async () => {
  const rules = [{ id: 'r1', name: 'r1', decision: 'allow', resources: ['a.example'], source: 'user', scope: 'sandbox', protocol: 'tcp', status: 'active', removable: true }]
  assert.deepEqual(await mutatedBy('rules', { ...base, '/api/rules': { rules }, '/api/log': { entries: [] } }), {})
  assert.deepEqual(await mutatedBy('fs', { ...base, '/api/fs-rules': { rules }, '/api/log': { entries: [] } }), {})
})

test('polls that bring nothing new change nothing on the Profiles tab', async () => {
  assert.deepEqual(await mutatedBy('manage', { ...base, '/api/manage': { profiles: [{ name: 'p', default: true }], sandboxes: [] }, '/api/log': { entries: [] } }), {})
})

// Changes still show: only what is the same is left alone.
test('a change in what the Network log holds is drawn, and the host row is drawn again with it', async () => {
  let entries = [{ host: 'a.example:443', decision: 'allowed', hits: 3, lastSeen: '2026-11-12T10:00:00Z', rule: '' }]
  const p = await openPage({ routes: { ...base, '/api/log': () => ({ entries }) }, storage: { 'vibe-companion-tab': 'log' } })
  try {
    await p.poll()
    assert.match(p.$('log-table').textContent, /a\.example/)
    entries = [...entries, { host: 'new.example:443', decision: 'blocked', hits: 1, lastSeen: '2026-11-12T10:05:00Z', rule: '' }]
    await p.poll()
    assert.match(p.$('log-table').textContent, /new\.example/)
    assert.match(p.$('log-summary').textContent, /2 total/)
  } finally { p.close() }
})

test('a change in the Sandbox card is drawn', async () => {
  let info = 'Name: box'
  const p = await openPage({ routes: { ...adminRoutes, '/api/admin': () => ({ status: { exists: true, running: true, profile: 'p', info }, job: null, upgrades: {} }) }, storage: { 'vibe-companion-tab': 'admin' } })
  try {
    await p.poll()
    assert.equal(p.$('admin-info').textContent, 'Name: box')
    info = 'Name: box\nAgent: claude'
    await p.poll()
    assert.equal(p.$('admin-info').textContent, 'Name: box\nAgent: claude')
  } finally { p.close() }
})
